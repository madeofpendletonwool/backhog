package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// ProjectUpdate carries the mutable fields of a project. Each field is
// optional; nil means "leave alone". ClearTarget distinguishes "set no target"
// from "don't touch the target" — the same distinction the entry PATCH makes.
type ProjectUpdate struct {
	Name        *string
	Description *string
	TargetCount *int
	ClearTarget bool
	Rules       *models.RuleSet
	// Completed manually closes (true) or reopens (false) the project.
	Completed *bool
}

// CreateProject creates a project of any kind. Kind-specific requirements are
// enforced here so an invalid project can never be persisted. mediaScope is
// the arena the project lives in ('game' or 'book'); empty defaults to game,
// matching the column default pre-existing rows carry.
func (s *Store) CreateProject(ctx context.Context, userID, name, description, kind, mediaScope string, targetCount *int, rules *models.RuleSet) (models.Project, error) {
	if !models.ValidProjectKind(kind) {
		return models.Project{}, fmt.Errorf("kind must be 'checklist', 'count_goal' or 'rule_goal'")
	}
	if strings.TrimSpace(name) == "" {
		return models.Project{}, fmt.Errorf("name is required")
	}
	if mediaScope == "" {
		mediaScope = models.MediaGame
	}
	if !models.ValidMediaType(mediaScope) {
		return models.Project{}, fmt.Errorf("media must be 'game' or 'book'")
	}

	switch kind {
	case models.ProjectCountGoal:
		if targetCount == nil || *targetCount <= 0 {
			return models.Project{}, fmt.Errorf("count goals require a positive target_count")
		}
		rules = nil
	case models.ProjectRuleGoal:
		if rules == nil {
			return models.Project{}, fmt.Errorf("rule goals require rules")
		}
		if err := ValidateRuleSet(*rules); err != nil {
			return models.Project{}, err
		}
		if targetCount != nil && *targetCount <= 0 {
			return models.Project{}, fmt.Errorf("target_count must be positive")
		}
	default: // checklist: the member list is the target
		targetCount = nil
		rules = nil
	}

	var rulesJSON *string
	if rules != nil {
		encoded, err := json.Marshal(rules)
		if err != nil {
			return models.Project{}, err
		}
		str := string(encoded)
		rulesJSON = &str
	}

	p := models.Project{ID: newID(), Name: name, Description: description, Kind: kind, MediaScope: mediaScope, TargetCount: targetCount, Rules: rules}
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO projects (id, user_id, name, description, kind, media_scope, target_count, rules_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING created_at`,
		p.ID, userID, name, description, kind, mediaScope, targetCount, rulesJSON).Scan(&p.CreatedAt)
	if err != nil {
		return models.Project{}, err
	}
	return s.projectWithProgress(ctx, userID, p.ID)
}

// GetProjects returns a user's projects with computed progress. Reading a
// project whose target has been met stamps its completed_at, so completion is
// recorded even if it happened as a side effect of a status change elsewhere.
func (s *Store) GetProjects(ctx context.Context, userID string) ([]models.Project, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, description, kind, media_scope, target_count, rules_json, created_at, completed_at
		FROM projects WHERE user_id = ?
		ORDER BY completed_at IS NOT NULL, created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	bare := []models.Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		bare = append(bare, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	projects := make([]models.Project, 0, len(bare))
	for _, p := range bare {
		full, err := s.projectWithProgress(ctx, userID, p.ID)
		if err != nil {
			return nil, err
		}
		projects = append(projects, full)
	}
	return projects, nil
}

// GetProject returns one project, scoped to its owner.
func (s *Store) GetProject(ctx context.Context, userID, projectID string) (models.Project, error) {
	return s.projectWithProgress(ctx, userID, projectID)
}

// UpdateProject renames a project, retargets it, replaces its rules, or
// manually closes / reopens it.
func (s *Store) UpdateProject(ctx context.Context, userID, projectID string, u ProjectUpdate) (models.Project, error) {
	existing, err := s.projectRow(ctx, userID, projectID)
	if err != nil {
		return models.Project{}, err
	}

	sets := []string{}
	args := []any{}
	if u.Name != nil {
		if strings.TrimSpace(*u.Name) == "" {
			return models.Project{}, fmt.Errorf("name is required")
		}
		sets = append(sets, "name = ?")
		args = append(args, *u.Name)
	}
	if u.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *u.Description)
	}

	switch {
	case u.ClearTarget:
		if existing.Kind == models.ProjectCountGoal {
			return models.Project{}, fmt.Errorf("count goals require a target_count")
		}
		sets = append(sets, "target_count = NULL")
	case u.TargetCount != nil:
		if *u.TargetCount <= 0 {
			return models.Project{}, fmt.Errorf("target_count must be positive")
		}
		sets = append(sets, "target_count = ?")
		args = append(args, *u.TargetCount)
	}

	if u.Rules != nil {
		if existing.Kind != models.ProjectRuleGoal {
			return models.Project{}, fmt.Errorf("only rule goals have rules")
		}
		if err := ValidateRuleSet(*u.Rules); err != nil {
			return models.Project{}, err
		}
		encoded, err := json.Marshal(u.Rules)
		if err != nil {
			return models.Project{}, err
		}
		sets = append(sets, "rules_json = ?")
		args = append(args, string(encoded))
	}

	if u.Completed != nil {
		if *u.Completed {
			if existing.CompletedAt == nil {
				sets = append(sets, "completed_at = CURRENT_TIMESTAMP")
			}
		} else {
			sets = append(sets, "completed_at = NULL")
		}
	}

	if len(sets) > 0 {
		args = append(args, userID, projectID)
		if _, err := s.db.ExecContext(ctx,
			`UPDATE projects SET `+strings.Join(sets, ", ")+` WHERE user_id = ? AND id = ?`, args...); err != nil {
			return models.Project{}, err
		}
	}
	return s.GetProject(ctx, userID, projectID)
}

// DeleteProject removes a project. Its items cascade; the library entries
// survive.
func (s *Store) DeleteProject(ctx context.Context, userID, projectID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM projects WHERE user_id = ? AND id = ?`, userID, projectID)
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

// ProjectItemsFor resolves a project to its item view: stored members with
// their done overrides for checklists, the live match pool for rule goals,
// nothing for count goals (every finished entry in the project's arena counts,
// not a fixed set).
func (s *Store) ProjectItemsFor(ctx context.Context, userID, projectID string) ([]models.ProjectItem, error) {
	p, err := s.projectRow(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}

	if p.Kind == models.ProjectRuleGoal && p.Rules != nil {
		entries, err := s.evaluateSmartInMedia(ctx, userID, *p.Rules, p.MediaScope)
		if err != nil {
			return nil, err
		}
		items := make([]models.ProjectItem, 0, len(entries))
		for _, e := range entries {
			items = append(items, models.ProjectItem{Entry: e})
		}
		return items, nil
	}
	if p.Kind != models.ProjectChecklist {
		return []models.ProjectItem{}, nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT entry_id, done_override FROM project_items
		WHERE project_id = ? ORDER BY position ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := []string{}
	overrides := map[string]*bool{}
	for rows.Next() {
		var id string
		var done sql.NullInt64
		if err := rows.Scan(&id, &done); err != nil {
			return nil, err
		}
		ids = append(ids, id)
		if done.Valid {
			v := done.Int64 == 1
			overrides[id] = &v
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []models.ProjectItem{}, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	full := append([]any{userID}, toAny(ids)...)
	entries, err := s.queryEntries(ctx, entrySelect+`
		WHERE e.user_id = ? AND e.id IN (`+placeholders+`)`, full...)
	if err != nil {
		return nil, err
	}

	byID := make(map[string]models.Entry, len(entries))
	for _, e := range entries {
		byID[e.ID] = e
	}
	items := make([]models.ProjectItem, 0, len(ids))
	for _, id := range ids {
		e, ok := byID[id]
		if !ok {
			continue // a deleted entry leaves a dangling row until cascaded; skip it
		}
		items = append(items, models.ProjectItem{Entry: e, Done: overrides[id]})
	}
	return items, nil
}

// AddProjectItem appends an entry to a checklist project. Both the project and
// the entry must belong to the caller, and the entry must live in the
// project's arena — a book project's checklist is books, full stop.
func (s *Store) AddProjectItem(ctx context.Context, userID, projectID, entryID string) error {
	p, err := s.projectRow(ctx, userID, projectID)
	if err != nil {
		return err
	}
	if p.Kind != models.ProjectChecklist {
		return fmt.Errorf("only checklist projects have members")
	}
	entry, err := s.GetEntry(ctx, userID, entryID)
	if err != nil {
		return err
	}
	if entry.MediaType != p.MediaScope {
		return fmt.Errorf("this project is scoped to %ss", p.MediaScope)
	}

	var maxPos sql.NullFloat64
	if err := s.db.QueryRowContext(ctx,
		`SELECT MAX(position) FROM project_items WHERE project_id = ?`, projectID).Scan(&maxPos); err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO project_items (project_id, entry_id, position) VALUES (?, ?, ?)
		 ON CONFLICT DO NOTHING`, projectID, entryID, nextAfter(maxPos))
	return err
}

// RemoveProjectItem detaches an entry from a checklist project.
func (s *Store) RemoveProjectItem(ctx context.Context, userID, projectID, entryID string) error {
	if _, err := s.projectRow(ctx, userID, projectID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM project_items WHERE project_id = ? AND entry_id = ?`, projectID, entryID)
	return err
}

// SetProjectItemDone sets or clears the manual done override on a checklist
// member. A nil done returns the item to status-derived completion.
func (s *Store) SetProjectItemDone(ctx context.Context, userID, projectID, entryID string, done *bool) error {
	p, err := s.projectRow(ctx, userID, projectID)
	if err != nil {
		return err
	}
	if p.Kind != models.ProjectChecklist {
		return fmt.Errorf("only checklist projects have members")
	}

	var value any
	if done != nil {
		value = *done
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE project_items SET done_override = ? WHERE project_id = ? AND entry_id = ?`,
		value, projectID, entryID)
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

// MoveProjectItem repositions an entry within a checklist project, using the
// same fractional-index scheme as the play queue.
func (s *Store) MoveProjectItem(ctx context.Context, userID, projectID, entryID, beforeID, afterID string) error {
	p, err := s.projectRow(ctx, userID, projectID)
	if err != nil {
		return err
	}
	if p.Kind != models.ProjectChecklist {
		return fmt.Errorf("only checklist projects have members")
	}

	before, err := s.projectItemPosition(ctx, projectID, beforeID)
	if err != nil {
		return err
	}
	after, err := s.projectItemPosition(ctx, projectID, afterID)
	if err != nil {
		return err
	}

	pos, err := midpoint(before, after)
	if errors.Is(err, errNeedsRenormalize) {
		if err := s.renormalizeProject(ctx, projectID); err != nil {
			return err
		}
		return s.MoveProjectItem(ctx, userID, projectID, entryID, beforeID, afterID)
	}
	if err != nil {
		return err
	}

	res, err := s.db.ExecContext(ctx,
		`UPDATE project_items SET position = ? WHERE project_id = ? AND entry_id = ?`, pos, projectID, entryID)
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

// ProjectIDsForEntry returns the ids of the checklist projects containing an
// entry, so the UI can show membership without loading every project.
func (s *Store) ProjectIDsForEntry(ctx context.Context, userID, entryID string) ([]string, error) {
	// Join through projects to scope by owner: a bare project_items lookup
	// would happily report membership of another user's project.
	rows, err := s.db.QueryContext(ctx, `
		SELECT pi.project_id
		FROM project_items pi
		JOIN projects p ON p.id = pi.project_id
		JOIN library_entries e ON e.id = pi.entry_id
		WHERE p.user_id = ? AND e.user_id = ? AND pi.entry_id = ?`,
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

// --- progress ----------------------------------------------------------

// projectRow loads the stored row without computing progress or stamping
// completion. Mutation paths validate against this: adding a played game to a
// one-member checklist must not "complete" the project as a side effect.
func (s *Store) projectRow(ctx context.Context, userID, projectID string) (models.Project, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, description, kind, media_scope, target_count, rules_json, created_at, completed_at
		FROM projects WHERE user_id = ? AND id = ?`, userID, projectID)
	p, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Project{}, ErrNotFound
	}
	return p, err
}

// projectWithProgress loads a project row and fills in its progress,
// stamping completed_at if the target has just been met.
func (s *Store) projectWithProgress(ctx context.Context, userID, projectID string) (models.Project, error) {
	p, err := s.projectRow(ctx, userID, projectID)
	if err != nil {
		return models.Project{}, err
	}

	progress, err := s.computeProgress(ctx, userID, p)
	if err != nil {
		// A broken saved rule set should not blank the whole page; report the
		// project with zeroed progress instead.
		progress = models.ProjectProgress{}
	}
	p.Progress = progress

	// Auto-stamp: the target was met and nobody has recorded it yet.
	if p.CompletedAt == nil && progress.TargetCount > 0 && progress.CompletedCount >= progress.TargetCount {
		now := nowUTC()
		if _, err := s.db.ExecContext(ctx,
			`UPDATE projects SET completed_at = ? WHERE user_id = ? AND id = ? AND completed_at IS NULL`,
			now, userID, projectID); err == nil {
			p.CompletedAt = &now
		}
	}
	return p, nil
}

func (s *Store) computeProgress(ctx context.Context, userID string, p models.Project) (models.ProjectProgress, error) {
	switch p.Kind {
	case models.ProjectChecklist:
		return s.checklistProgress(ctx, p.ID)
	case models.ProjectCountGoal:
		return s.countGoalProgress(ctx, userID, p.MediaScope, p.TargetCount)
	case models.ProjectRuleGoal:
		if p.Rules == nil {
			return models.ProjectProgress{}, fmt.Errorf("rule goal has no rules")
		}
		return s.ruleGoalProgress(ctx, userID, p.MediaScope, p.TargetCount, *p.Rules)
	}
	return models.ProjectProgress{}, fmt.Errorf("unknown project kind %q", p.Kind)
}

// itemDone is the per-member completion expression: the manual override when
// set, otherwise derived from the entry's status. 'played' is the finished
// status in both arenas — a read book is a played entry.
const itemDone = `COALESCE(pi.done_override, e.status = 'played')`

func (s *Store) checklistProgress(ctx context.Context, projectID string) (models.ProjectProgress, error) {
	var pr models.ProjectProgress
	// LEFT JOIN games: a checklist may hold book members, and only games carry
	// a time-to-beat — a book contributes to the counts and zero to the hours.
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(`+itemDone+`), 0),
		       COALESCE(SUM(g.time_to_beat_main), 0),
		       COALESCE(SUM(CASE WHEN `+itemDone+` THEN g.time_to_beat_main ELSE 0 END), 0)
		FROM project_items pi
		JOIN library_entries e ON e.id = pi.entry_id
		LEFT JOIN games g ON g.id = e.game_id
		WHERE pi.project_id = ?`, projectID).
		Scan(&pr.TargetCount, &pr.CompletedCount, &pr.EstHoursTotal, &pr.EstHoursDone)
	if err != nil {
		return models.ProjectProgress{}, err
	}
	finalizeProgress(&pr)
	return pr, nil
}

// countGoalProgress measures against the owned library of the project's
// arena: finished entries count as done, backlog + in-progress are the
// remaining candidates — "finish N games" and "finish N books" are the same
// query with a different media_type.
func (s *Store) countGoalProgress(ctx context.Context, userID, media string, target *int) (models.ProjectProgress, error) {
	if target == nil {
		return models.ProjectProgress{}, fmt.Errorf("count goal has no target")
	}
	if media == "" {
		media = models.MediaGame
	}
	pr := models.ProjectProgress{TargetCount: *target}

	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(e.status = 'played'), 0),
		       COALESCE(SUM(g.time_to_beat_main), 0),
		       COALESCE(SUM(CASE WHEN e.status = 'played' THEN g.time_to_beat_main ELSE 0 END), 0)
		FROM library_entries e LEFT JOIN games g ON g.id = e.game_id
		WHERE e.user_id = ? AND e.media_type = ? AND e.status IN ('backlog','playing','played')`, userID, media).
		Scan(&pr.CompletedCount, &pr.EstHoursTotal, &pr.EstHoursDone)
	if err != nil {
		return models.ProjectProgress{}, err
	}
	finalizeProgress(&pr)
	return pr, nil
}

