package models

import "time"

// Status values for a library entry.
const (
	StatusBacklog = "backlog"
	StatusPlaying = "playing"
	StatusPlayed  = "played"
	StatusDropped = "dropped"
)

// ValidStatus reports whether s is one of the four tracked statuses.
func ValidStatus(s string) bool {
	switch s {
	case StatusBacklog, StatusPlaying, StatusPlayed, StatusDropped:
		return true
	}
	return false
}

type User struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

type NamedRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Game is the shared, IGDB-sourced metadata record. Times to beat are seconds.
type Game struct {
	ID                 int64      `json:"id"`
	Name               string     `json:"name"`
	Slug               string     `json:"slug"`
	Summary            string     `json:"summary"`
	CoverURL           string     `json:"cover_url"`
	AccentHex          string     `json:"accent_hex"`
	FirstReleaseDate   *int64     `json:"first_release_date"`
	IGDBRating         *float64   `json:"igdb_rating"`
	TimeToBeatMain     *int64     `json:"time_to_beat_main"`
	TimeToBeatComplete *int64     `json:"time_to_beat_complete"`
	Genres             []NamedRef `json:"genres"`
	Platforms          []NamedRef `json:"platforms"`
}

// Entry is one game in one user's library.
type Entry struct {
	ID            string     `json:"id"`
	Game          Game       `json:"game"`
	Status        string     `json:"status"`
	PlatformID    *int64     `json:"platform_id"`
	UserRating    *int       `json:"user_rating"`
	Notes         string     `json:"notes"`
	QueuePosition *float64   `json:"queue_position"`
	StartedAt     *time.Time `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type List struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Kind        string     `json:"kind"`
	Rules       *RuleSet   `json:"rules,omitempty"`
	Count       int        `json:"count"`
	CreatedAt   time.Time  `json:"created_at"`
}

// RuleSet is the stored definition of a smart list.
type RuleSet struct {
	Match string `json:"match"` // "all" | "any"
	Rules []Rule `json:"rules"`
	Sort  *Sort  `json:"sort,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type Rule struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}

type Sort struct {
	Field string `json:"field"`
	Dir   string `json:"dir"`
}

// Stats summarises a user's library for the dashboard strip.
type Stats struct {
	Total        int     `json:"total"`
	Backlog      int     `json:"backlog"`
	Playing      int     `json:"playing"`
	Played       int     `json:"played"`
	Dropped      int     `json:"dropped"`
	BacklogHours float64 `json:"backlog_hours"`
	PlayedHours  float64 `json:"played_hours"`
	Completion   float64 `json:"completion"`
}
