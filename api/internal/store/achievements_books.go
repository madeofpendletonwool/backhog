package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/collinpendleton/backhog/api/internal/achievements"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// bookAchievementSizing is the per-entry projection the book snapshots read:
// the canonical text length of any attached EPUB, the printing's page count
// (the entry's own edition, else the work's earliest one with a count), and
// the three format flags — paper (a physical copy), ebook (an attached EPUB),
// audio (an attached audiobook). The same sizing facts the reading debt
// reasons about, reduced to what the predicates need.
const bookAchievementSizing = `
	COALESCE((SELECT et.char_count FROM epub_texts et
	          JOIN media_files mf ON mf.id = et.media_file_id
	          WHERE mf.book_id = e.book_id AND mf.kind = 'epub'
	          ORDER BY mf.id LIMIT 1), 0),
	COALESCE(ed.page_count,
		(SELECT ed2.page_count FROM book_editions ed2
		 WHERE ed2.book_id = e.book_id AND ed2.page_count IS NOT NULL
		 ORDER BY ed2.published_year, ed2.id LIMIT 1), 0),
	EXISTS(SELECT 1 FROM physical_copies pc WHERE pc.entry_id = e.id),
	EXISTS(SELECT 1 FROM media_files mf2 WHERE mf2.book_id = e.book_id AND mf2.kind = 'epub'),
	EXISTS(SELECT 1 FROM media_files mf3 WHERE mf3.book_id = e.book_id AND mf3.kind = 'audio')`

// evaluateBookEventTx is the books arena's event evaluation: the book
// snapshot (sizing, formats, book-scoped aggregates), then the book and
// arena-agnostic halves of the catalogue. Books are finished and dropped
// through the shared entry-status flow, so the events are the same kinds the
// games arena fires; reading sessions carry no evaluation hook — nothing in
// the book catalogue scores hours.
func evaluateBookEventTx(ctx context.Context, tx *sql.Tx, userID, entryID, kind string, droppedAtFallback *time.Time) ([]unlockStub, error) {
	var e achievements.Entry
	var finishedAt, startedAt sql.NullTime
	var chars int64
	var pages int64
	var paper, ebook, audio bool
	err := tx.QueryRowContext(ctx, `
		SELECT e.status, e.created_at, e.finished_at, e.started_at, `+bookAchievementSizing+`
		FROM library_entries e
		LEFT JOIN book_editions ed ON ed.id = e.edition_id
		WHERE e.user_id = ? AND e.id = ?`, userID, entryID).
		Scan(&e.Status, &e.CreatedAt, &finishedAt, &startedAt,
			&chars, &pages, &paper, &ebook, &audio)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.ID = entryID
	e.MediaType = models.MediaBook
	e.At = time.Now()
	if finishedAt.Valid {
		e.At = finishedAt.Time
	}
	if startedAt.Valid {
		t := startedAt.Time
		e.StartedAt = &t
	}
	// The canonical text is measured rather than reported, so it wins on
	// page counts — the same precedence the reading debt uses.
	switch {
	case chars > 0:
		e.PageCount = int(chars) / charsPerPage
	default:
		e.PageCount = int(pages)
	}
	if paper {
		e.FormatCount++
	}
	if ebook {
		e.FormatCount++
	}
	if audio {
		e.FormatCount++
	}

	e.DropHistory, err = loadDropHistoryTx(ctx, tx, entryID, droppedAtFallback)
	if err != nil {
		return nil, err
	}

	if err := snapshotBookAggregatesTx(ctx, tx, userID, &e); err != nil {
		return nil, err
	}

	return unlockMatchingTx(ctx, tx, userID, entryID, kind, e, models.MediaBook)
}

// snapshotBookAggregatesTx fills the book-scoped user aggregates the book
// predicates read: finished count, the unread pile and its peak, and the
// calendar-year finishes-versus-acquisitions race. Every query is scoped to
// media_type 'book' by hand, exactly as snapshotAggregatesTx scopes to
// 'game' — the two arenas never move each other's counters.
func snapshotBookAggregatesTx(ctx context.Context, tx *sql.Tx, userID string, e *achievements.Entry) error {
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM library_entries WHERE user_id = ? AND media_type = 'book' AND status = 'played'`,
		userID).Scan(&e.PlayedCount); err != nil {
		return err
	}

	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM library_entries WHERE user_id = ? AND media_type = 'book' AND status IN ('backlog','playing')`,
		userID).Scan(&e.UnplayedCount); err != nil {
		return err
	}
	timeline, err := loadUnplayedTimelineTx(ctx, tx, userID, models.MediaBook)
	if err != nil {
		return err
	}
	_, e.PeakUnplayedCount = timeline.stateAt(e.At)

	year := fmt.Sprintf("%04d", e.At.Year())
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM library_entries
		WHERE user_id = ? AND media_type = 'book' AND status = 'played'
		  AND finished_at IS NOT NULL AND strftime('%Y', finished_at) = ?`,
		userID, year).Scan(&e.YearFinishes); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM library_entries
		WHERE user_id = ? AND media_type = 'book' AND status <> 'wishlist'
		  AND strftime('%Y', created_at) = ?`,
		userID, year).Scan(&e.YearAdditions); err != nil {
		return err
	}
	return nil
}

