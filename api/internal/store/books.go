package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/collinpendleton/backhog/api/internal/metadata"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// UpsertBook writes provider metadata into the shared book cache — the work
// row, plus its edition when the record carries one (ISBN lookups) — in one
// transaction, mirroring UpsertGame. Fields a lean search fetch does not carry
// (description, subjects, cover, accent, year) are preserved rather than
// blanked, so searching a book must not wipe its detail metadata.
func (s *Store) UpsertBook(ctx context.Context, b metadata.Book, accentHex string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	authorsJSON := marshalStrings(b.Authors)
	subjectsJSON := marshalStrings(b.Subjects)

	_, err = tx.ExecContext(ctx, `
		INSERT INTO books (id, title, authors_json, description, cover_url,
		                   cover_local_path, accent_hex, first_publish_year,
		                   subjects_json, raw_json, fetched_at)
		VALUES (?, ?, ?, ?, ?, '', ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			title              = excluded.title,
			authors_json       = CASE WHEN excluded.authors_json <> '[]'
			                          THEN excluded.authors_json ELSE books.authors_json END,
			description        = COALESCE(NULLIF(excluded.description, ''), books.description),
			cover_url          = COALESCE(NULLIF(excluded.cover_url, ''), books.cover_url),
			accent_hex         = COALESCE(NULLIF(excluded.accent_hex, ''), books.accent_hex),
			first_publish_year = COALESCE(excluded.first_publish_year, books.first_publish_year),
			subjects_json      = CASE WHEN excluded.subjects_json <> '[]'
			                          THEN excluded.subjects_json ELSE books.subjects_json END,
			raw_json           = COALESCE(NULLIF(excluded.raw_json, ''), books.raw_json),
			fetched_at         = CURRENT_TIMESTAMP`,
		b.ID, b.Title, authorsJSON, b.Description, b.CoverURL, accentHex,
		b.FirstPublishYear, subjectsJSON, string(b.Raw))
	if err != nil {
		return err
	}

	if b.Edition != nil {
		if err := upsertEdition(ctx, tx, *b.Edition); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// upsertEdition writes one edition row. Fields not carried by a particular
// fetch (a sparse record) keep whatever a richer previous fetch stored.
func upsertEdition(ctx context.Context, tx *sql.Tx, ed metadata.BookEdition) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO book_editions (id, book_id, isbn10, isbn13, publisher,
		                           published_year, page_count, binding, language,
		                           cover_url, raw_json, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			isbn10         = COALESCE(NULLIF(excluded.isbn10, ''), book_editions.isbn10),
			isbn13         = COALESCE(NULLIF(excluded.isbn13, ''), book_editions.isbn13),
			publisher      = COALESCE(NULLIF(excluded.publisher, ''), book_editions.publisher),
			published_year = COALESCE(excluded.published_year, book_editions.published_year),
			page_count     = COALESCE(excluded.page_count, book_editions.page_count),
			binding        = COALESCE(NULLIF(excluded.binding, ''), book_editions.binding),
			language       = COALESCE(NULLIF(excluded.language, ''), book_editions.language),
			cover_url      = COALESCE(NULLIF(excluded.cover_url, ''), book_editions.cover_url),
			raw_json       = COALESCE(NULLIF(excluded.raw_json, ''), book_editions.raw_json),
			fetched_at     = CURRENT_TIMESTAMP`,
		ed.ID, ed.BookID, ed.ISBN10, ed.ISBN13, ed.Publisher,
		ed.PublishedYear, ed.PageCount, ed.Binding, ed.Language,
		ed.CoverURL, string(ed.Raw))
	return err
}

func marshalStrings(list []string) string {
	if len(list) == 0 {
		return "[]"
	}
	encoded, err := json.Marshal(list)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

// UpsertEditions writes a work's edition list in one transaction — the
// enrich-on-add path, so the add dialog gets every printing without a second
// round trip. Sparse records keep whatever a richer previous fetch stored.
func (s *Store) UpsertEditions(ctx context.Context, bookID string, eds []metadata.BookEdition) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for i := range eds {
		if eds[i].BookID == "" {
			eds[i].BookID = bookID
		}
		if err := upsertEdition(ctx, tx, eds[i]); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetBook returns a cached work with its editions.
func (s *Store) GetBook(ctx context.Context, id string) (models.Book, error) {
	book, err := s.bookByID(ctx, id)
	if err != nil {
		return models.Book{}, err
	}
	eds, err := s.editionsByBook(ctx, id)
	if err != nil {
		return models.Book{}, err
	}
	book.Editions = eds
	return book, nil
}

// BooksByIDs loads a set of cached works keyed by id, without editions — the
// lean shape search results are served in.
func (s *Store) BooksByIDs(ctx context.Context, ids []string) (map[string]models.Book, error) {
	out := make(map[string]models.Book, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	placeholders, args := inClauseStr(ids)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, authors_json, COALESCE(description, ''), COALESCE(cover_url, ''),
		       COALESCE(accent_hex, ''), first_publish_year, subjects_json
		FROM books WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		b, err := scanBook(rows)
		if err != nil {
			return nil, err
		}
		out[b.ID] = b
	}
	return out, rows.Err()
}

// CoverURLForBook returns the upstream cover URL for a work, used to lazily
// re-download a cover that is missing from disk.
func (s *Store) CoverURLForBook(ctx context.Context, id string) (string, error) {
	var url sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT cover_url FROM books WHERE id = ?`, id).Scan(&url)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return url.String, err
}

// RecordBookCover stores the on-disk cover path and sampled accent colour for
// a work.
func (s *Store) RecordBookCover(ctx context.Context, id, localPath, accentHex string) error {
	if localPath == "" && accentHex == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE books SET cover_local_path = COALESCE(NULLIF(?, ''), cover_local_path),
		                  accent_hex = COALESCE(NULLIF(?, ''), accent_hex)
		 WHERE id = ?`, localPath, accentHex, id)
	return err
}

// OwnedBookIDs reports which of the given works are already in a user's
// library, so search results can be marked as added.
func (s *Store) OwnedBookIDs(ctx context.Context, userID string, ids []string) (map[string]bool, error) {
	owned := make(map[string]bool, len(ids))
	if len(ids) == 0 {
		return owned, nil
	}
	placeholders, args := inClauseStr(ids)
	rows, err := s.db.QueryContext(ctx,
		`SELECT book_id FROM library_entries WHERE user_id = ? AND book_id IN (`+placeholders+`)`,
		append([]any{userID}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		owned[id] = true
	}
	return owned, rows.Err()
}

// BookEntryIDs maps work ids to the user's entry ids for them — the attach
// flow is entry-keyed, so search results and suggestions carry the entry
// when the book is already owned.
func (s *Store) BookEntryIDs(ctx context.Context, userID string, ids []string) (map[string]string, error) {
	out := make(map[string]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	placeholders, args := inClauseStr(ids)
	rows, err := s.db.QueryContext(ctx,
		`SELECT book_id, id FROM library_entries WHERE user_id = ? AND media_type = 'book' AND book_id IN (`+placeholders+`)`,
		append([]any{userID}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var bookID, entryID string
		if err := rows.Scan(&bookID, &entryID); err != nil {
			return nil, err
		}
		out[bookID] = entryID
	}
	return out, rows.Err()
}

func (s *Store) bookByID(ctx context.Context, id string) (models.Book, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, title, authors_json, COALESCE(description, ''), COALESCE(cover_url, ''),
		       COALESCE(accent_hex, ''), first_publish_year, subjects_json
		FROM books WHERE id = ?`, id)
	b, err := scanBook(row)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Book{}, ErrNotFound
	}
	return b, err
}

