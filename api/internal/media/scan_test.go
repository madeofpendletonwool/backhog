package media

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/db"
	"github.com/collinpendleton/backhog/api/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return store.New(database)
}

func writeFile(t *testing.T, path string, contents []byte) time.Time {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	return info.ModTime()
}

// fixtureLibrary builds a NAS-like tree: an audiobooks root and an ebooks
// root, with supported files, unsupported files (.aax, .pdf), a DRM-wrapped
// epub, and NAS housekeeping clutter.
func fixtureLibrary(t *testing.T) (audioDir, booksDir string) {
	t.Helper()
	base := t.TempDir()
	audioDir = filepath.Join(base, "audiobooks")
	booksDir = filepath.Join(base, "ebooks")

	writeFile(t, filepath.Join(audioDir, "Project Hail Mary.m4b"), buildM4B("Project Hail Mary", "Andy Weir", 3600))
	writeFile(t, filepath.Join(audioDir, "Disc 1", "chapter 01.mp3"), buildMP3("Chapter 1", "Narrator Person", "Some Book", 40))
	writeFile(t, filepath.Join(audioDir, "locked.aax"), []byte("audible DRM bytes"))
	writeFile(t, filepath.Join(booksDir, "novel.epub"), buildEPUB(false))
	writeFile(t, filepath.Join(booksDir, "wrapped.epub"), buildEPUB(true))
	writeFile(t, filepath.Join(booksDir, "cover.pdf"), []byte("%PDF-1.4 not a book"))
	writeFile(t, filepath.Join(booksDir, ".hidden.epub"), buildEPUB(false))
	writeFile(t, filepath.Join(audioDir, "@eaDir", "thumb.jpg"), []byte("synology thumbnail"))
	return audioDir, booksDir
}

