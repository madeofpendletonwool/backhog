package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// CreateList creates a manual or smart list. Smart rules are validated here so
// an invalid list can never be persisted.
func (s *Store) CreateList(ctx context.Context, userID, name, description, kind string, rules *models.RuleSet) (models.List, error) {
	if kind != "manual" && kind != "smart" {
		return models.List{}, fmt.Errorf("kind must be 'manual' or 'smart'")
	}
	if strings.TrimSpace(name) == "" {
		return models.List{}, fmt.Errorf("name is required")
	}

	var rulesJSON *string
	if kind == "smart" {
		if rules == nil {
			return models.List{}, fmt.Errorf("smart lists require rules")
		}
		if err := ValidateRuleSet(*rules); err != nil {
			return models.List{}, err
		}
		encoded, err := json.Marshal(rules)
		if err != nil {
			return models.List{}, err
		}
		s := string(encoded)
		rulesJSON = &s
	}

	l := models.List{ID: newID(), Name: name, Description: description, Kind: kind, Rules: rules}
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO lists (id, user_id, name, description, kind, rules_json)
		VALUES (?, ?, ?, ?, ?, ?) RETURNING created_at`,
		l.ID, userID, name, description, kind, rulesJSON).Scan(&l.CreatedAt)
	if err != nil {
		return models.List{}, err
	}
	return l, nil
}

// GetLists returns a user's lists with their current sizes. Smart list counts
// are computed live, which is why this is not a single query.
func (s *Store) GetLists(ctx context.Context, userID string) ([]models.List, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT l.id, l.name, l.description, l.kind, l.rules_json, l.created_at,
		       (SELECT COUNT(*) FROM list_items li WHERE li.list_id = l.id)
		FROM lists l WHERE l.user_id = ?
		ORDER BY l.kind DESC, l.created_at ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	lists := []models.List{}
	for rows.Next() {
		l, manualCount, err := scanList(rows)
		if err != nil {
			return nil, err
		}
		l.Count = manualCount
		lists = append(lists, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range lists {
		if lists[i].Kind != "smart" || lists[i].Rules == nil {
			continue
		}
		count, err := s.countSmart(ctx, userID, *lists[i].Rules)
		if err != nil {
			// A broken saved rule set should not blank the whole sidebar.
			lists[i].Count = 0
			continue
		}
		lists[i].Count = count
	}
	return lists, nil
}

// GetList returns one list, scoped to its owner.
func (s *Store) GetList(ctx context.Context, userID, listID string) (models.List, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT l.id, l.name, l.description, l.kind, l.rules_json, l.created_at,
		       (SELECT COUNT(*) FROM list_items li WHERE li.list_id = l.id)
		FROM lists l WHERE l.user_id = ? AND l.id = ?`, userID, listID)

	l, manualCount, err := scanList(row)
	if errors.Is(err, sql.ErrNoRows) {
		return models.List{}, ErrNotFound
	}
	if err != nil {
		return models.List{}, err
	}
	l.Count = manualCount
	return l, nil
}

// UpdateList renames a list or replaces its rules.
func (s *Store) UpdateList(ctx context.Context, userID, listID string, name, description *string, rules *models.RuleSet) (models.List, error) {
	existing, err := s.GetList(ctx, userID, listID)
	if err != nil {
		return models.List{}, err
	}

	sets := []string{}
	args := []any{}
	if name != nil {
		if strings.TrimSpace(*name) == "" {
			return models.List{}, fmt.Errorf("name is required")
		}
		sets = append(sets, "name = ?")
		args = append(args, *name)
	}
	if description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *description)
	}
	if rules != nil {
		if existing.Kind != "smart" {
			return models.List{}, fmt.Errorf("only smart lists have rules")
		}
		if err := ValidateRuleSet(*rules); err != nil {
			return models.List{}, err
		}
		encoded, err := json.Marshal(rules)
		if err != nil {
			return models.List{}, err
		}
		sets = append(sets, "rules_json = ?")
		args = append(args, string(encoded))
	}
	if len(sets) == 0 {
		return existing, nil
	}

	args = append(args, userID, listID)
	if _, err := s.db.ExecContext(ctx,
		`UPDATE lists SET `+strings.Join(sets, ", ")+` WHERE user_id = ? AND id = ?`, args...); err != nil {
		return models.List{}, err
	}
	return s.GetList(ctx, userID, listID)
}

