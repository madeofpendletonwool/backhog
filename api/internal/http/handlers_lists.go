package http

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/collinpendleton/backhog/api/internal/auth"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

func (s *Server) handleGetLists(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	lists, err := s.store.GetLists(r.Context(), userID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lists": lists})
}

type createListRequest struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Kind        string          `json:"kind"`
	Rules       *models.RuleSet `json:"rules"`
}

func (s *Server) handleCreateList(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	var body createListRequest
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	if body.Kind == "" {
		body.Kind = "manual"
	}

	list, err := s.store.CreateList(r.Context(), userID, body.Name, body.Description, body.Kind, body.Rules)
	if err != nil {
		fail(w, errorf(http.StatusBadRequest, err.Error()))
		return
	}
	writeJSON(w, http.StatusCreated, list)
}

// handleGetList returns the list along with its resolved entries, so the UI can
// render a smart list without a second round trip.
func (s *Server) handleGetList(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	listID := chi.URLParam(r, "listID")

	list, err := s.store.GetList(r.Context(), userID, listID)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errNotFound)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}

	entries, err := s.store.ListEntriesFor(r.Context(), userID, listID)
	if err != nil {
		fail(w, errorf(http.StatusBadRequest, err.Error()))
		return
	}
	list.Count = len(entries)
	writeJSON(w, http.StatusOK, map[string]any{"list": list, "entries": entries})
}

type updateListRequest struct {
	Name        *string         `json:"name"`
	Description *string         `json:"description"`
	Rules       *models.RuleSet `json:"rules"`
}

func (s *Server) handleUpdateList(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	var body updateListRequest
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}

	list, err := s.store.UpdateList(r.Context(), userID, chi.URLParam(r, "listID"),
		body.Name, body.Description, body.Rules)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errNotFound)
		return
	}
	if err != nil {
		fail(w, errorf(http.StatusBadRequest, err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleDeleteList(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	err = s.store.DeleteList(r.Context(), userID, chi.URLParam(r, "listID"))
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errNotFound)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type listItemRequest struct {
	EntryID string `json:"entry_id"`
}

func (s *Server) handleAddListItem(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	var body listItemRequest
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	if body.EntryID == "" {
		fail(w, errorf(http.StatusBadRequest, "entry_id is required"))
		return
	}

	err = s.store.AddListItem(r.Context(), userID, chi.URLParam(r, "listID"), body.EntryID)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errNotFound)
		return
	}
	if err != nil {
		fail(w, errorf(http.StatusBadRequest, err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleRemoveListItem(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	err = s.store.RemoveListItem(r.Context(), userID, chi.URLParam(r, "listID"), chi.URLParam(r, "entryID"))
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errNotFound)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleReorderListItem(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	var body reorderRequest
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	if body.EntryID == "" {
		fail(w, errorf(http.StatusBadRequest, "entry_id is required"))
		return
	}

	err = s.store.MoveListItem(r.Context(), userID, chi.URLParam(r, "listID"),
		body.EntryID, body.BeforeID, body.AfterID)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errNotFound)
		return
	}
	if err != nil {
		fail(w, errorf(http.StatusBadRequest, err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleEntryLists returns which manual lists an entry belongs to.
func (s *Server) handleEntryLists(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	// Confirm the entry is ours first, so this can't be used to probe ids.
	if _, err := s.store.GetEntry(r.Context(), userID, chi.URLParam(r, "entryID")); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fail(w, errNotFound)
			return
		}
		fail(w, err)
		return
	}

	ids, err := s.store.ListIDsForEntry(r.Context(), userID, chi.URLParam(r, "entryID"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"list_ids": ids})
}

// handleSmartFields describes the queryable fields so the rule builder does not
// have to hard-code them, and stays in sync with the compiler's whitelist.
func (s *Server) handleSmartFields(w http.ResponseWriter, r *http.Request) {
	type fieldDTO struct {
		Key   string   `json:"key"`
		Label string   `json:"label"`
		Type  string   `json:"type"`
		Ops   []string `json:"ops"`
		Enum  []string `json:"enum,omitempty"`
	}

	fields := store.SmartFields()
	out := make([]fieldDTO, 0, len(fields))
	for key, f := range fields {
		out = append(out, fieldDTO{Key: key, Label: f.Label, Type: f.Type, Ops: f.Ops, Enum: f.Enum})
	}
	writeJSON(w, http.StatusOK, map[string]any{"fields": out})
}
