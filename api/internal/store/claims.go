package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// The claims store (MAD-466): backhog's half of the Arda contract with no
// model. Extractors run outside and write through the import endpoint; the
// rows here are only what survived its deterministic cite-or-drop check.
// The handler owns validation — it holds the canonical text the quotes are
// checked against — and this file owns landing the survivors idempotently
// and serving them back with their versions attached.

// ImportClaims lands one validated batch in a single transaction: claims
// with their versions, plus the entities and aliases the same run named.
//
// Idempotency is the identity columns, not a client token: a claim is its
// evidence plus its statement, a version is its claim plus its reveal plus
// its statement, an entity is its name on that shelf. Re-importing a batch
// is a no-op; re-importing after a re-ingest refreshed the handler's
// chapter hashes, so the conflict path updates them — the heal a stale
// claim needs to come back on its own.
func (s *Store) ImportClaims(ctx context.Context, userID, entryID string, claims []models.BookClaim, entities []models.BookEntity) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for i := range claims {
		c := &claims[i]
		if c.ID == "" {
			c.ID = newID()
		}
		c.UserID, c.Entry = userID, entryID
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO book_claims (id, user_id, entry_id, statement, subject, predicate, object,
			                         char_start, char_end, quote, chapter_index, chapter_hash,
			                         source, source_version)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(entry_id, char_start, char_end, statement) DO UPDATE SET
				quote          = excluded.quote,
				chapter_index  = excluded.chapter_index,
				chapter_hash   = excluded.chapter_hash,
				source         = excluded.source,
				source_version = excluded.source_version
			RETURNING id, created_at`,
			c.ID, userID, entryID, c.Statement, c.Subject, c.Predicate, c.Object,
			c.CharStart, c.CharEnd, c.Quote, c.ChapterIndex, c.ChapterHash,
			c.Source, c.SourceVersion).Scan(&c.ID, &c.CreatedAt); err != nil {
			return fmt.Errorf("import claim: %w", err)
		}
		for vi := range c.Versions {
			v := &c.Versions[vi]
			if v.ID == "" {
				v.ID = newID()
			}
			v.ClaimID = c.ID
			if err := tx.QueryRowContext(ctx, `
				INSERT INTO claim_versions (id, claim_id, statement, char_start, char_end, quote,
				                            chapter_index, chapter_hash, reveal_offset,
				                            source, source_version)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(claim_id, reveal_offset, statement) DO UPDATE SET
					quote          = excluded.quote,
					chapter_index  = excluded.chapter_index,
					chapter_hash   = excluded.chapter_hash,
					source         = excluded.source,
					source_version = excluded.source_version
				RETURNING id, created_at`,
				v.ID, c.ID, v.Statement, v.CharStart, v.CharEnd, v.Quote,
				v.ChapterIndex, v.ChapterHash, v.RevealOffset,
				v.Source, v.SourceVersion).Scan(&v.ID, &v.CreatedAt); err != nil {
				return fmt.Errorf("import claim version: %w", err)
			}
		}
	}

	for i := range entities {
		e := &entities[i]
		if e.ID == "" {
			e.ID = newID()
		}
		e.UserID = userID
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO book_entities (id, user_id, book_id, name, kind)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(user_id, book_id, name) DO UPDATE SET
				kind = excluded.kind
			RETURNING id, created_at`,
			e.ID, userID, e.BookID, e.Name, e.Kind).Scan(&e.ID, &e.CreatedAt); err != nil {
			return fmt.Errorf("import entity: %w", err)
		}
		for _, alias := range e.Aliases {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO entity_aliases (entity_id, alias) VALUES (?, ?)
				ON CONFLICT(entity_id, alias) DO NOTHING`, e.ID, alias); err != nil {
				return fmt.Errorf("import entity alias: %w", err)
			}
		}
	}

	return tx.Commit()
}

// ListBookClaims returns one entry's claims with their versions attached,
// in book order. The clamp, the staleness filter and the version reveal
// rule are policies about positions and live in the handler that owns the
// bound — this is the raw, honest store.
func (s *Store) ListBookClaims(ctx context.Context, userID, entryID string) ([]models.BookClaim, error) {
	claimRows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, entry_id, statement, subject, predicate, object,
		       char_start, char_end, quote, chapter_index, chapter_hash,
		       source, source_version, created_at
		FROM book_claims
		WHERE user_id = ? AND entry_id = ?
		ORDER BY char_start, id`, userID, entryID)
	if err != nil {
		return nil, err
	}
	out := []models.BookClaim{}
	byID := make(map[string]*models.BookClaim)
	for claimRows.Next() {
		var c models.BookClaim
		if err := claimRows.Scan(&c.ID, &c.UserID, &c.Entry, &c.Statement,
			&c.Subject, &c.Predicate, &c.Object,
			&c.CharStart, &c.CharEnd, &c.Quote, &c.ChapterIndex, &c.ChapterHash,
			&c.Source, &c.SourceVersion, &c.CreatedAt); err != nil {
			claimRows.Close()
			return nil, err
		}
		out = append(out, c)
		byID[c.ID] = &out[len(out)-1]
	}
	claimRows.Close()
	if err := claimRows.Err(); err != nil {
		return nil, err
	}

	verRows, err := s.db.QueryContext(ctx, `
		SELECT v.id, v.claim_id, v.statement, v.char_start, v.char_end, v.quote,
		       v.chapter_index, v.chapter_hash, v.reveal_offset,
		       v.source, v.source_version, v.created_at
		FROM claim_versions v
		JOIN book_claims c ON c.id = v.claim_id
		WHERE c.user_id = ? AND c.entry_id = ?
		ORDER BY v.reveal_offset, v.id`, userID, entryID)
	if err != nil {
		return nil, err
	}
	defer verRows.Close()
	for verRows.Next() {
		var v models.ClaimVersion
		if err := verRows.Scan(&v.ID, &v.ClaimID, &v.Statement, &v.CharStart, &v.CharEnd,
			&v.Quote, &v.ChapterIndex, &v.ChapterHash, &v.RevealOffset,
			&v.Source, &v.SourceVersion, &v.CreatedAt); err != nil {
			return nil, err
		}
		if c, ok := byID[v.ClaimID]; ok {
			c.Versions = append(c.Versions, v)
		}
	}
	return out, verRows.Err()
}

// ListBookEntities returns the reader's entities for one book with their
// aliases. Visibility is computed at read time against the clamped claims,
// so this is every entity the shelf holds, current and not.
func (s *Store) ListBookEntities(ctx context.Context, userID, bookID string) ([]models.BookEntity, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, book_id, name, kind, created_at
		FROM book_entities
		WHERE user_id = ? AND book_id = ?
		ORDER BY name`, userID, bookID)
	if err != nil {
		return nil, err
	}
	out := []models.BookEntity{}
	ids := make([]string, 0, 8)
	for rows.Next() {
		var e models.BookEntity
		if err := rows.Scan(&e.ID, &e.UserID, &e.BookID, &e.Name, &e.Kind, &e.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, e)
		ids = append(ids, e.ID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range out {
		aliasRows, err := s.db.QueryContext(ctx, `
			SELECT alias FROM entity_aliases WHERE entity_id = ? ORDER BY alias`, out[i].ID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return nil, err
		}
		for aliasRows.Next() {
			var a string
			if err := aliasRows.Scan(&a); err != nil {
				aliasRows.Close()
				return nil, err
			}
			out[i].Aliases = append(out[i].Aliases, a)
		}
		aliasRows.Close()
		if err := aliasRows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}
