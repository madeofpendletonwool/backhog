-- +goose Up

-- A project is a temporary objective: "finish the Souls games", "10 games
-- before buying another", "clear the PS2 backlog". Unlike a list (what
-- exists), a project tracks progress toward a target and ends.
CREATE TABLE projects (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    kind         TEXT NOT NULL CHECK (kind IN ('checklist','count_goal','rule_goal')),
    -- target_count: required for count_goal, optional for rule_goal (null =
    -- every matching entry), ignored for checklist (the member list is the target).
    target_count INTEGER,
    -- rules_json: the target set for rule_goal, a validated smart-list RuleSet.
    rules_json   TEXT,
    created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- Stamped automatically the first time the target is met, or manually when
    -- the user closes the project. Sticky: finishing, then replaying, does not
    -- un-complete a project.
    completed_at TIMESTAMP
);
CREATE INDEX idx_projects_user ON projects(user_id);

-- Members of a checklist project. position is the same fractional index the
-- play queue uses. done_override is the manual override on per-item
-- completion: NULL derives it from the entry's status (played = done),
-- 1 forces done, 0 forces not done.
CREATE TABLE project_items (
    project_id   TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    entry_id     TEXT NOT NULL REFERENCES library_entries(id) ON DELETE CASCADE,
    position     REAL NOT NULL,
    done_override INTEGER,
    PRIMARY KEY (project_id, entry_id)
);
CREATE INDEX idx_project_items_position ON project_items(project_id, position);

-- +goose Down

DROP TABLE project_items;
DROP TABLE projects;