// ruleGoalProgress evaluates the rule set live: the match pool is the target
// set, and matching entries already finished count as done. Without an
// explicit target_count the goal is to finish the whole pool. The pool is
// constrained to the project's arena — the rules alone must not be able to
// pull the other arena's entries into a book project's target.
func (s *Store) ruleGoalProgress(ctx context.Context, userID, media string, target *int, rs models.RuleSet) (models.ProjectProgress, error) {
	if media == "" {
		media = models.MediaGame
	}
	where, args, err := compileRules(rs)
	if err != nil {
		return models.ProjectProgress{}, err
	}

	var poolCount int
	var poolHours float64
	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(g.time_to_beat_main), 0)
		FROM library_entries e LEFT JOIN games g ON g.id = e.game_id
		WHERE e.user_id = ? AND e.media_type = ? AND `+where,
		append([]any{userID, media}, args...)...).
		Scan(&poolCount, &poolHours)
	if err != nil {
		return models.ProjectProgress{}, err
	}

	var doneCount int
	var doneHours, totalHours float64
	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(g.time_to_beat_main), 0),
		       COALESCE(SUM(CASE WHEN e.status = 'played' THEN g.time_to_beat_main ELSE 0 END), 0)
		FROM library_entries e LEFT JOIN games g ON g.id = e.game_id
		WHERE e.user_id = ? AND e.media_type = ? AND e.status = 'played' AND `+where,
		append([]any{userID, media}, args...)...).
		Scan(&doneCount, &totalHours, &doneHours)
	if err != nil {
		return models.ProjectProgress{}, err
	}

	pr := models.ProjectProgress{
		CompletedCount: doneCount,
		EstHoursTotal:  poolHours,
		EstHoursDone:   doneHours,
	}
	if target != nil {
		pr.TargetCount = *target
	} else {
		pr.TargetCount = poolCount
	}
	finalizeProgress(&pr)
	return pr, nil
}

