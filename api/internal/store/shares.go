package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// ErrSelfShare rejects sharing a book with yourself.
var ErrSelfShare = errors.New("cannot share a book with yourself")

// fileAccessRule renders the predicate that decides whether a user may read
// the files attached to a book. It exists as one expression, in one place,
// because it is the only thing standing between two accounts on the same
// server and each other's libraries — and because every text, audio,
// alignment and attach path has to ask exactly the same question.
//
// A user may read a book's files when any of these holds:
//
//  1. Nobody owns them. Either nothing is attached at all — an ordinary
//     book somebody added from Open Library and has not paired with a file
//     — or the rows predate ownership and the migration found no admin to
//     assign them to. There is no one to ask, so nothing is refused.
//
//  2. They attached some of them. Attaching is what ownership means here.
//
//  3. Someone who attached some of them shared the book with them. The
//     second half of that clause matters: the share counts only while the
//     sharer still owns files on the book, so a share can never be used to
//     launder access to files a third account attached.
//
// book is the SQL naming the book — a bound "?" when the id is known, or a
// correlated column such as e.book_id when it is not. The rendered fragment
// takes the user id twice, after whatever book takes.
func fileAccessRule(book string) string {
	return `(
	NOT EXISTS (SELECT 1 FROM media_files mf
	             WHERE mf.book_id = ` + book + ` AND mf.attached_by IS NOT NULL)
	OR EXISTS (SELECT 1 FROM media_files mf
	            WHERE mf.book_id = ` + book + ` AND mf.attached_by = ?)
	OR EXISTS (SELECT 1 FROM book_shares bs
	            WHERE bs.book_id = ` + book + ` AND bs.shared_with_id = ?
	              AND EXISTS (SELECT 1 FROM media_files mf
	                           WHERE mf.book_id = bs.book_id
	                             AND mf.attached_by = bs.owner_id))
)`
}

// CanAccessBookFiles reports whether the user may read and listen to the
// files attached to a book. See fileAccessRule for the rule itself.
func (s *Store) CanAccessBookFiles(ctx context.Context, userID, bookID string) (bool, error) {
	var ok bool
	err := s.db.QueryRowContext(ctx,
		`SELECT `+fileAccessRule("?"),
		bookID, bookID, userID, bookID, userID).Scan(&ok)
	return ok, err
}

