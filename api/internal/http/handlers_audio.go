package http

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/collinpendleton/backhog/api/internal/auth"
	bookaudio "github.com/collinpendleton/backhog/api/internal/books/audio"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// audioCacheControl is a year, private. The bytes behind a track never change
// — a rewritten file gets a new mtime and therefore a new ETag — so the only
// reason to revalidate is the response being user-scoped, which "private"
// already covers. Long caching is what makes scrubbing a 400MB m4b tolerable.
const audioCacheControl = "private, max-age=31536000, immutable"

// handleBookAudioTimeline serves a book's audio as one continuous timeline:
// the tracks in listening order with their global start offsets, the total
// duration, and whether any track's length is unknown.
func (s *Server) handleBookAudioTimeline(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	timeline, err := s.audio.Timeline(r.Context(), userID, chi.URLParam(r, "entryID"))
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, errNotFound)
		return
	case errors.Is(err, bookaudio.ErrEmptyTimeline):
		fail(w, errorf(http.StatusNotFound, "no audiobook is attached to this book"))
		return
	case err != nil:
		fail(w, err)
		return
	}

	writeJSON(w, http.StatusOK, timeline)
}

// handleBookAudioTrack streams one track's bytes with full range support.
//
// http.ServeContent does the protocol work — Range, multi-part ranges,
// If-Range, If-None-Match, 206 and 416 — because hand-rolled range parsing is
// where audio streaming goes wrong. What this handler owns is everything
// before that: the caller is authenticated, the entry is theirs, the track is
// attached to that entry's book, and the path is inside the media roots.
// Those checks run again on every range follow-up; the URL carries no
// authority of its own.
func (s *Server) handleBookAudioTrack(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	trackID, err := strconv.ParseInt(chi.URLParam(r, "trackID"), 10, 64)
	if err != nil || trackID <= 0 {
		fail(w, errorf(http.StatusBadRequest, "invalid track id"))
		return
	}

	file, info, contentType, err := s.audio.OpenTrack(r.Context(), userID, chi.URLParam(r, "entryID"), trackID)
	if err != nil {
		// Not-yours, not-attached, gone from the NAS and escapes-the-root all
		// answer alike: the response must not distinguish them.
		if errors.Is(err, store.ErrNotFound) {
			fail(w, errNotFound)
			return
		}
		fail(w, err)
		return
	}
	defer file.Close()

	modTime := info.ModTime()
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("ETag", audioETag(info.Size(), modTime))
	w.Header().Set("Cache-Control", audioCacheControl)
	w.Header().Set("Accept-Ranges", "bytes")

	http.ServeContent(w, r, info.Name(), modTime, file)
}

// audioETag identifies the bytes by (size, mtime): the pair the scanner
// already treats as this file's identity. It is a strong validator — the
// content really is byte-identical for a given pair — which matters because
// If-Range only accepts a strong one, and a weak validator would make every
// seek re-download the file from zero.
func audioETag(size int64, modTime time.Time) string {
	return fmt.Sprintf(`"%x-%x"`, size, modTime.UnixNano())
}