func finalizeProgress(pr *models.ProjectProgress) {
	pr.EstHoursTotal /= 3600
	pr.EstHoursDone /= 3600
	pr.EstHoursRemaining = math.Max(0, pr.EstHoursTotal-pr.EstHoursDone)
	if pr.TargetCount > 0 {
		pr.Percent = math.Min(100, float64(pr.CompletedCount)/float64(pr.TargetCount)*100)
		pr.Percent = math.Round(pr.Percent*10) / 10
	}
}

func scanProject(sc scanner) (models.Project, error) {
	var p models.Project
	var target sql.NullInt64
	var rulesJSON sql.NullString
	var completed sql.NullTime
	if err := sc.Scan(&p.ID, &p.Name, &p.Description, &p.Kind, &p.MediaScope, &target, &rulesJSON, &p.CreatedAt, &completed); err != nil {
		return models.Project{}, err
	}
	if p.MediaScope == "" {
		p.MediaScope = models.MediaGame
	}
	if target.Valid {
		t := int(target.Int64)
		p.TargetCount = &t
	}
	if rulesJSON.Valid && rulesJSON.String != "" {
		var rs models.RuleSet
		if err := json.Unmarshal([]byte(rulesJSON.String), &rs); err == nil {
			p.Rules = &rs
		}
	}
	if completed.Valid {
		p.CompletedAt = &completed.Time
	}
	return p, nil
}

func (s *Store) projectItemPosition(ctx context.Context, projectID, entryID string) (*float64, error) {
	if entryID == "" {
		return nil, nil
	}
	var pos float64
	err := s.db.QueryRowContext(ctx,
		`SELECT position FROM project_items WHERE project_id = ? AND entry_id = ?`, projectID, entryID).Scan(&pos)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &pos, nil
}

func (s *Store) renormalizeProject(ctx context.Context, projectID string) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT entry_id FROM project_items WHERE project_id = ? ORDER BY position ASC`, projectID)
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
			`UPDATE project_items SET position = ? WHERE project_id = ? AND entry_id = ?`,
			float64(i+1)*positionGap, projectID, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func toAny(ids []string) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

// nowUTC truncates to whole seconds so a freshly stamped completed_at matches
// what the database hands back on the next read.
func nowUTC() time.Time {
	return time.Now().UTC().Truncate(time.Second)
}
