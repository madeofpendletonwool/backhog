package audio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// ErrPathEscape reports a media_files row whose path resolves outside every
// configured media root. Rows are written by the scanner, but a hand-edited
// database or a symlink planted in the library must not turn the streaming
// endpoint into an arbitrary-file read, so containment is re-checked on the
// way out, not trusted from the way in.
var ErrPathEscape = errors.New("audio: media path escapes its root")

// Service builds timelines and opens track files for a book entry. Every
// method takes the caller's user id and resolves through their library
// entry, so ownership is checked on every request — including the range
// follow-ups a browser fires while seeking. The URL is not a capability.
type Service struct {
	store *store.Store
	// roots are the configured media roots (MEDIA_DIR), cleaned. They are
	// re-resolved through symlinks per request rather than at construction
	// so a NAS that mounts after boot starts working without a restart.
	roots []string
}

// NewService returns a service over the configured media roots. With no
// roots, every track resolves outside the library and streaming answers 404:
// that is the correct behaviour for a deployment with MEDIA_DIR unset.
func NewService(st *store.Store, roots []string) *Service {
	cleaned := make([]string, 0, len(roots))
	seen := map[string]bool{}
	for _, r := range roots {
		r = filepath.Clean(r)
		if r == "" || r == "." || seen[r] {
			continue
		}
		seen[r] = true
		cleaned = append(cleaned, r)
	}
	return &Service{store: st, roots: cleaned}
}

// Timeline assembles the global-seconds timeline for a user's book entry.
//
// Durations the scanner could not read from the tags are derived here by
// parsing the container headers, and written back so the next request does
// not repeat the work. A file that genuinely cannot be measured leaves the
// timeline degraded rather than being given a made-up length.
func (s *Service) Timeline(ctx context.Context, userID, entryID string) (Timeline, error) {
	bookID, err := s.store.BookIDForEntry(ctx, userID, entryID)
	if err != nil {
		return Timeline{}, err
	}
	files, err := s.store.AudioMediaFilesForBook(ctx, bookID)
	if err != nil {
		return Timeline{}, err
	}
	if len(files) == 0 {
		return Timeline{}, ErrEmptyTimeline
	}

	tracks := make([]Track, 0, len(files))
	for _, f := range files {
		t := Track{
			MediaFileID: f.ID,
			Path:        f.Path,
			Title:       trackTitle(f),
			SizeBytes:   f.SizeBytes,
			Missing:     f.MissingAt != nil,
		}
		if d := s.durationOf(ctx, f); d > 0 {
			t.Duration = d
			t.Measured = true
		}
		tracks = append(tracks, t)
	}
	return NewTimeline(tracks), nil
}

// TrackFile is one audio track resolved to the absolute path the
// alignment worker needs, with containment verified against the
// configured roots — the same check streaming applies, because the
// worker reads the file directly off the same read-only mount.
type TrackFile struct {
	MediaFileID int64   `json:"id"`
	Path        string  `json:"path"`
	Duration    float64 `json:"duration_seconds"`
	// Missing marks a file whose path is currently absent from its root
	// (or resolves outside every root): the worker cannot read its
	// bytes, and the job it belongs to can only fail.
	Missing bool `json:"missing"`
}

// TrackFiles lists a book's attached audio in timeline order, each file
// resolved to an absolute path with its measured duration. It is the
// audio half of an alignment claim: the worker container mounts the
// same library the API does, so API-side absolute paths are meaningful
// there too.
func (s *Service) TrackFiles(ctx context.Context, bookID string) ([]TrackFile, error) {
	files, err := s.store.AudioMediaFilesForBook(ctx, bookID)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, ErrEmptyTimeline
	}
	out := make([]TrackFile, 0, len(files))
	for _, f := range files {
		tf := TrackFile{MediaFileID: f.ID, Missing: f.MissingAt != nil}
		if abs, err := s.resolve(f.Root, f.Path); err == nil {
			tf.Path = abs
		} else {
			tf.Missing = true
		}
		if d := s.durationOf(ctx, f); d > 0 {
			tf.Duration = d
		}
		out = append(out, tf)
	}
	return out, nil
}