// BookFilesForEntry resolves a library entry to its book id and refuses when
// the caller may not read that book's files. It is BookIDForEntry plus the
// access rule, and it is what every file-reading path calls: the canonical
// text, the reader's images, the audio timeline and its tracks, position
// translation, alignment and the attach page.
//
// Denial is ErrNotFound, never a distinct status. A reader who was never
// shared a book should not be able to tell "these files exist and you may
// not have them" from "there are no files here", any more than they can
// tell whose library an entry id belongs to.
//
// The paths that are *not* gated through this are the ones that touch no
// files: reading progress, reading sessions, physical copies and page
// anchors. Those are the user's own rows about their own printing, and they
// keep working on a book whose files were never shared or have gone away.
func (s *Store) BookFilesForEntry(ctx context.Context, userID, entryID string) (string, error) {
	var bookID string
	err := s.db.QueryRowContext(ctx, `
		SELECT e.book_id FROM library_entries e
		WHERE e.id = ? AND e.user_id = ? AND e.media_type = 'book'
		  AND `+fileAccessRule("e.book_id"),
		entryID, userID, userID, userID).Scan(&bookID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return bookID, err
}

// ShareBook grants the recipient access to the files the owner attached to
// the book behind one of the owner's entries. Sharing twice is idempotent.
//
// The owner does not have to have attached anything yet. A share is a
// statement about a person — "he can read my books" — not a snapshot of the
// file table, and making it conditional on the current attachments would
// mean detaching an EPUB to swap in a better copy silently revoked everyone.
// The access rule handles the empty case on its own: a share by someone who
// owns no files on that book grants nothing until they do.
func (s *Store) ShareBook(ctx context.Context, ownerID, entryID, recipientID string) (models.BookShare, error) {
	if ownerID == recipientID {
		return models.BookShare{}, ErrSelfShare
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.BookShare{}, err
	}
	defer tx.Rollback()

	var bookID string
	err = tx.QueryRowContext(ctx, `
		SELECT book_id FROM library_entries
		WHERE id = ? AND user_id = ? AND media_type = 'book'`, entryID, ownerID).Scan(&bookID)
	if errors.Is(err, sql.ErrNoRows) {
		return models.BookShare{}, ErrNotFound
	}
	if err != nil {
		return models.BookShare{}, err
	}

	// The recipient must exist and be able to sign in. Sharing with a
	// suspended account would silently start working again the day it is
	// restored, which is not what "share with him" meant.
	var recipient models.User
	err = tx.QueryRowContext(ctx,
		`SELECT id, username, email FROM users WHERE id = ? AND disabled_at IS NULL`,
		recipientID).Scan(&recipient.ID, &recipient.Username, &recipient.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return models.BookShare{}, ErrNotFound
	}
	if err != nil {
		return models.BookShare{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO book_shares (id, book_id, owner_id, shared_with_id)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (book_id, owner_id, shared_with_id) DO NOTHING`,
		newID(), bookID, ownerID, recipientID); err != nil {
		return models.BookShare{}, err
	}

	share := models.BookShare{
		BookID:   bookID,
		OwnerID:  ownerID,
		UserID:   recipient.ID,
		Username: recipient.Username,
	}
	err = tx.QueryRowContext(ctx, `
		SELECT bs.id, bs.created_at,
		       EXISTS (SELECT 1 FROM library_entries e
		                WHERE e.user_id = bs.shared_with_id AND e.book_id = bs.book_id)
		FROM book_shares bs
		WHERE bs.book_id = ? AND bs.owner_id = ? AND bs.shared_with_id = ?`,
		bookID, ownerID, recipientID).Scan(&share.ID, &share.SharedAt, &share.InLibrary)
	if err != nil {
		return models.BookShare{}, err
	}
	if err := tx.Commit(); err != nil {
		return models.BookShare{}, err
	}
	return share, nil
}

// RevokeShare withdraws one grant. Only the row goes: the recipient keeps
// their library entry, their position and their reading sessions, and the
// book becomes one with nothing attached — a state the app already renders.
// Sharing it again later picks up exactly where they left off.
func (s *Store) RevokeShare(ctx context.Context, ownerID, entryID, recipientID string) error {
	var bookID string
	err := s.db.QueryRowContext(ctx, `
		SELECT book_id FROM library_entries
		WHERE id = ? AND user_id = ? AND media_type = 'book'`, entryID, ownerID).Scan(&bookID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	res, err := s.db.ExecContext(ctx, `
		DELETE FROM book_shares
		WHERE book_id = ? AND owner_id = ? AND shared_with_id = ?`,
		bookID, ownerID, recipientID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ShareCandidatesForEntry lists every account this book could be shared
// with, flagged with whether it already is and whether they have added the
// book to their own shelf. It is the share picker's whole payload.
func (s *Store) ShareCandidatesForEntry(ctx context.Context, ownerID, entryID string) ([]models.ShareCandidate, error) {
	var bookID string
	err := s.db.QueryRowContext(ctx, `
		SELECT book_id FROM library_entries
		WHERE id = ? AND user_id = ? AND media_type = 'book'`, entryID, ownerID).Scan(&bookID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT u.id, u.username, u.role,
		       EXISTS (SELECT 1 FROM book_shares bs
		                WHERE bs.book_id = ? AND bs.owner_id = ? AND bs.shared_with_id = u.id),
		       EXISTS (SELECT 1 FROM library_entries e
		                WHERE e.user_id = u.id AND e.book_id = ?)
		FROM users u
		WHERE u.id != ? AND u.disabled_at IS NULL
		ORDER BY u.username COLLATE NOCASE`,
		bookID, ownerID, bookID, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.ShareCandidate{}
	for rows.Next() {
		var c models.ShareCandidate
		if err := rows.Scan(&c.UserID, &c.Username, &c.Role, &c.Shared, &c.InLibrary); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SharesByOwner lists everything this user has shared out, newest first —
// the "who has my books" half of the sharing panel.
func (s *Store) SharesByOwner(ctx context.Context, ownerID string) ([]models.BookShare, error) {
	return s.shares(ctx, `
		SELECT bs.id, bs.book_id, COALESCE(b.title, ''), bs.owner_id, COALESCE(o.username, ''),
		       bs.shared_with_id, r.username, r.email, bs.created_at,
		       EXISTS (SELECT 1 FROM library_entries e
		                WHERE e.user_id = bs.shared_with_id AND e.book_id = bs.book_id)
		FROM book_shares bs
		JOIN users r ON r.id = bs.shared_with_id
		LEFT JOIN users o ON o.id = bs.owner_id
		LEFT JOIN books b ON b.id = bs.book_id
		WHERE bs.owner_id = ?
		ORDER BY bs.created_at DESC`, ownerID)
}

// SharesWithUser lists everything shared with this user — their "shared with
// me" shelf. InLibrary says whether they have added it yet; a share grants
// access but never reaches into someone else's library to put rows in it.
func (s *Store) SharesWithUser(ctx context.Context, userID string) ([]models.BookShare, error) {
	return s.shares(ctx, `
		SELECT bs.id, bs.book_id, COALESCE(b.title, ''), bs.owner_id, COALESCE(o.username, ''),
		       bs.shared_with_id, r.username, '', bs.created_at,
		       EXISTS (SELECT 1 FROM library_entries e
		                WHERE e.user_id = bs.shared_with_id AND e.book_id = bs.book_id)
		FROM book_shares bs
		JOIN users r ON r.id = bs.shared_with_id
		LEFT JOIN users o ON o.id = bs.owner_id
		LEFT JOIN books b ON b.id = bs.book_id
		WHERE bs.shared_with_id = ?
		ORDER BY bs.created_at DESC`, userID)
}

func (s *Store) shares(ctx context.Context, query string, args ...any) ([]models.BookShare, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.BookShare{}
	for rows.Next() {
		var sh models.BookShare
		if err := rows.Scan(&sh.ID, &sh.BookID, &sh.BookTitle, &sh.OwnerID, &sh.OwnerName,
			&sh.UserID, &sh.Username, &sh.UserEmail, &sh.SharedAt, &sh.InLibrary); err != nil {
			return nil, err
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}

// hydrateSharedBy fills Entry.SharedBy on the book entries whose files
// belong to somebody else. It is one query for the whole page rather than
// one per row, and it runs only on the library listing — the badge is a
// listing affordance, not a fact the entry projection owes every caller.
func (s *Store) hydrateSharedBy(ctx context.Context, userID string, entries []models.Entry) error {
	wanted := map[string]bool{}
	for i := range entries {
		if entries[i].Book != nil {
			wanted[entries[i].Book.ID] = true
		}
	}
	if len(wanted) == 0 {
		return nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT bs.book_id, o.username
		FROM book_shares bs
		JOIN users o ON o.id = bs.owner_id
		WHERE bs.shared_with_id = ?
		  AND EXISTS (SELECT 1 FROM media_files mf
		               WHERE mf.book_id = bs.book_id AND mf.attached_by = bs.owner_id)
		  AND NOT EXISTS (SELECT 1 FROM media_files mf
		                   WHERE mf.book_id = bs.book_id AND mf.attached_by = ?)`,
		userID, userID)
	if err != nil {
		return err
	}
	defer rows.Close()

	owners := map[string]string{}
	for rows.Next() {
		var bookID, owner string
		if err := rows.Scan(&bookID, &owner); err != nil {
			return err
		}
		if wanted[bookID] {
			owners[bookID] = owner
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for i := range entries {
		if entries[i].Book == nil {
			continue
		}
		if owner, ok := owners[entries[i].Book.ID]; ok {
			entries[i].SharedBy = owner
		}
	}
	return nil
}
