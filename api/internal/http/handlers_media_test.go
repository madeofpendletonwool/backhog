package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/backfill"
	"github.com/collinpendleton/backhog/api/internal/config"
	"github.com/collinpendleton/backhog/api/internal/db"
	"github.com/collinpendleton/backhog/api/internal/media"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// mediaTestApp is a booted router whose media runner points at a real
// fixture directory, with one logged-in client.
func mediaTestApp(t *testing.T, roots ...string) (base string, client *http.Client) {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "media.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	st := store.New(database)
	srv := NewServer(config.Config{}, st, nil, nil, nil, nil, &backfill.Runner{}, media.NewRunner(st, roots))
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client = &http.Client{Jar: jar, Timeout: 10 * time.Second}
	body, _ := json.Marshal(map[string]string{
		"email":    "media@example.com",
		"username": "media",
		"password": "hogwash123",
	})
	resp, err := client.Post(ts.URL+"/api/auth/register", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register: status %d", resp.StatusCode)
	}
	return ts.URL, client
}

func mediaFixtureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Book"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "Book", "read.m4b"), []byte("fake m4b"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return root
}

func TestMediaEndpoints(t *testing.T) {
	root := mediaFixtureRoot(t)
	base, client := mediaTestApp(t, root)

	// Unauthenticated requests are rejected.
	resp, err := http.Get(base + "/api/media/files")
	if err != nil {
		t.Fatalf("get files anonymous: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous /media/files status = %d, want 401", resp.StatusCode)
	}

	// Idle status before any scan.
	resp, err = client.Get(base + "/api/media/scan")
	if err != nil {
		t.Fatalf("get scan: %v", err)
	}
	var status struct {
		Running bool `json:"running"`
		Last    *struct {
			Found int `json:"found"`
		} `json:"last"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode scan status: %v", err)
	}
	resp.Body.Close()
	if status.Running || status.Last != nil {
		t.Errorf("idle status = %+v", status)
	}

	// Kick a scan, then poll until it finishes.
	resp, err = client.Post(base+"/api/media/scan", "application/json", nil)
	if err != nil {
		t.Fatalf("post scan: %v", err)
	}
	var kicked struct {
		Started bool `json:"started"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&kicked); err != nil {
		t.Fatalf("decode kick: %v", err)
	}
	resp.Body.Close()
	if !kicked.Started {
		t.Fatal("scan did not start")
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err = client.Get(base + "/api/media/scan")
		if err != nil {
			t.Fatalf("poll scan: %v", err)
		}
		if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
			t.Fatalf("decode poll: %v", err)
		}
		resp.Body.Close()
		if !status.Running && status.Last != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("scan never finished; status %+v", status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if status.Last.Found != 1 {
		t.Errorf("scan found %d files, want 1", status.Last.Found)
	}

	// The file list serves the scanned inventory.
	resp, err = client.Get(base + "/api/media/files?kind=audio")
	if err != nil {
		t.Fatalf("get files: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get files: status %d", resp.StatusCode)
	}
	var listing struct {
		Files []struct {
			Path string `json:"path"`
			Kind string `json:"kind"`
		} `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		t.Fatalf("decode files: %v", err)
	}
	if len(listing.Files) != 1 || listing.Files[0].Path != "Book/read.m4b" || listing.Files[0].Kind != "audio" {
		t.Errorf("files = %+v", listing.Files)
	}

	// Unknown kinds are rejected rather than silently ignored.
	resp, err = client.Get(base + "/api/media/files?kind=vinyl")
	if err != nil {
		t.Fatalf("get files bad kind: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad kind status = %d, want 400", resp.StatusCode)
	}
}
