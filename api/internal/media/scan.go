// Package media keeps the media_files inventory in sync with the read-only
// NAS library. The Books arena never uploads anything: files are bind-mounted
// into the container and the scanner walks the configured roots, upserting by
// (root, path) with cheap (size, mtime) change detection. Disappeared paths
// are flagged, never deleted, so a temporarily-unmounted NAS cannot destroy
// user associations.
package media

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/collinpendleton/backhog/api/internal/books/epub"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// supportedExtensions maps file extensions to media file kinds. Everything
// else — .aax, .aaxc, .mobi, cover art — is not inventoried and only counted,
// but it is counted *with a reason* (see recordSkip). DRM of any form is
// deliberately out of scope: this tool is DRM-free by decision.
var supportedExtensions = map[string]string{
	".mp3":  models.MediaFileAudio,
	".m4a":  models.MediaFileAudio,
	".m4b":  models.MediaFileAudio,
	".opus": models.MediaFileAudio,
	".epub": models.MediaFileEpub,
}

// metaVersion is the version of the metadata extraction the scanner performs:
// embedded audio tags, and an epub's own OPF package metadata. It joins
// (size, mtime) in the fast-path comparison, so bumping it here re-reads every
// file's metadata exactly once on the next scan and then goes quiet. Without
// it, improving the extractor would only ever affect files that happened to
// change afterwards. This is books.ParserVersion's trick, applied to the
// inventory rather than to the canonical text.
//
// 1: audio container tags; epub OPF title/author/series/identifiers.
const metaVersion = 1

// ScanResult summarises one scan, live while it runs and frozen as the last
// result once it finishes.
type ScanResult struct {
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	Roots       []string   `json:"roots"`
	Found       int        `json:"found"`       // supported files seen this scan
	New         int        `json:"new"`         // rows inserted
	Changed     int        `json:"changed"`     // rows refreshed (size/mtime moved)
	Restored    int        `json:"restored"`    // previously-missing files back
	Missing     int        `json:"missing"`     // paths gone since last scan
	Unsupported int        `json:"unsupported"` // skipped: wrong type, DRM, or unhandled
	Sidecars    int        `json:"sidecars"`    // .opf metadata files parsed
	Failed      int        `json:"failed"`      // files that errored mid-scan
	Error       string     `json:"error,omitempty"`
}

// ScanStatus is the live view the progress endpoint serves.
type ScanStatus struct {
	Running     bool        `json:"running"`
	Found       int         `json:"found"`
	New         int         `json:"new"`
	Unsupported int         `json:"unsupported"`
	Last        *ScanResult `json:"last,omitempty"`
}

// Runner serialises scans: Run refuses to start a second walk while one is
// still running. It follows the backfill.Runner long-job pattern, kicked from
// the UI and polled for progress.
type Runner struct {
	store *store.Store
	roots []string
	// running is the single admission gate; mu guards the counters below,
	// which double as the in-progress result and the last completed one.
	running atomic.Bool
	mu      sync.Mutex
	live    ScanResult
	last    *ScanResult
}

func NewRunner(st *store.Store, roots []string) *Runner {
	cleaned := make([]string, 0, len(roots))
	seen := map[string]bool{}
	for _, r := range roots {
		r = filepath.Clean(r)
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		cleaned = append(cleaned, r)
	}
	return &Runner{store: st, roots: cleaned}
}

// Running reports whether a scan is in progress.
func (r *Runner) Running() bool { return r.running.Load() }

// Status snapshots the progress counters for the API.
func (r *Runner) Status() ScanStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return ScanStatus{
		Running:     r.Running(),
		Found:       r.live.Found,
		New:         r.live.New,
		Unsupported: r.live.Unsupported,
		Last:        r.last,
	}
}

