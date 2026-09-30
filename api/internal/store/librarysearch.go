package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// The library-wide text search (MAD-470). The FTS5 table is written on
// ingest and re-parse (books.EnsureForMediaFile) and healed by the search
// index walk; everything here is the read side plus the replace that keeps
// the table honest, so no handler ever builds FTS syntax of its own.

// BookTextMatch is one book's best FTS hit for a query: the book and the
// bm25 rank of its best-matching chapter. Ranks are FTS5's — lower is
// better — and only their order is meaningful.
type BookTextMatch struct {
	BookID string
	Rank   float64
}

// LibrarySearchBook is one searchable book resolved for one user: their
// entry for it, its title, and the canonical text every hit will be placed
// against. Built from the FTS candidates by LibrarySearchScope, which is
// where the arena's whole permission model — ownership, unowned files,
// shares — decides what a library search may even see.
type LibrarySearchBook struct {
	EntryID          string
	BookID           string
	Title            string
	TextID           string
	CharCount        int
	NormalizedSHA256 string
	MediaFileID      int64
}

// FoldForFTS prepares canonical text or a folded query for the FTS table.
//
// The canonical text is already lowercase letters, digits and single
// spaces, which is exactly the unicode61 tokenizer's token space — so this
// is nearly the identity. The one transformation is CJK: unicode61 treats a
// whole run of Han/Kana/Hangul as a single token, so 東京の物語 would be
// unmatchable except as that exact run. Space-separating every CJK rune on
// the way in makes each rune a token, and because the query builder folds
// through this same function, a remembered fragment of a Japanese sentence
// matches as a phrase the way it does in the in-book search.
func FoldForFTS(text string) string {
	cjk := false
	for _, r := range text {
		if isCJK(r) {
			cjk = true
			break
		}
	}
	if !cjk {
		return text
	}
	var b strings.Builder
	b.Grow(len(text) + len(text)/2)
	for _, r := range text {
		if isCJK(r) {
			b.WriteRune(r)
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// isCJK reports whether a rune belongs to the script families unicode61
// would glom into one token: Han, Hiragana, Katakana and Hangul.
func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}

// ReplaceBookTextIndex writes one media file's chapter text into the FTS
// table, replacing whatever was there. The chapter ranges partition
// [0, len(text)) exactly, so the slices tile the book; an empty chapter
// contributes nothing. Idempotent by construction: delete-then-insert under
// one transaction, keyed by media file so a re-parse (same file, same
// epub_texts row id) lands in place.
func (s *Store) ReplaceBookTextIndex(ctx context.Context, bookID string, mediaFileID int64,
	chapters []models.EpubChapter, text string) error {

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM book_text_fts WHERE media_file_id = ?`, mediaFileID); err != nil {
		return err
	}
	for _, ch := range chapters {
		if ch.CharEnd <= ch.CharStart || ch.CharEnd > len(text) {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO book_text_fts (body, book_id, media_file_id) VALUES (?, ?, ?)`,
			FoldForFTS(text[ch.CharStart:ch.CharEnd]), bookID, mediaFileID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// BookTextIndexed reports whether a media file has any FTS rows — the
// currency check the ingester's fast path and the index walk both use.
func (s *Store) BookTextIndexed(ctx context.Context, mediaFileID int64) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM book_text_fts WHERE media_file_id = ? LIMIT 1`, mediaFileID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// SearchBookTextIndex runs one MATCH expression over the FTS table and
// returns the books it hit, best chapter first. bm25 cannot be aggregated
// inside SQLite's GROUP BY, so the per-book best is kept in Go: rows arrive
// rank-ordered and a book's first row is its best one.
//
// rowLimit bounds the chapter rows read for a pathological query (a term
// every chapter contains); books past the bound are simply not candidates,
// which is a truncation a library search can live with and a common word
// deserves.
func (s *Store) SearchBookTextIndex(ctx context.Context, expr string, rowLimit int) ([]BookTextMatch, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT book_id, bm25(book_text_fts) AS rank
		FROM book_text_fts
		WHERE book_text_fts MATCH ?
		ORDER BY rank
		LIMIT ?`, expr, rowLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BookTextMatch
	seen := make(map[string]bool)
	for rows.Next() {
		var m BookTextMatch
		if err := rows.Scan(&m.BookID, &m.Rank); err != nil {
			return nil, err
		}
		if seen[m.BookID] {
			continue
		}
		seen[m.BookID] = true
		out = append(out, m)
	}
	return out, rows.Err()
}

// LibrarySearchScope resolves FTS candidate books to the caller's searchable
// copies: a library entry of their own for the book, the file-access rule
// satisfied (attached by nobody, by them, or shared with them), a present
// primary text file — the same resolver the reading paths use — and a
// current canonical parse. A book that fails any leg is absent,
// indistinguishable from never having matched, the same denial shape every
// file-backed path uses.
func (s *Store) LibrarySearchScope(ctx context.Context, userID string, bookIDs []string) ([]LibrarySearchBook, error) {
	if len(bookIDs) == 0 {
		return nil, nil
	}
	placeholders := strings.Repeat("?,", len(bookIDs))
	args := make([]any, 0, len(bookIDs)+3)
	args = append(args, userID)
	for _, id := range bookIDs {
		args = append(args, id)
	}
	args = append(args, userID, userID)

	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.book_id, COALESCE(NULLIF(b.title, ''), 'Untitled'),
		       et.id, et.char_count, et.normalized_sha256, file.id
		FROM library_entries e
		JOIN books b ON b.id = e.book_id
		JOIN media_files file ON file.id = (
			SELECT mf.id FROM media_files mf
			WHERE mf.book_id = e.book_id AND mf.missing_at IS NULL AND `+primaryTextIs+`
			ORDER BY `+TextFormatRank+`, mf.id
			LIMIT 1)
		JOIN epub_texts et ON et.media_file_id = file.id
		WHERE e.user_id = ? AND e.media_type = 'book'
		  AND e.book_id IN (`+placeholders[:len(placeholders)-1]+`)
		  AND `+fileAccessRule("e.book_id"),
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LibrarySearchBook
	for rows.Next() {
		var b LibrarySearchBook
		if err := rows.Scan(&b.EntryID, &b.BookID, &b.Title,
			&b.TextID, &b.CharCount, &b.NormalizedSHA256, &b.MediaFileID); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// BookTextIndexCandidates lists primary text files whose book is not fully
// indexed: never parsed, parsed under a parser version that has since moved
// (the re-parse itself still to run), or parsed and current but missing FTS
// rows. Files the PDF gate already ruled image-native are excluded — they
// will never produce a canonical text, and their lettering is the OCR
// corpus's domain, not this one. Ordered by id past the cursor so a bounded
// walk rotates through the backlog instead of retrying its head forever.
func (s *Store) BookTextIndexCandidates(ctx context.Context, parserVersion string, cursor int64, limit int) ([]models.MediaFile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+qualify(mediaFileColumns, "mf")+`
		FROM media_files mf
		WHERE mf.missing_at IS NULL AND mf.book_id IS NOT NULL AND `+primaryTextIs+`
		  AND mf.id > ?
		  AND NOT EXISTS (SELECT 1 FROM pdf_files pf
		                    WHERE pf.media_file_id = mf.id
		                      AND pf.classification = 'image-native')
		  AND (
		  	NOT EXISTS (SELECT 1 FROM epub_texts et WHERE et.media_file_id = mf.id)
		  	OR EXISTS (SELECT 1 FROM epub_texts et
			             WHERE et.media_file_id = mf.id AND et.parser_version <> ?)
		  	OR NOT EXISTS (SELECT 1 FROM book_text_fts f WHERE f.media_file_id = mf.id)
		  )
		ORDER BY mf.id
		LIMIT ?`, cursor, parserVersion, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.MediaFile
	for rows.Next() {
		f, err := scanMediaFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