// DeleteList removes a list. Its items cascade; the library entries survive.
func (s *Store) DeleteList(ctx context.Context, userID, listID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM lists WHERE user_id = ? AND id = ?`, userID, listID)
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

// ListEntriesFor resolves a list to its entries: stored membership for manual
// lists, a live query for smart lists.
func (s *Store) ListEntriesFor(ctx context.Context, userID, listID string) ([]models.Entry, error) {
	l, err := s.GetList(ctx, userID, listID)
	if err != nil {
		return nil, err
	}
	if l.Kind == "smart" {
		if l.Rules == nil {
			return []models.Entry{}, nil
		}
		return s.evaluateSmart(ctx, userID, *l.Rules)
	}
	return s.queryEntries(ctx, entrySelect+`
		JOIN list_items li ON li.entry_id = e.id
		WHERE e.user_id = ? AND li.list_id = ?
		ORDER BY li.position ASC`, userID, listID)
}

// evaluateSmart runs a smart list's rules against the user's library.
func (s *Store) evaluateSmart(ctx context.Context, userID string, rs models.RuleSet) ([]models.Entry, error) {
	where, args, err := compileRules(rs)
	if err != nil {
		return nil, err
	}

	orderBy := "e.created_at DESC"
	if rs.Sort != nil {
		col, ok := smartSorts[rs.Sort.Field]
		if !ok {
			return nil, fmt.Errorf("unknown sort field %q", rs.Sort.Field)
		}
		dir := "ASC"
		if strings.EqualFold(rs.Sort.Dir, "desc") {
			dir = "DESC"
		}
		orderBy = col + " " + dir + " NULLS LAST"
	}

	limit := rs.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}

	query := entrySelect + " WHERE e.user_id = ? AND " + where +
		" ORDER BY " + orderBy + " LIMIT ?"
	full := append([]any{userID}, args...)
	full = append(full, limit)

	return s.queryEntries(ctx, query, full...)
}

func (s *Store) countSmart(ctx context.Context, userID string, rs models.RuleSet) (int, error) {
	where, args, err := compileRules(rs)
	if err != nil {
		return 0, err
	}
	query := `SELECT COUNT(*) FROM library_entries e JOIN games g ON g.id = e.game_id
	          WHERE e.user_id = ? AND ` + where
	var count int
	err = s.db.QueryRowContext(ctx, query, append([]any{userID}, args...)...).Scan(&count)
	return count, err
}

// ListIDsForEntry returns the ids of the manual lists containing an entry, so
// the UI can show membership without loading every list's contents.
func (s *Store) ListIDsForEntry(ctx context.Context, userID, entryID string) ([]string, error) {
	// Join through lists to scope by owner: a bare list_items lookup would
	// happily report membership of another user's list.
	rows, err := s.db.QueryContext(ctx, `
		SELECT li.list_id
		FROM list_items li
		JOIN lists l ON l.id = li.list_id
		JOIN library_entries e ON e.id = li.entry_id
		WHERE l.user_id = ? AND e.user_id = ? AND li.entry_id = ?`,
		userID, userID, entryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// AddListItem appends an entry to a manual list. Both the list and the entry
// must belong to the caller.
func (s *Store) AddListItem(ctx context.Context, userID, listID, entryID string) error {
	l, err := s.GetList(ctx, userID, listID)
	if err != nil {
		return err
	}
	if l.Kind != "manual" {
		return fmt.Errorf("cannot add items to a smart list")
	}
	if _, err := s.GetEntry(ctx, userID, entryID); err != nil {
		return err
	}

	var maxPos sql.NullFloat64
	if err := s.db.QueryRowContext(ctx,
		`SELECT MAX(position) FROM list_items WHERE list_id = ?`, listID).Scan(&maxPos); err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO list_items (list_id, entry_id, position) VALUES (?, ?, ?)
		 ON CONFLICT DO NOTHING`, listID, entryID, nextAfter(maxPos))
	return err
}