// Kick starts an asynchronous scan when none is active. Returns whether one
// was started. Admission is decided by Run itself so a kicked run can never
// be locked out by this check.
func (r *Runner) Kick() bool {
	if r.Running() {
		return false
	}
	go func() {
		// Detached from any request: the scan outlives the caller.
		result, err := r.Run(context.Background())
		if err != nil {
			slog.Error("media scan failed", "error", err)
			return
		}
		slog.Info("media scan done",
			"found", result.Found, "new", result.New, "changed", result.Changed,
			"restored", result.Restored, "missing", result.Missing,
			"unsupported", result.Unsupported, "failed", result.Failed)
	}()
	return true
}

// Run performs one synchronous scan of every configured root and returns its
// summary. It is idempotent: scanning an unchanged library writes nothing.
func (r *Runner) Run(ctx context.Context) (ScanResult, error) {
	if !r.running.CompareAndSwap(false, true) {
		return r.snapshotLive(), nil
	}
	defer r.running.Store(false)

	roots := append([]string(nil), r.roots...)
	r.reset(roots)

	s := &scan{runner: r, roots: roots, seen: map[string]map[string]bool{},
		skipped:  map[string][]models.MediaSkipped{},
		sidecars: map[string][]models.MediaSidecar{}}
	s.mutate(func(l *ScanResult) { l.StartedAt = time.Now(); l.Roots = roots })

	var err error
	s.index, err = r.store.MediaFileIndex(ctx)
	if err == nil {
		for _, root := range roots {
			if err = ctx.Err(); err != nil {
				break
			}
			if _, statErr := os.Stat(root); statErr != nil {
				// An absent root usually means the mount is gone entirely.
				// Leave its rows alone: marking a whole library missing
				// because the NAS is unmounted would be noise, not
				// information — a present-but-empty root is what marks files
				// missing, because then they really are gone.
				slog.Warn("media scan root unavailable, skipping", "root", root, "error", statErr)
				continue
			}
			s.seen[root] = map[string]bool{}
			s.walkRoot(ctx, root)
		}
	}
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		err = r.store.InsertMediaFiles(ctx, s.inserts)
	}
	if err == nil {
		// Keyed on the roots actually walked, not on the accumulator: a root
		// whose last unsupported file was deleted has no entry in s.skipped
		// and still needs its stale rows cleared.
		for _, root := range s.walkedRoots() {
			if err = r.store.ReplaceMediaSkipped(ctx, root, s.skipped[root]); err != nil {
				break
			}
		}
	}
	if err == nil {
		// Swapped per root like the skipped rows, and for the same reason:
		// a sidecar describes what is on disk right now, so a root that was
		// walked replaces its set wholesale. Roots that were unavailable this
		// run have no entry here and keep whatever they had.
		for _, root := range s.walkedRoots() {
			if err = r.store.ReplaceMediaSidecars(ctx, root, s.sidecars[root]); err != nil {
				break
			}
		}
	}
	if err == nil && len(s.restores) > 0 {
		err = r.store.RestoreMediaFiles(ctx, s.restores, s.live().StartedAt)
	}
	if err == nil {
		if ids := s.missingIDs(); len(ids) > 0 {
			if err = r.store.MarkMediaFilesMissing(ctx, ids, s.live().StartedAt); err == nil {
				s.mutate(func(l *ScanResult) { l.Missing = len(ids) })
			}
		}
	}
	if err != nil {
		s.mutate(func(l *ScanResult) { l.Error = err.Error() })
	}

	now := time.Now()
	s.mutate(func(l *ScanResult) { l.FinishedAt = &now })
	finish := s.live()
	r.mu.Lock()
	r.last = &finish
	r.mu.Unlock()
	return finish, err
}

// scan carries one run's accumulators; the live counters on the runner are
// the single source of truth for the summary.
type scan struct {
	runner   *Runner
	roots    []string
	index    map[string]map[string]store.MediaFileStamp
	seen     map[string]map[string]bool
	inserts  []models.MediaFile
	restores []int64
	// skipped accumulates this run's unsupported-file rows per root, so the
	// attach UI can explain the missing half of a library.
	skipped map[string][]models.MediaSkipped
	// sidecars accumulates this run's parsed .opf metadata per root, swapped
	// at the end of the scan alongside skipped.
	sidecars map[string][]models.MediaSidecar
}