func TestScanInventoriesLibrary(t *testing.T) {
	audioDir, booksDir := fixtureLibrary(t)
	st := newTestStore(t)
	runner := NewRunner(st, []string{audioDir, booksDir})

	res, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("scan: %v (result %+v)", err, res)
	}
	if res.Found != 3 || res.New != 3 || res.Unsupported != 3 {
		t.Errorf("counts = found %d, new %d, unsupported %d; want 3, 3, 3", res.Found, res.New, res.Unsupported)
	}
	if res.Missing != 0 || res.Failed != 0 {
		t.Errorf("missing = %d, failed = %d; want 0, 0", res.Missing, res.Failed)
	}

	files, err := st.ListMediaFiles(context.Background(), store.MediaFileFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("got %d files, want 3: %+v", len(files), files)
	}

	type fileClass struct{ kind, root string }
	byPath := map[string]fileClass{}
	for _, f := range files {
		byPath[f.Path] = fileClass{kind: f.Kind, root: f.Root}
	}
	m4bRel, mp3Rel, epubRel := "Project Hail Mary.m4b", "Disc 1/chapter 01.mp3", "novel.epub"
	if byPath[m4bRel].kind != "audio" || byPath[m4bRel].root != audioDir {
		t.Errorf("m4b classified wrong: %+v", byPath[m4bRel])
	}
	if byPath[mp3Rel].kind != "audio" {
		t.Errorf("mp3 classified wrong: %+v", byPath[mp3Rel])
	}
	if byPath[epubRel].kind != "epub" || byPath[epubRel].root != booksDir {
		t.Errorf("epub classified wrong: %+v", byPath[epubRel])
	}

	// The m4b carries its tags and duration in the container.
	var m4bTags map[string]any
	for _, f := range files {
		if f.Path == m4bRel {
			if f.DurationSeconds == nil || *f.DurationSeconds != 3600 {
				t.Errorf("m4b duration = %v, want 3600", f.DurationSeconds)
			}
			if err := json.Unmarshal(f.ContainerMetadata, &m4bTags); err != nil {
				t.Fatalf("m4b tags not JSON: %v", err)
			}
		}
		if f.Path == mp3Rel {
			if f.DurationSeconds == nil || *f.DurationSeconds < 1.0 || *f.DurationSeconds > 1.1 {
				t.Errorf("mp3 duration = %v, want ~1.04 (40 frames)", f.DurationSeconds)
			}
			if f.ContainerMetadata == nil {
				t.Error("mp3 has no tag JSON")
			} else {
				var tags map[string]any
				if err := json.Unmarshal(f.ContainerMetadata, &tags); err != nil {
					t.Fatalf("mp3 tags not JSON: %v", err)
				}
				if tags["title"] != "Chapter 1" || tags["artist"] != "Narrator Person" {
					t.Errorf("mp3 tags = %v", tags)
				}
			}
		}
		if f.Path == epubRel {
			if f.ContainerMetadata != nil || f.DurationSeconds != nil {
				t.Errorf("epub should have no tags or duration: %+v", f)
			}
		}
	}
	if m4bTags["title"] != "Project Hail Mary" || m4bTags["album_artist"] != "Andy Weir" {
		t.Errorf("m4b tags = %v", m4bTags)
	}

	// Skipped files are remembered with their reason, so the attach UI can
	// explain the missing half of a library. Hidden and housekeeping names
	// (.hidden.epub, @eaDir) are never recorded.
	skipped, err := st.ListMediaSkipped(context.Background())
	if err != nil {
		t.Fatalf("list skipped: %v", err)
	}
	skipReason := map[string]string{}
	for _, f := range skipped {
		skipReason[f.Path] = f.Reason
	}
	if len(skipped) != 3 {
		t.Fatalf("got %d skipped rows, want 3: %+v", len(skipped), skipped)
	}
	if skipReason["locked.aax"] != "unsupported_extension" {
		t.Errorf("locked.aax reason = %q", skipReason["locked.aax"])
	}
	if skipReason["cover.pdf"] != "unsupported_extension" {
		t.Errorf("cover.pdf reason = %q", skipReason["cover.pdf"])
	}
	if skipReason["wrapped.epub"] != "drm_epub" {
		t.Errorf("wrapped.epub reason = %q", skipReason["wrapped.epub"])
	}

	// Status reports the finished result with the four headline counts.
	status := runner.Status()
	if status.Running || status.Last == nil {
		t.Fatalf("status after scan: %+v", status)
	}
	if status.Last.Found != 3 || status.Last.New != 3 || status.Last.Unsupported != 3 || status.Last.Missing != 0 {
		t.Errorf("status last counts: %+v", status.Last)
	}

	// A skip that goes away disappears from the inventory on rescan.
	if err := os.Remove(filepath.Join(booksDir, "cover.pdf")); err != nil {
		t.Fatalf("remove pdf: %v", err)
	}
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("rescan: %v", err)
	}
	skipped, err = st.ListMediaSkipped(context.Background())
	if err != nil {
		t.Fatalf("list skipped again: %v", err)
	}
	for _, f := range skipped {
		if f.Path == "cover.pdf" {
			t.Error("cover.pdf still listed as skipped after removal")
		}
	}
}

// TestScanTwiceIsIdempotent covers two acceptance points at once: no
// duplicate rows, and no re-reading of unchanged files — proven by making one
// file unreadable (mode 0000) before the second scan. The cheap (size, mtime)
// path never opens it, so the scan still succeeds and writes nothing.
func TestScanTwiceIsIdempotent(t *testing.T) {
	audioDir, booksDir := fixtureLibrary(t)
	st := newTestStore(t)
	runner := NewRunner(st, []string{audioDir, booksDir})

	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("first scan: %v", err)
	}
	before, err := st.ListMediaFiles(context.Background(), store.MediaFileFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	locked := filepath.Join(audioDir, "Project Hail Mary.m4b")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })

	res, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("second scan: %v (result %+v)", err, res)
	}
	if res.Found != 3 || res.New != 0 || res.Changed != 0 || res.Failed != 0 {
		t.Errorf("second scan counts = %+v; want found 3, nothing new/changed/failed", res)
	}

	after, err := st.ListMediaFiles(context.Background(), store.MediaFileFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("row count changed: %d -> %d (duplicates not prevented?)", len(before), len(after))
	}
	for i := range before {
		a, b := before[i], after[i]
		if a.ID != b.ID || a.ScannedAt != b.ScannedAt || a.SHA256 != b.SHA256 || a.Mtime != b.Mtime {
			t.Errorf("row %q was rewritten on an unchanged rescan: %+v -> %+v", a.Path, a, b)
		}
		if b.SHA256 != nil {
			t.Errorf("row %q was hashed during a scan", a.Path)
		}
	}
}