// scanBook reads a work row from either a Rows or a Row.
func scanBook(row interface{ Scan(dest ...any) error }) (models.Book, error) {
	var b models.Book
	var authors, subjects string
	if err := row.Scan(&b.ID, &b.Title, &authors, &b.Description, &b.CoverURL,
		&b.AccentHex, &b.FirstPublishYear, &subjects); err != nil {
		return models.Book{}, err
	}
	b.Authors = unmarshalStrings(authors)
	b.Subjects = unmarshalStrings(subjects)
	return b, nil
}

func (s *Store) editionsByBook(ctx context.Context, bookID string) ([]models.BookEdition, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, book_id, isbn10, isbn13, publisher, published_year,
		       page_count, binding, language, cover_url
		FROM book_editions WHERE book_id = ?
		ORDER BY published_year, id`, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	eds := []models.BookEdition{}
	for rows.Next() {
		var ed models.BookEdition
		if err := rows.Scan(&ed.ID, &ed.BookID, &ed.ISBN10, &ed.ISBN13, &ed.Publisher,
			&ed.PublishedYear, &ed.PageCount, &ed.Binding, &ed.Language, &ed.CoverURL); err != nil {
			return nil, err
		}
		eds = append(eds, ed)
	}
	return eds, rows.Err()
}

func unmarshalStrings(encoded string) []string {
	var out []string
	if err := json.Unmarshal([]byte(encoded), &out); err != nil {
		return []string{}
	}
	if out == nil {
		return []string{}
	}
	return out
}

// inClauseStr is inClause for TEXT keys.
func inClauseStr(ids []string) (string, []any) {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(ids)), ","), args
}

// OwnedBookEntries returns every book the user has a library entry for, with
// that entry's id, in the lean shape (no editions) the matcher scores against.
//
// Deliberately not ListEntries: that one paginates, and silently defaults to
// 60 rows when no limit is given. A matcher that sees only part of a library
// proposes books the user already owns as if they were new, which then fails
// to attach — so this query is unbounded by design. It stays cheap because it
// is one join returning only what scoring and display need.
func (s *Store) OwnedBookEntries(ctx context.Context, userID string) ([]models.Book, map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT b.id, b.title, b.authors_json, COALESCE(b.description, ''), COALESCE(b.cover_url, ''),
		       COALESCE(b.accent_hex, ''), b.first_publish_year, b.subjects_json, e.id
		FROM library_entries e
		JOIN books b ON b.id = e.book_id
		WHERE e.user_id = ? AND e.media_type = 'book'
		ORDER BY e.created_at DESC, e.id`, userID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var books []models.Book
	entryIDs := map[string]string{}
	for rows.Next() {
		var b models.Book
		var authorsJSON, subjectsJSON string
		var entryID string
		if err := rows.Scan(&b.ID, &b.Title, &authorsJSON, &b.Description, &b.CoverURL,
			&b.AccentHex, &b.FirstPublishYear, &subjectsJSON, &entryID); err != nil {
			return nil, nil, err
		}
		b.Authors = unmarshalStrings(authorsJSON)
		b.Subjects = unmarshalStrings(subjectsJSON)
		// A user with two entries for one book keeps the first, which the
		// ordering makes the most recently added.
		if _, seen := entryIDs[b.ID]; seen {
			continue
		}
		entryIDs[b.ID] = entryID
		books = append(books, b)
	}
	return books, entryIDs, rows.Err()
}
