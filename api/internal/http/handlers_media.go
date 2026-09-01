package http

import (
	"net/http"

	"github.com/collinpendleton/backhog/api/internal/auth"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// handleMediaScan is the on-demand trigger for the NAS library walk: it
// starts a scan when none is active and always reports progress and the last
// run's counts of found/new/missing/unsupported.
func (s *Server) handleMediaScan(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		started := s.media.Kick()
		writeJSON(w, http.StatusOK, map[string]bool{"started": started})
		return
	}
	writeJSON(w, http.StatusOK, s.media.Status())
}

// handleMediaFiles lists scanned library files for the attach UI. kind
// filters by audio|epub, unattached=true hides files already tied to a book,
// and missing rows are excluded unless include_missing=true.
func (s *Server) handleMediaFiles(w http.ResponseWriter, r *http.Request) {
	if _, err := auth.MustUserID(r.Context()); err != nil {
		fail(w, errUnauthorized)
		return
	}

	q := r.URL.Query()
	kind := q.Get("kind")
	if kind != "" && !models.ValidMediaFileKind(kind) {
		fail(w, errorf(http.StatusBadRequest, "kind must be one of: audio, epub"))
		return
	}

	files, err := s.store.ListMediaFiles(r.Context(), store.MediaFileFilter{
		Kind:           kind,
		Unattached:     q.Get("unattached") == "true",
		IncludeMissing: q.Get("include_missing") == "true",
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}
