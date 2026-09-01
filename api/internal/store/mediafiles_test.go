package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

func insertTestFiles(t *testing.T, s *Store, n int) []models.MediaFile {
	t.Helper()
	files := make([]models.MediaFile, 0, n)
	for i := 0; i < n; i++ {
		kind := models.MediaFileAudio
		if i%2 == 0 {
			kind = models.MediaFileEpub
		}
		files = append(files, models.MediaFile{
			Root: "/media/root", Path: timeSlot(i), Kind: kind,
			SizeBytes: int64(1000 + i), Mtime: int64(1_700_000_000_000_000_000 + i),
			ScannedAt: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC),
		})
	}
	if err := s.InsertMediaFiles(context.Background(), files); err != nil {
		t.Fatalf("insert: %v", err)
	}
	return files
}

func timeSlot(i int) string {
	return string(rune('a'+i%26)) + "/" + string(rune('a'+i/26)) + ".m4b"
}

func TestMediaFileIndexAndLifecycle(t *testing.T) {
	s := newTestStore(t)
	insertTestFiles(t, s, 3)

	index, err := s.MediaFileIndex(context.Background())
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	stamps := index["/media/root"]
	if len(stamps) != 3 {
		t.Fatalf("index has %d stamps, want 3", len(stamps))
	}

	// One file changes on disk: (size, mtime) move, content columns refresh.
	var changed models.MediaFile
	for path, stamp := range stamps {
		if path == timeSlot(1) {
			changed = models.MediaFile{
				ID: stamp.ID, Root: "/media/root", Path: path, Kind: models.MediaFileAudio,
				SizeBytes: 9999, Mtime: stamp.Mtime + 5,
				DurationSeconds: ptr(3610.5), ContainerMetadata: json.RawMessage(`{"title":"New"}`),
				ScannedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
			}
		}
	}
	if changed.ID == 0 {
		t.Fatalf("did not find stamp for %s", timeSlot(1))
	}
	if err := s.UpdateMediaFileContent(context.Background(), changed); err != nil {
		t.Fatalf("update: %v", err)
	}

	files, err := s.ListMediaFiles(context.Background(), MediaFileFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var updated *models.MediaFile
	for i := range files {
		if files[i].ID == changed.ID {
			updated = &files[i]
		}
	}
	if updated == nil {
		t.Fatal("updated row missing")
	}
	if updated.SizeBytes != 9999 || updated.DurationSeconds == nil || *updated.DurationSeconds != 3610.5 {
		t.Errorf("updated row = %+v", updated)
	}
	if string(updated.ContainerMetadata) != `{"title":"New"}` {
		t.Errorf("updated metadata = %s", updated.ContainerMetadata)
	}

	// Two files disappear; one comes back. Missing never deletes rows.
	var ids []int64
	for path, stamp := range stamps {
		if path != timeSlot(1) {
			ids = append(ids, stamp.ID)
		}
	}
	at := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	if err := s.MarkMediaFilesMissing(context.Background(), ids, at); err != nil {
		t.Fatalf("mark missing: %v", err)
	}
	if err := s.RestoreMediaFiles(context.Background(), ids[:1], at); err != nil {
		t.Fatalf("restore: %v", err)
	}

	var total int
	if err := s.DB().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM media_files`).Scan(&total); err != nil {
		t.Fatalf("count: %v", err)
	}
	if total != 3 {
		t.Errorf("%d rows after missing/restore, want 3 (never deleted)", total)
	}

	// Default listing hides missing rows; include_missing surfaces them.
	present, err := s.ListMediaFiles(context.Background(), MediaFileFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(present) != 2 {
		t.Errorf("%d present files, want 2", len(present))
	}
	all, err := s.ListMediaFiles(context.Background(), MediaFileFilter{IncludeMissing: true})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("%d files including missing, want 3", len(all))
	}
}

func TestListMediaFilesFilters(t *testing.T) {
	s := newTestStore(t)
	files := insertTestFiles(t, s, 4)
	ctx := context.Background()

	if _, err := s.DB().ExecContext(ctx,
		`UPDATE media_files SET book_id = 'b1' WHERE path = ?`, timeSlot(1)); err != nil {
		t.Fatalf("attach: %v", err)
	}

	audio, err := s.ListMediaFiles(ctx, MediaFileFilter{Kind: "audio"})
	if err != nil {
		t.Fatalf("list audio: %v", err)
	}
	for _, f := range audio {
		if f.Kind != "audio" {
			t.Errorf("kind filter leaked %q row", f.Kind)
		}
	}
	if len(audio) != 2 {
		t.Errorf("%d audio files, want 2", len(audio))
	}

	unattached, err := s.ListMediaFiles(ctx, MediaFileFilter{Unattached: true})
	if err != nil {
		t.Fatalf("list unattached: %v", err)
	}
	if len(unattached) != 3 {
		t.Errorf("%d unattached files, want 3", len(unattached))
	}
	for _, f := range unattached {
		if f.BookID != nil {
			t.Errorf("unattached filter returned attached row %s", f.Path)
		}
	}

	_ = files
}

// The insert path is chunked at 128 rows; a batch spanning boundaries lands
// whole.
func TestInsertMediaFilesChunks(t *testing.T) {
	s := newTestStore(t)
	insertTestFiles(t, s, 300)

	var count int
	if err := s.DB().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM media_files`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 300 {
		t.Errorf("%d rows inserted, want 300", count)
	}
}