// durationOf returns a file's play time, preferring the stored value and
// falling back to a header parse whose result is persisted. 0 means the file
// could not be measured at all.
func (s *Service) durationOf(ctx context.Context, f models.MediaFile) float64 {
	if f.DurationSeconds != nil && *f.DurationSeconds > 0 {
		return *f.DurationSeconds
	}
	if f.MissingAt != nil {
		return 0
	}
	abs, err := s.resolve(f.Root, f.Path)
	if err != nil {
		slog.WarnContext(ctx, "audio duration: resolve", "file", f.ID, "error", err)
		return 0
	}
	seconds, err := ProbeDuration(abs)
	if err != nil {
		slog.WarnContext(ctx, "audio duration: probe", "file", f.ID, "path", f.Path, "error", err)
		return 0
	}
	if err := s.store.SetMediaFileDuration(ctx, f.ID, seconds); err != nil {
		// The measurement is still good for this response; only the cache
		// of it failed.
		slog.WarnContext(ctx, "audio duration: persist", "file", f.ID, "error", err)
	}
	return seconds
}

// OpenTrack opens one track of a user's book entry for streaming and returns
// the file, its stat and its content type. The caller closes the file.
//
// Every failure below — unknown entry, entry belonging to someone else, file
// attached to a different book, path outside the library — comes back as
// store.ErrNotFound so the endpoint answers with a single indistinguishable
// 404.
func (s *Service) OpenTrack(ctx context.Context, userID, entryID string, trackID int64) (*os.File, os.FileInfo, string, error) {
	bookID, err := s.store.BookIDForEntry(ctx, userID, entryID)
	if err != nil {
		return nil, nil, "", err
	}
	f, err := s.store.AudioMediaFileForBook(ctx, bookID, trackID)
	if err != nil {
		return nil, nil, "", err
	}

	abs, err := s.resolve(f.Root, f.Path)
	if err != nil {
		if errors.Is(err, ErrPathEscape) {
			slog.ErrorContext(ctx, "audio stream: path escapes media root",
				"file", f.ID, "root", f.Root, "path", f.Path)
		}
		return nil, nil, "", err
	}

	file, err := os.Open(abs) // O_RDONLY: the media roots are read-only
	if err != nil {
		return nil, nil, "", err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, "", err
	}
	if info.IsDir() {
		file.Close()
		return nil, nil, "", store.ErrNotFound
	}
	return file, info, ContentType(f.Path), nil
}

// resolve joins a stored root-relative path onto its root and verifies, with
// symlinks resolved, that the result is still inside one of the configured
// media roots. Both a ".." in the stored path and a symlink pointing out of
// the mount fail here.
func (s *Service) resolve(root, rel string) (string, error) {
	abs := filepath.Join(root, filepath.FromSlash(rel))
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// A path that does not resolve is indistinguishable from one that
		// was never there.
		return "", fmt.Errorf("%w: %w", store.ErrNotFound, err)
	}
	for _, r := range s.roots {
		rootReal, err := filepath.EvalSymlinks(r)
		if err != nil {
			continue // root not mounted right now
		}
		if real == rootReal || strings.HasPrefix(real, rootReal+string(filepath.Separator)) {
			return real, nil
		}
	}
	return "", fmt.Errorf("%w: %w", store.ErrNotFound, ErrPathEscape)
}

// ContentType maps an audiobook file extension onto the type browsers need to
// pick a decoder. Unknown extensions never reach here from the scanner, which
// only inventories these three, but an octet-stream fallback is safer than
// letting ServeContent sniff a byte range.
func ContentType(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".mp3":
		return "audio/mpeg"
	case ".m4a", ".m4b":
		return "audio/mp4"
	}
	return "application/octet-stream"
}

// trackTitle prefers the container's own title tag and falls back to the file
// name, which for a well-organised library is "01 - Erasmas.m4b".
func trackTitle(f models.MediaFile) string {
	if len(f.ContainerMetadata) > 0 {
		var tags struct {
			Title string `json:"title"`
		}
		if err := json.Unmarshal(f.ContainerMetadata, &tags); err == nil {
			if title := strings.TrimSpace(tags.Title); title != "" {
				return title
			}
		}
	}
	base := path.Base(f.Path)
	return strings.TrimSuffix(base, path.Ext(base))
}