// walkRoot walks one root, classifying files and queueing writes. Errors on
// single files count as Failed and never stop the walk.
func (s *scan) walkRoot(ctx context.Context, root string) {
	r := s.runner
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			slog.Warn("media scan walk", "path", path, "error", err)
			s.mutate(func(l *ScanResult) { l.Failed++ })
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			// Hidden and NAS housekeeping directories (.DS_Store droppings,
			// Synology @eaDir) are not library content.
			if path != root && isJunkName(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if isJunkName(d.Name()) {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		kind, supported := supportedExtensions[ext]
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			s.mutate(func(l *ScanResult) { l.Failed++ })
			return nil
		}
		rel = filepath.ToSlash(rel)

		info, err := d.Info()
		if err != nil {
			slog.Warn("media scan stat", "path", path, "error", err)
			s.mutate(func(l *ScanResult) { l.Failed++ })
			return nil
		}
		size, mtime := info.Size(), info.ModTime().UnixNano()

		if !supported {
			// Three different statements, not one shrug. An .opf is not a
			// book at all — it is the answer key next to the books, so it is
			// parsed for the matcher and recorded as accounted-for. A Kindle
			// file is a format this tool chose not to parse. Everything else
			// (.aax/.aaxc, cover art, ...) is genuinely unrecognised. None of
			// the three is ever inventoried, and all three are remembered so
			// the attach UI can show *why* they are missing.
			switch {
			case ext == sidecarExtension:
				if car, ok := parseSidecar(root, rel, path); ok {
					car.SeenAt = s.live().StartedAt
					s.sidecars[root] = append(s.sidecars[root], car)
					s.mutate(func(l *ScanResult) { l.Sidecars++ })
				}
				s.recordSkip(root, rel, ext, models.MediaSkipSidecar, size, mtime)
			case knownUnhandled[ext]:
				s.recordSkip(root, rel, ext, models.MediaSkipFormatUnhandled, size, mtime)
			default:
				s.recordSkip(root, rel, ext, models.MediaSkipUnsupported, size, mtime)
			}
			s.mutate(func(l *ScanResult) { l.Unsupported++ })
			return nil
		}

		if stamp, ok := s.index[root][rel]; ok && stamp.Size == size && stamp.Mtime == mtime &&
			stamp.MetaVersion == metaVersion {
			// Cheap path: (size, mtime) unchanged and the metadata was read by
			// the current extractor, so the file is not opened at all — no tag
			// read, no hash, no write. If it was flagged missing, the flag
			// clears and the row keeps its book_id.
			if stamp.Missing {
				s.restores = append(s.restores, stamp.ID)
				s.mutate(func(l *ScanResult) { l.Found++; l.Restored++ })
			} else {
				s.mutate(func(l *ScanResult) { l.Found++ })
			}
			s.seen[root][rel] = true
			return nil
		}

		file := models.MediaFile{
			Root:        root,
			Path:        rel,
			Kind:        kind,
			SizeBytes:   size,
			Mtime:       mtime,
			MetaVersion: metaVersion,
			ScannedAt:   s.live().StartedAt,
		}
		switch kind {
		case models.MediaFileEpub:
			// One open answers both questions: DRM, and what the book's own
			// OPF says it is. Without the metadata an epub is matched on its
			// filename alone, which is the weakest evidence there is.
			//
			// Only the DRM verdict can stop a file being inventoried. An OPF
			// that will not parse — an exotic charset declaration, a
			// malformed package — costs the book its metadata, never its
			// place in the library: it falls back to filename matching,
			// exactly as it did before any metadata was read at all.
			encrypted, tags, err := readEpubMetadata(path)
			if err != nil && !errors.Is(err, epub.ErrBadMetadata) {
				// The container itself would not open, so nothing about this
				// file could be determined — DRM included. Refusing to
				// inventory it is the DRM-respecting answer.
				slog.Warn("media scan epub", "path", path, "error", err)
				s.mutate(func(l *ScanResult) { l.Failed++ })
				return nil
			}
			if err != nil {
				slog.Warn("media scan epub metadata", "path", path, "error", err)
			}
			file.ContainerMetadata = marshalTags(tags)
			if encrypted {
				// DRM-wrapped epub: unsupported after all. The path stays out
				// of the seen set, so an existing row is flagged missing by
				// the end-of-scan pass instead of being deleted.
				s.recordSkip(root, rel, ext, models.MediaSkipDRM, size, mtime)
				s.mutate(func(l *ScanResult) { l.Unsupported++ })
				return nil
			}
		case models.MediaFileAudio:
			file.ContainerMetadata, file.DurationSeconds = readAudioMetadata(path, ext)
		}

		if stamp, ok := s.index[root][rel]; ok {
			file.ID = stamp.ID
			if err := r.store.UpdateMediaFileContent(ctx, file); err != nil {
				slog.Warn("media scan update", "path", path, "error", err)
				s.mutate(func(l *ScanResult) { l.Failed++ })
				return nil
			}
			if stamp.Missing {
				s.restores = append(s.restores, stamp.ID)
				s.mutate(func(l *ScanResult) { l.Found++; l.Changed++; l.Restored++ })
			} else {
				s.mutate(func(l *ScanResult) { l.Found++; l.Changed++ })
			}
			s.seen[root][rel] = true
			return nil
		}

		s.inserts = append(s.inserts, file)
		s.mutate(func(l *ScanResult) { l.Found++; l.New++ })
		s.seen[root][rel] = true
		return nil
	})
	if err != nil {
		slog.Warn("media scan walk root", "root", root, "error", err)
	}
}

