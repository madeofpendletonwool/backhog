package http

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/collinpendleton/backhog/api/internal/auth"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// handleBookShares serves the share picker for one of the caller's book
// entries: every account the book could be shared with, flagged with whether
// it already is and whether they have it on their own shelf.
func (s *Server) handleBookShares(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	candidates, err := s.store.ShareCandidatesForEntry(r.Context(), userID, chi.URLParam(r, "entryID"))
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errNotFound)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidates": candidates})
}

type shareRequest struct {
	UserID string `json:"user_id"`
}

// handleShareBook grants one account access to the files behind one of the
// caller's books. Sharing twice is idempotent, so the picker can send the
// state it wants without first reading the state that is.
func (s *Server) handleShareBook(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	var body shareRequest
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	if body.UserID == "" {
		fail(w, errorf(http.StatusBadRequest, "user_id is required"))
		return
	}

	share, err := s.store.ShareBook(r.Context(), userID, chi.URLParam(r, "entryID"), body.UserID)
	switch {
	case errors.Is(err, store.ErrSelfShare):
		fail(w, errorf(http.StatusBadRequest, "you already have this book"))
	case errors.Is(err, store.ErrNotFound):
		fail(w, errNotFound)
	case err != nil:
		fail(w, err)
	default:
		writeJSON(w, http.StatusOK, share)
	}
}

// handleUnshareBook withdraws one grant. Only the grant goes: the recipient
// keeps their entry, their position and their reading sessions, and the book
// becomes one with nothing attached until it is shared again.
func (s *Server) handleUnshareBook(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	err = s.store.RevokeShare(r.Context(), userID,
		chi.URLParam(r, "entryID"), chi.URLParam(r, "userID"))
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errNotFound)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleSharesOverview answers both halves of "who has what": the books this
// account has shared out, and the books other people have shared with it.
// The second list is the "shared with me" shelf — a share grants access but
// never puts rows in someone else's library, so a book appears here with
// in_library false until they add it themselves.
func (s *Server) handleSharesOverview(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	shared, err := s.store.SharesByOwner(r.Context(), userID)
	if err != nil {
		fail(w, err)
		return
	}
	received, err := s.store.SharesWithUser(r.Context(), userID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"shared": shared, "received": received})
}
