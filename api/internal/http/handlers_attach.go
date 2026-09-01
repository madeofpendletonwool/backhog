package http

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/collinpendleton/backhog/api/internal/auth"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// handleMediaCandidates serves the attach review queue: unattached files
// grouped into audiobook directories and single EPUBs, each with a ranked
// suggestion list, plus the skipped-file inventory so the UI can explain
// the missing half of a library.
func (s *Server) handleMediaCandidates(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	candidates, err := s.matcher.Candidates(r.Context(), userID)
	if err != nil {
		fail(w, err)
		return
	}
	skipped, err := s.store.ListMediaSkipped(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"candidates": candidates,
		"skipped":    skipped,
	})
}

type attachRequest struct {
	// FileIDs are attached in the order given: for audio, the array is the
	// explicit track order.
	FileIDs []int64 `json:"file_ids"`
	// Kind cross-checks the batch: every file must be of this kind.
	Kind string `json:"kind"`
}

// handleAttachFiles attaches media files to a book entry. Attaching an
// EPUB also triggers its canonical-text parse — the reader and the
// alignment work both consume that text, and it should exist by the time
// anyone opens the book.
func (s *Server) handleAttachFiles(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	var body attachRequest
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	if !models.ValidMediaFileKind(body.Kind) {
		fail(w, errorf(http.StatusBadRequest, "kind must be one of: audio, epub"))
		return
	}

	files, err := s.store.AttachMediaFiles(r.Context(), userID, chi.URLParam(r, "entryID"), body.FileIDs, body.Kind)
	switch {
	case errors.Is(err, store.ErrAttach):
		fail(w, errorf(http.StatusBadRequest, err.Error()))
		return
	case errors.Is(err, store.ErrNotFound):
		fail(w, errNotFound)
		return
	case errors.Is(err, store.ErrConflict):
		fail(w, errorf(http.StatusConflict,
			"one of these files is already attached to another book; detach it first"))
		return
	case err != nil:
		fail(w, err)
		return
	}

	if body.Kind == models.MediaFileEpub && s.epubs != nil {
		for _, f := range files {
			if _, perr := s.epubs.EnsureForMediaFile(r.Context(), f); perr != nil {
				// The attachment holds — the text endpoints parse lazily —
				// but the failure is worth a log line, not silence.
				slog.WarnContext(r.Context(), "epub parse on attach", "file", f.Path, "error", perr)
			}
		}
	}

	writeJSON(w, http.StatusCreated, map[string]any{"attached": len(files), "files": files})
}

// handleDetachFile clears one file's attachment. The file on disk is never
// touched and the inventory row stays.
func (s *Server) handleDetachFile(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	fileID, err := strconv.ParseInt(chi.URLParam(r, "fileID"), 10, 64)
	if err != nil || fileID <= 0 {
		fail(w, errorf(http.StatusBadRequest, "invalid file id"))
		return
	}

	err = s.store.DetachMediaFile(r.Context(), userID, chi.URLParam(r, "entryID"), fileID)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errNotFound)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"detached": true})
}

// handleBookFiles lists the files attached to one of the user's book
// entries: the epub first, then audio in track order.
func (s *Server) handleBookFiles(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	files, err := s.store.MediaFilesForEntry(r.Context(), userID, chi.URLParam(r, "entryID"))
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errNotFound)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

type ignoreRequest struct {
	FileIDs []int64 `json:"file_ids"`
}

// handleMediaIgnore records "stop suggesting these files" for the caller.
func (s *Server) handleMediaIgnore(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	var body ignoreRequest
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	if len(body.FileIDs) == 0 {
		fail(w, errorf(http.StatusBadRequest, "file_ids is required"))
		return
	}

	ignored, err := s.store.IgnoreMediaFiles(r.Context(), userID, body.FileIDs)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"ignored": ignored})
}

// handleMediaUnignore reverses one ignore.
func (s *Server) handleMediaUnignore(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	fileID, err := strconv.ParseInt(chi.URLParam(r, "fileID"), 10, 64)
	if err != nil || fileID <= 0 {
		fail(w, errorf(http.StatusBadRequest, "invalid file id"))
		return
	}

	err = s.store.UnignoreMediaFile(r.Context(), userID, fileID)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errNotFound)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ignored": false})
}