func TestMissingAndRestore(t *testing.T) {
	audioDir, _ := fixtureLibrary(t)
	st := newTestStore(t)
	runner := NewRunner(st, []string{audioDir})

	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("scan: %v", err)
	}
	files, err := st.ListMediaFiles(context.Background(), store.MediaFileFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	var gone struct {
		id    int64
		path  string
		mtime time.Time
	}
	for _, f := range files {
		if filepath.Base(f.Path) == "chapter 01.mp3" {
			gone.id = f.ID
			gone.path = filepath.Join(audioDir, filepath.FromSlash(f.Path))
			gone.mtime = time.Unix(0, f.Mtime)
		}
	}
	if gone.id == 0 {
		t.Fatalf("mp3 not found in scan results: %+v", files)
	}

	// The attach stage owns book_id through the API; seed a real books row
	// so the direct write below satisfies the foreign key 00015 added.
	if _, err := st.DB().ExecContext(context.Background(),
		`INSERT INTO books (id, title) VALUES ('book-1', 'Some Book')`); err != nil {
		t.Fatalf("seed book: %v", err)
	}
	if _, err := st.DB().ExecContext(context.Background(),
		`UPDATE media_files SET book_id = ? WHERE id = ?`, "book-1", gone.id); err != nil {
		t.Fatalf("attach: %v", err)
	}

	// The NAS folder goes away for a while.
	if err := os.Remove(gone.path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	res, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("scan after removal: %v", err)
	}
	if res.Missing != 1 || res.Found != 1 {
		t.Errorf("counts after removal = %+v; want found 1, missing 1", res)
	}
	row := st.DB().QueryRow(`SELECT book_id, missing_at IS NOT NULL FROM media_files WHERE id = ?`, gone.id)
	var bookID string
	var missing bool
	if err := row.Scan(&bookID, &missing); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if !missing || bookID != "book-1" {
		t.Errorf("removed row = (book %q, missing %v); want (book-1, true)", bookID, missing)
	}

	// The file comes back byte-identical with its original mtime: the cheap
	// path restores the row without opening it, book_id intact.
	if err := os.WriteFile(gone.path, buildMP3("Chapter 1", "Narrator Person", "Some Book", 40), 0o644); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if err := os.Chtimes(gone.path, gone.mtime, gone.mtime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	res, err = runner.Run(context.Background())
	if err != nil {
		t.Fatalf("scan after restore: %v", err)
	}
	if res.Restored != 1 || res.Missing != 0 || res.New != 0 {
		t.Errorf("counts after restore = %+v; want restored 1, no new, no missing", res)
	}
	row = st.DB().QueryRow(`SELECT book_id, missing_at IS NULL FROM media_files WHERE id = ?`, gone.id)
	if err := row.Scan(&bookID, &missing); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if !missing || bookID != "book-1" {
		t.Errorf("restored row = (book %q, missing %v); want (book-1, false)", bookID, missing)
	}
}

// TestMediaRootsAreNeverWritten locks the hard constraint: nothing under the
// media roots is opened for writing. The whole tree is made read-only before
// the scan — a write-open would fail with EACCES and surface as Failed — and
// an mtime snapshot proves no metadata changed either.
func TestMediaRootsAreNeverWritten(t *testing.T) {
	audioDir, booksDir := fixtureLibrary(t)
	st := newTestStore(t)
	runner := NewRunner(st, []string{audioDir, booksDir})

	// The tree goes fully read-only first: a write-open anywhere now fails
	// with EACCES and surfaces as Failed. The snapshot is taken after the
	// chmod so the comparison is purely against what the scan does.
	makeReadOnly(t, audioDir)
	makeReadOnly(t, booksDir)
	t.Cleanup(func() {
		makeWritable(t, audioDir)
		makeWritable(t, booksDir)
	})

	snapshot := treeSnapshot(t, audioDir, booksDir)

	res, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("scan over read-only roots: %v (result %+v)", err, res)
	}
	if res.Found != 3 || res.Failed != 0 {
		t.Errorf("counts over read-only roots = %+v; want found 3, failed 0", res)
	}

	after := treeSnapshot(t, audioDir, booksDir)
	for path, before := range snapshot {
		got, ok := after[path]
		if !ok {
			t.Errorf("%s disappeared during scan", path)
			continue
		}
		if got != before {
			t.Errorf("%s changed during scan:\n  before %s\n  after  %s", path, before, got)
		}
	}
}