// backfillBookAchievementsTx replays the books arena's history so the
// gallery is complete on first open, the same pass the games backfill makes
// over games. Finishes replay in completion order so the count ladder
// attaches to the book that crossed each line; drops replay after them for
// the drop predicates. There is no resume replay: no book predicate keys on
// resume, and the drop arcs the finish predicates need are already covered
// by loadUserDropArcsTx, which is user-wide. Attachments and physical
// copies are judged from what the library knows today — the same
// approximation the games backfill makes about platform assignments.
func (s *Store) backfillBookAchievementsTx(ctx context.Context, tx *sql.Tx, userID string) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT e.id, e.created_at, COALESCE(e.finished_at, e.created_at), `+bookAchievementSizing+`
		FROM library_entries e
		LEFT JOIN book_editions ed ON ed.id = e.edition_id
		WHERE e.user_id = ? AND e.media_type = 'book' AND e.status = 'played'
		ORDER BY COALESCE(e.finished_at, e.created_at) ASC, e.id`, userID)
	if err != nil {
		return err
	}
	played := []achievements.Entry{}
	for rows.Next() {
		var e achievements.Entry
		var chars, pages int64
		var paper, ebook, audio bool
		var finishedAt string
		if err := rows.Scan(&e.ID, &e.CreatedAt, &finishedAt, &chars, &pages,
			&paper, &ebook, &audio); err != nil {
			rows.Close()
			return err
		}
		e.Status = models.StatusPlayed
		if at, ok := parseDBTime(finishedAt); ok {
			e.At = at
		} else {
			e.At = e.CreatedAt
		}
		if chars > 0 {
			e.PageCount = int(chars) / charsPerPage
		} else {
			e.PageCount = int(pages)
		}
		for _, has := range []bool{paper, ebook, audio} {
			if has {
				e.FormatCount++
			}
		}
		played = append(played, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// The same replay machinery the games pass uses, scoped to books: the
	// unread-pile sweep, the acquisitions walk, and the user-wide drop
	// arcs (which already carry the book entries' histories).
	timeline, err := loadUnplayedTimelineTx(ctx, tx, userID, models.MediaBook)
	if err != nil {
		return err
	}
	additions, err := additionsTx(ctx, tx, userID, models.MediaBook)
	if err != nil {
		return err
	}
	arcs, _, err := loadUserDropArcsTx(ctx, tx, userID, models.MediaBook)
	if err != nil {
		return err
	}
	yearFinishes, yearAdditions := map[int]int{}, map[int]int{}
	ai := 0

	for i := range played {
		played[i].PlayedCount = i + 1
		played[i].MediaType = models.MediaBook
		played[i].DropHistory = arcsUntil(arcs[played[i].ID], played[i].At)
		for ai < len(additions) && !additions[ai].After(played[i].At) {
			yearAdditions[additions[ai].Year()]++
			ai++
		}
		year := played[i].At.Year()
		yearFinishes[year]++
		played[i].YearFinishes = yearFinishes[year]
		played[i].YearAdditions = yearAdditions[year]
		played[i].UnplayedCount, played[i].PeakUnplayedCount = timeline.stateAt(played[i].At)

		if _, err := unlockMatchingTx(ctx, tx, userID, played[i].ID,
			achievements.EventFinished, played[i], models.MediaBook); err != nil {
			return err
		}
	}

	// Dropped books feed the drop predicates — the unread-pile ladder and
	// the honest DNF — replayed in drop order.
	rows, err = tx.QueryContext(ctx, `
		SELECT e.id, e.created_at, COALESCE(e.finished_at, e.created_at), e.started_at
		FROM library_entries e
		WHERE e.user_id = ? AND e.media_type = 'book' AND e.status = 'dropped'
		ORDER BY COALESCE(e.finished_at, e.created_at) ASC, e.id`, userID)
	if err != nil {
		return err
	}
	dropped := []achievements.Entry{}
	for rows.Next() {
		var e achievements.Entry
		var finishedAt string
		var startedAt sql.NullTime
		if err := rows.Scan(&e.ID, &e.CreatedAt, &finishedAt, &startedAt); err != nil {
			rows.Close()
			return err
		}
		e.Status = models.StatusDropped
		if at, ok := parseDBTime(finishedAt); ok {
			e.At = at
		} else {
			e.At = e.CreatedAt
		}
		if startedAt.Valid {
			t := startedAt.Time
			e.StartedAt = &t
		}
		dropped = append(dropped, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for i := range dropped {
		dropped[i].MediaType = models.MediaBook
		dropped[i].UnplayedCount, dropped[i].PeakUnplayedCount = timeline.stateAt(dropped[i].At)
		if _, err := unlockMatchingTx(ctx, tx, userID, dropped[i].ID,
			achievements.EventDropped, dropped[i], models.MediaBook); err != nil {
			return err
		}
	}
	return nil
}