// RemoveListItem detaches an entry from a manual list.
func (s *Store) RemoveListItem(ctx context.Context, userID, listID, entryID string) error {
	if _, err := s.GetList(ctx, userID, listID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM list_items WHERE list_id = ? AND entry_id = ?`, listID, entryID)
	return err
}

// MoveListItem repositions an entry within a manual list, using the same
// fractional-index scheme as the play queue.
func (s *Store) MoveListItem(ctx context.Context, userID, listID, entryID, beforeID, afterID string) error {
	if _, err := s.GetList(ctx, userID, listID); err != nil {
		return err
	}

	before, err := s.listItemPosition(ctx, listID, beforeID)
	if err != nil {
		return err
	}
	after, err := s.listItemPosition(ctx, listID, afterID)
	if err != nil {
		return err
	}

	pos, err := midpoint(before, after)
	if errors.Is(err, errNeedsRenormalize) {
		if err := s.renormalizeList(ctx, listID); err != nil {
			return err
		}
		return s.MoveListItem(ctx, userID, listID, entryID, beforeID, afterID)
	}
	if err != nil {
		return err
	}

	res, err := s.db.ExecContext(ctx,
		`UPDATE list_items SET position = ? WHERE list_id = ? AND entry_id = ?`, pos, listID, entryID)
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

func (s *Store) listItemPosition(ctx context.Context, listID, entryID string) (*float64, error) {
	if entryID == "" {
		return nil, nil
	}
	var pos float64
	err := s.db.QueryRowContext(ctx,
		`SELECT position FROM list_items WHERE list_id = ? AND entry_id = ?`, listID, entryID).Scan(&pos)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &pos, nil
}

func (s *Store) renormalizeList(ctx context.Context, listID string) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT entry_id FROM list_items WHERE list_id = ? ORDER BY position ASC`, listID)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx,
			`UPDATE list_items SET position = ? WHERE list_id = ? AND entry_id = ?`,
			float64(i+1)*positionGap, listID, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// scanner covers both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanList(sc scanner) (models.List, int, error) {
	var l models.List
	var rulesJSON sql.NullString
	var count int
	if err := sc.Scan(&l.ID, &l.Name, &l.Description, &l.Kind, &rulesJSON, &l.CreatedAt, &count); err != nil {
		return models.List{}, 0, err
	}
	if rulesJSON.Valid && rulesJSON.String != "" {
		var rs models.RuleSet
		if err := json.Unmarshal([]byte(rulesJSON.String), &rs); err == nil {
			l.Rules = &rs
		}
	}
	return l, count, nil
}

type defaultList struct {
	name, description string
	rules             models.RuleSet
}

// defaultListRuleSets defines the smart lists every new account starts with.
func defaultListRuleSets() []defaultList {
	return []defaultList{
		{
			"Quick Wins", "Backlog games you could finish this weekend",
			models.RuleSet{
				Match: "all",
				Rules: []models.Rule{
					{Field: "status", Op: "eq", Value: models.StatusBacklog},
					{Field: "hours_to_beat", Op: "lt", Value: 8.0},
				},
				Sort: &models.Sort{Field: "hours_to_beat", Dir: "asc"},
			},
		},
		{
			"Unplayed Gems", "Highly rated games you have never started",
			models.RuleSet{
				Match: "all",
				Rules: []models.Rule{
					{Field: "status", Op: "eq", Value: models.StatusBacklog},
					{Field: "igdb_rating", Op: "gt", Value: 85.0},
				},
				Sort: &models.Sort{Field: "igdb_rating", Dir: "desc"},
			},
		},
		{
			"Stalled", "Started over a month ago and still going",
			models.RuleSet{
				Match: "all",
				Rules: []models.Rule{
					{Field: "status", Op: "eq", Value: models.StatusPlaying},
					{Field: "days_since_started", Op: "gt", Value: 30.0},
				},
				Sort: &models.Sort{Field: "updated", Dir: "asc"},
			},
		},
		{
			"Gathering Dust", "In the backlog for more than six months",
			models.RuleSet{
				Match: "all",
				Rules: []models.Rule{
					{Field: "status", Op: "eq", Value: models.StatusBacklog},
					{Field: "days_since_added", Op: "gt", Value: 180.0},
				},
				Sort: &models.Sort{Field: "added", Dir: "asc"},
			},
		},
	}
}

// SeedDefaultLists gives a new account a useful set of smart lists so the app
// has something to show before the user has built any of their own.
func (s *Store) SeedDefaultLists(ctx context.Context, userID string) error {
	for _, d := range defaultListRuleSets() {
		if _, err := s.CreateList(ctx, userID, d.name, d.description, "smart", &d.rules); err != nil {
			return fmt.Errorf("seed list %q: %w", d.name, err)
		}
	}
	return nil
}
