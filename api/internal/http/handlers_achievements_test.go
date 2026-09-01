package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/backfill"
	"github.com/collinpendleton/backhog/api/internal/config"
	"github.com/collinpendleton/backhog/api/internal/db"
	"github.com/collinpendleton/backhog/api/internal/media"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// eggTestApp is a booted router over a migrated SQLite database, with one
// registered-and-logged-in user per client.
type eggTestApp struct {
	base  string
	spawn func(username string) *http.Client
}

func newEggTestApp(t *testing.T) *eggTestApp {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "eggs.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	srv := NewServer(config.Config{}, store.New(database), nil, nil, nil, &backfill.Runner{}, &media.Runner{})
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	spawn := func(username string) *http.Client {
		t.Helper()
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatalf("cookie jar: %v", err)
		}
		c := &http.Client{Jar: jar, Timeout: 10 * time.Second}
		body, _ := json.Marshal(map[string]string{
			"email":    username + "@example.com",
			"username": username,
			"password": "hogwash123",
		})
		resp, err := c.Post(ts.URL+"/api/auth/register", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("register %s: %v", username, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("register %s: status %d", username, resp.StatusCode)
		}
		return c
	}
	return &eggTestApp{base: ts.URL, spawn: spawn}
}

// egg fires the egg endpoint for a logged-in client and returns the status.
func (app *eggTestApp) egg(t *testing.T, client *http.Client, id string) int {
	t.Helper()
	resp, err := client.Post(app.base+"/api/achievements/"+id+"/egg", "application/json", nil)
	if err != nil {
		t.Fatalf("post egg %s: %v", id, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// wall fetches the caller's achievement gallery.
func (app *eggTestApp) wall(t *testing.T, client *http.Client) map[string]bool {
	t.Helper()
	r, err := client.Get(app.base + "/api/achievements")
	if err != nil {
		t.Fatalf("get achievements: %v", err)
	}
	defer r.Body.Close()
	var list struct {
		Achievements []struct {
			ID          string     `json:"id"`
			Title       string     `json:"title"`
			Description string     `json:"description"`
			Icon        string     `json:"icon"`
			UnlockedAt  *time.Time `json:"unlocked_at"`
		} `json:"achievements"`
	}
	if err := json.NewDecoder(r.Body).Decode(&list); err != nil {
		t.Fatalf("decode achievements: %v", err)
	}
	out := map[string]bool{}
	for _, a := range list.Achievements {
		if a.ID == "night_owl" {
			out["unlocked"] = a.UnlockedAt != nil
			out["revealed"] = a.Title != "???"
			out["masked"] = a.Icon == "lock" &&
				a.Description == "You'll know it when you feel it. Especially around 3 AM."
		}
	}
	return out
}

func TestHandleAchievementEgg(t *testing.T) {
	app := newEggTestApp(t)
	client := app.spawn("egguser")

	// The happy path: a real egg unlocks and the response is the toast
	// payload — the revealed achievement, flagged as newly unlocked.
	resp, err := client.Post(app.base+"/api/achievements/konami/egg", "application/json", nil)
	if err != nil {
		t.Fatalf("post egg: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var payload struct {
		Unlocked    bool `json:"unlocked"`
		Achievement struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Egg   bool   `json:"egg"`
		} `json:"achievement"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !payload.Unlocked || payload.Achievement.ID != "konami" ||
		payload.Achievement.Title != "Old Habits" || !payload.Achievement.Egg {
		t.Errorf("payload = %+v, want a new konami reveal", payload)
	}

	// Idempotent: the same egg again succeeds but is not new.
	resp2, err := client.Post(app.base+"/api/achievements/konami/egg", "application/json", nil)
	if err != nil {
		t.Fatalf("post egg again: %v", err)
	}
	defer resp2.Body.Close()
	var again struct {
		Unlocked bool `json:"unlocked"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&again); err != nil {
		t.Fatalf("decode again: %v", err)
	}
	if again.Unlocked {
		t.Error("second egg unlock reported as new")
	}

	// Non-egg catalogue ids are rejected — the endpoint is not a generic
	// unlock button.
	if code := app.egg(t, client, "first_blood"); code != http.StatusBadRequest {
		t.Errorf("non-egg status = %d, want 400", code)
	}
	if code := app.egg(t, client, "the_big_n"); code != http.StatusBadRequest {
		t.Errorf("hidden non-egg status = %d, want 400", code)
	}
	// Unknown ids 404 like every other achievement-scoped lookup.
	if code := app.egg(t, client, "game_genie"); code != http.StatusNotFound {
		t.Errorf("unknown id status = %d, want 404", code)
	}

	// Light rate limit: 10 per user per egg per minute, then a polite
	// 429. Only the first of the ten actually unlocks; the rest are
	// idempotent 200s that report unlocked=false.
	for i := 1; i <= 11; i++ {
		code := app.egg(t, client, "queue_shuffler")
		want := http.StatusOK
		if i == 11 {
			want = http.StatusTooManyRequests
		}
		if code != want {
			t.Fatalf("attempt %d status = %d, want %d", i, code, want)
		}
	}
}

func TestHandleAchievementEggScopingAndAuth(t *testing.T) {
	app := newEggTestApp(t)
	u1 := app.spawn("egguser")

	// Unauthenticated calls never reach the handler.
	anon := &http.Client{Timeout: 10 * time.Second}
	if code := app.egg(t, anon, "konami"); code != http.StatusUnauthorized {
		t.Errorf("anonymous status = %d, want 401", code)
	}

	// A second user unlocks their own egg; it must not leak to the first.
	u2 := app.spawn("u2")
	if code := app.egg(t, u2, "night_owl"); code != http.StatusOK {
		t.Fatalf("u2 egg status = %d, want 200", code)
	}

	if w := app.wall(t, u2); !w["unlocked"] || !w["revealed"] {
		t.Errorf("u2 wall = %v, want night_owl unlocked and revealed", w)
	}
	// u1 never played the egg: still locked, still masked.
	if w := app.wall(t, u1); w["unlocked"] || !w["masked"] {
		t.Errorf("u1 wall = %v, want night_owl locked and masked", w)
	}

	// The limiter keys by user+egg, so u2's night_owl unlock does not
	// throttle u1's first one.
	if code := app.egg(t, u1, "night_owl"); code != http.StatusOK {
		t.Errorf("u1 egg after u2's = %d, want 200", code)
	}
}

func TestEggLimiterWindow(t *testing.T) {
	l := newEggLimiter(2, time.Minute)
	start := time.Now()
	if !l.allow("k", start) || !l.allow("k", start.Add(time.Second)) {
		t.Fatal("first two attempts should pass")
	}
	if l.allow("k", start.Add(2*time.Second)) {
		t.Fatal("third attempt in the window should fail")
	}
	// A different key is not throttled by the first one.
	if !l.allow("other", start.Add(2*time.Second)) {
		t.Fatal("a different key shares no budget")
	}
	// The window rolls and the budget resets.
	if !l.allow("k", start.Add(time.Minute+time.Second)) {
		t.Fatal("attempt after the window should pass")
	}
}
