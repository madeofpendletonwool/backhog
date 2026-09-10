package http

import (
	"errors"
	"log/slog"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/collinpendleton/backhog/api/internal/auth"
	"github.com/collinpendleton/backhog/api/internal/books/pdf"
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

// handleAttachFiles attaches media files to a book entry. Attaching a
// text-side file (EPUB or MOBI/AZW/AZW3) also triggers its canonical-text
// parse — the reader and the alignment work both consume that text, and it
// should exist by the time anyone opens the book.
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

	// Exactly one text file per book is parsed: the designated primary.
	// Attaching a second format records that the user owns it, and that is
	// all it should cost — parsing every container of the same book would
	// write a second multi-megabyte canonical text nothing ever reads, and
	// leave two different char counts lying around for the sizing queries
	// to disagree over.
	if body.Kind == models.MediaFileEpub && s.epubs != nil {
		for _, f := range files {
			if !f.PrimaryText {
				continue
			}
			if _, perr := s.epubs.EnsureForMediaFile(r.Context(), f); perr != nil {
				if errors.Is(perr, pdf.ErrImageNative) {
					// Not a failure to warn about: the quality gate
					// classified the file image-native and the row is
					// written. The book is paged, its position lives on
					// the page axis, and there is no text by design.
					continue
				}
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

// handlePrimaryTextFile switches which of a book's text-side files is its
// canonical text — the one the reader opens and the one every stored offset
// is measured against.
//
// The parse happens before the switch, deliberately. Promoting onto a text
// nobody has read means promoting onto a length nobody knows, and the store
// refuses that rather than migrating positions blind; parsing here turns
// "not parsed yet" into a normal first switch instead of an error the user
// has to decode. A container that will not parse fails the request with the
// primary untouched, which is the right outcome: nothing was switched to.
func (s *Server) handlePrimaryTextFile(w http.ResponseWriter, r *http.Request) {
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
	entryID := chi.URLParam(r, "entryID")

	files, err := s.store.MediaFilesForEntry(r.Context(), userID, entryID)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errNotFound)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	var target *models.MediaFile
	for i := range files {
		if files[i].ID == fileID && files[i].Kind == models.MediaFileEpub {
			target = &files[i]
			break
		}
	}
	if target == nil {
		fail(w, errNotFound)
		return
	}
	// The parse (or, for a PDF, the classification) happens before the
	// switch, deliberately. Promoting onto a text nobody has read means
	// promoting onto a length nobody knows, and the store refuses that
	// rather than migrating positions blind; parsing here turns "not parsed
	// yet" into a normal first switch instead of an error the user has to
	// decode. A container that will not parse fails the request with the
	// primary untouched, which is the right outcome: nothing was switched
	// to. An image-native PDF is the deliberate exception: it is exactly
	// what a paged switch is for, so its classification is what gets
	// ensured and its refusal is success — a text-native PDF still parses
	// into its canonical text first, the same as any other container.
	if s.epubs != nil && !target.PrimaryText {
		perr := func() error {
			if !strings.EqualFold(filepath.Ext(target.Path), ".pdf") {
				_, err := s.epubs.EnsureForMediaFile(r.Context(), *target)
				return err
			}
			pf, err := s.epubs.EnsurePDFFile(r.Context(), *target)
			if err != nil {
				return err
			}
			if pf.Classification == models.PDFImageNative {
				return nil
			}
			_, err = s.epubs.EnsureForMediaFile(r.Context(), *target)
			return err
		}()
		if perr != nil {
			slog.WarnContext(r.Context(), "parse on promote", "file", target.Path, "error", perr)
			fail(w, errorf(http.StatusUnprocessableEntity,
				"could not read "+path.Base(target.Path)+" as a book, so it cannot become the canonical text"))
			return
		}
	}

	file, err := s.store.SetPrimaryTextFile(r.Context(), userID, entryID, fileID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, errNotFound)
		return
	case errors.Is(err, store.ErrTextNotParsed):
		fail(w, errorf(http.StatusUnprocessableEntity,
			"this file has no parsed text yet; try again once it has been read"))
		return
	case errors.Is(err, store.ErrAttach):
		fail(w, errorf(http.StatusBadRequest, err.Error()))
		return
	case err != nil:
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"file": file})
}

// handlePrimaryAudioEdition switches which of a book's audiobooks it is
// listened to — the recording the timeline is built from and the one every
// stored audio position means.
//
// Unlike its text-side counterpart there is nothing to prepare first: an
// audiobook needs no parse, only files. What the store does need to refuse is
// a recording it cannot play, which is a whole edition sitting on an
// unmounted root; that comes back as a 400 naming the reason rather than a
// silent switch onto a tape with no bytes behind it.
func (s *Server) handlePrimaryAudioEdition(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	editionID, err := strconv.ParseInt(chi.URLParam(r, "editionID"), 10, 64)
	if err != nil || editionID <= 0 {
		fail(w, errorf(http.StatusBadRequest, "invalid audiobook id"))
		return
	}

	edition, err := s.store.SetPrimaryAudioEdition(r.Context(), userID, chi.URLParam(r, "entryID"), editionID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, errNotFound)
		return
	case errors.Is(err, store.ErrAttach):
		fail(w, errorf(http.StatusBadRequest, err.Error()))
		return
	case err != nil:
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"audio_edition": edition})
}

// handleBookFiles lists the files attached to one of the user's book
// entries: text files first with the canonical one at their head (each
// carrying primary_text so the UI can say which), then audio in track order.
//
// The audiobooks come back grouped as well as flat. A book can have several
// recordings attached and only one of them is on the timeline, which is not
// something a client can work out from a list of paths — so the grouping, the
// designation and the derived labels are served alongside the files rather
// than left to be guessed at.
func (s *Server) handleBookFiles(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFrom(r.Context())
	if !ok {
		fail(w, errUnauthorized)
		return
	}

	entryID := chi.URLParam(r, "entryID")
	files, err := s.store.MediaFilesForEntry(r.Context(), user.ID, entryID)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errNotFound)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	editions, err := s.store.AudioEditionsForEntry(r.Context(), user.ID, entryID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"files":          redactPaths(files, user),
		"audio_editions": editions,
	})
}

// redactPaths blanks the NAS root and path on the way out to someone who may
// not manage files. A reader still needs this list — it is how the book
// detail page knows there is an audiobook to play and a text to read, and
// how the player labels its tracks — but the directory layout of somebody
// else's server is not part of that, and the file name is often the only
// thing in the payload that was never meant to be read aloud.
//
// The rest of the row survives untouched: id, kind, duration, primary-text
// flag, container metadata. Nothing downstream keys on path.
func redactPaths(files []models.MediaFile, user models.User) []models.MediaFile {
	if user.CanManageMedia() {
		return files
	}
	out := make([]models.MediaFile, len(files))
	for i, f := range files {
		f.Root, f.Path = "", path.Base(f.Path)
		out[i] = f
	}
	return out
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