// missingIDs collects the row IDs the index holds for the scanned roots whose
// paths this scan did not see and that are not already flagged. Roots that
// were unavailable this run have no seen map at all and are skipped: their
// rows must not be flagged missing just because a mount was gone.
func (s *scan) missingIDs() []int64 {
	var ids []int64
	for _, root := range s.roots {
		if s.seen[root] == nil {
			continue
		}
		for path, stamp := range s.index[root] {
			if !stamp.Missing && !s.seen[root][path] {
				ids = append(ids, stamp.ID)
			}
		}
	}
	return ids
}

// walkedRoots lists the roots this scan actually walked, in configured order.
// A root that was unavailable has no seen map, exactly as in missingIDs: its
// stored rows must survive an unmounted NAS rather than being replaced with
// nothing.
func (s *scan) walkedRoots() []string {
	var roots []string
	for _, root := range s.roots {
		if s.seen[root] != nil {
			roots = append(roots, root)
		}
	}
	return roots
}

func (s *scan) live() ScanResult {
	s.runner.mu.Lock()
	defer s.runner.mu.Unlock()
	return s.runner.live
}

// recordSkip queues one unsupported-file row for the root's inventory swap.
func (s *scan) recordSkip(root, rel, ext, reason string, size, mtime int64) {
	s.skipped[root] = append(s.skipped[root], models.MediaSkipped{
		Root: root, Path: rel, Ext: ext, Reason: reason,
		SizeBytes: size, Mtime: mtime, SeenAt: s.live().StartedAt,
	})
}

func (s *scan) mutate(fn func(*ScanResult)) {
	s.runner.mu.Lock()
	fn(&s.runner.live)
	s.runner.mu.Unlock()
}

func (r *Runner) reset(roots []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.live = ScanResult{Roots: roots}
}

func (r *Runner) snapshotLive() ScanResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.live
}

// isJunkName reports whether a file or directory is hidden clutter or NAS
// housekeeping rather than library content.
func isJunkName(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "@")
}