// TestAbsentRootKeepsRows verifies the unmounted-NAS case: a root that cannot
// be stat'ed at all never marks its files missing, while a present root with
// deleted files does.
func TestAbsentRootKeepsRows(t *testing.T) {
	audioDir, booksDir := fixtureLibrary(t)
	st := newTestStore(t)
	runner := NewRunner(st, []string{audioDir, booksDir})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("scan: %v", err)
	}

	// A brand-new runner as if the deployment now points at a vanished mount.
	gone := NewRunner(st, []string{filepath.Join(audioDir, "not-mounted")})
	res, err := gone.Run(context.Background())
	if err != nil {
		t.Fatalf("scan with absent root: %v", err)
	}
	if res.Missing != 0 {
		t.Errorf("absent root marked %d files missing; want 0", res.Missing)
	}

	files, err := st.ListMediaFiles(context.Background(), store.MediaFileFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("got %d files after absent-root scan, want 3", len(files))
	}

	// But a present root with a really deleted file does flag it.
	if err := os.Remove(filepath.Join(booksDir, "novel.epub")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	res, err = runner.Run(context.Background())
	if err != nil {
		t.Fatalf("scan after delete: %v", err)
	}
	if res.Missing != 1 {
		t.Errorf("missing after delete = %d, want 1", res.Missing)
	}
}

// --- read-only tree helpers -------------------------------------------------

type treeEntry struct {
	size  int64
	mode  fs.FileMode
	mtime time.Time
}

func (e treeEntry) String() string {
	return fmt.Sprintf("size %d mode %s mtime %s", e.size, e.mode, e.mtime.Format(time.RFC3339Nano))
}

func treeSnapshot(t *testing.T, roots ...string) map[string]treeEntry {
	t.Helper()
	snap := map[string]treeEntry{}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			snap[path] = treeEntry{info.Size(), info.Mode(), info.ModTime()}
			return nil
		})
		if err != nil {
			t.Fatalf("snapshot %s: %v", root, err)
		}
	}
	return snap
}

func makeReadOnly(t *testing.T, root string) {
	t.Helper()
	chmodTree(t, root, func(mode fs.FileMode) fs.FileMode {
		return mode &^ 0o222 // strip every write bit
	})
}

func makeWritable(t *testing.T, root string) {
	t.Helper()
	chmodTree(t, root, func(mode fs.FileMode) fs.FileMode {
		return mode | 0o200 // owner write back, so TempDir cleanup can remove it
	})
}

func chmodTree(t *testing.T, root string, transform func(fs.FileMode) fs.FileMode) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.Chmod(path, transform(info.Mode()).Perm())
	})
	if err != nil {
		t.Fatalf("chmod %s: %v", root, err)
	}
}
