package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/collinpendleton/backhog/api/internal/auth"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

func (s *Server) handleGetProjects(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	projects, err := s.store.GetProjects(r.Context(), userID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": projects})
}

type createProjectRequest struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Kind        string          `json:"kind"`
	// Media is the arena the project lives in ('game' or 'book'); empty
	// defaults to game. The client sends the arena it was created from.
	Media       string          `json:"media"`
	TargetCount *int            `json:"target_count"`
	Rules       *models.RuleSet `json:"rules"`
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	var body createProjectRequest
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}

	project, err := s.store.CreateProject(r.Context(), userID,
		body.Name, body.Description, body.Kind, body.Media, body.TargetCount, body.Rules)
	if err != nil {
		fail(w, errorf(http.StatusBadRequest, err.Error()))
		return
	}
	writeJSON(w, http.StatusCreated, project)
}

// handleGetProject returns the project along with its resolved items, so the
// UI can render a rule goal's match pool without a second round trip.
func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	projectID := chi.URLParam(r, "projectID")

	project, err := s.store.GetProject(r.Context(), userID, projectID)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errNotFound)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}

	items, err := s.store.ProjectItemsFor(r.Context(), userID, projectID)
	if err != nil {
		fail(w, errorf(http.StatusBadRequest, err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project": project, "items": items})
}

var projectPatchFields = map[string]bool{
	"name": true, "description": true, "target_count": true, "rules": true, "completed": true,
}

// parseProjectPatch converts a decoded PATCH body into a store.ProjectUpdate.
// target_count needs raw-value parsing so an explicit null (clear the target)
// can be told apart from an omitted key (leave it alone).
func parseProjectPatch(raw map[string]json.RawMessage) (store.ProjectUpdate, error) {
	var u store.ProjectUpdate

	for key := range raw {
		if !projectPatchFields[key] {
			return u, errorf(http.StatusBadRequest, fmt.Sprintf("unknown field %q", key))
		}
	}

	decode := func(key string, dst any) error {
		return json.Unmarshal(raw[key], dst)
	}

	if _, ok := raw["name"]; ok {
		var name string
		if err := decode("name", &name); err != nil {
			return u, errorf(http.StatusBadRequest, "name must be a string")
		}
		u.Name = &name
	}
	if _, ok := raw["description"]; ok {
		var description string
		if err := decode("description", &description); err != nil {
			return u, errorf(http.StatusBadRequest, "description must be a string")
		}
		u.Description = &description
	}
	if value, ok := raw["target_count"]; ok {
		if isJSONNull(value) {
			u.ClearTarget = true
		} else {
			var target int
			if err := decode("target_count", &target); err != nil {
				return u, errorf(http.StatusBadRequest, "target_count must be a number or null")
			}
			u.TargetCount = &target
		}
	}
	if value, ok := raw["rules"]; ok {
		if !isJSONNull(value) {
			var rules models.RuleSet
			if err := decode("rules", &rules); err != nil {
				return u, errorf(http.StatusBadRequest, "rules must be a rule set")
			}
			u.Rules = &rules
		}
	}
	if _, ok := raw["completed"]; ok {
		var completed bool
		if err := decode("completed", &completed); err != nil {
			return u, errorf(http.StatusBadRequest, "completed must be a boolean")
		}
		u.Completed = &completed
	}
	return u, nil
}

func isJSONNull(value json.RawMessage) bool {
	return string(value) == "null"
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(&raw); err != nil {
		fail(w, errorf(http.StatusBadRequest, "invalid request body: "+err.Error()))
		return
	}
	body, err := parseProjectPatch(raw)
	if err != nil {
		fail(w, err)
		return
	}

	project, err := s.store.UpdateProject(r.Context(), userID, chi.URLParam(r, "projectID"), body)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errNotFound)
		return
	}
	if err != nil {
		fail(w, errorf(http.StatusBadRequest, err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, project)
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	err = s.store.DeleteProject(r.Context(), userID, chi.URLParam(r, "projectID"))
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

func (s *Server) handleAddProjectItem(w http.ResponseWriter, r *http.Request) {
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

	err = s.store.AddProjectItem(r.Context(), userID, chi.URLParam(r, "projectID"), body.EntryID)
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

func (s *Server) handleRemoveProjectItem(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	err = s.store.RemoveProjectItem(r.Context(), userID,
		chi.URLParam(r, "projectID"), chi.URLParam(r, "entryID"))
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errNotFound)
		return
	}
	if err != nil {
		fail(w, errorf(http.StatusBadRequest, err.Error()))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSetProjectItemDone applies the manual per-item completion override:
// true / false force it, null returns the item to status-derived completion.
func (s *Server) handleSetProjectItemDone(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(&raw); err != nil {
		fail(w, errorf(http.StatusBadRequest, "invalid request body: "+err.Error()))
		return
	}
	value, ok := raw["done"]
	if !ok {
		fail(w, errorf(http.StatusBadRequest, "done is required"))
		return
	}

	var done *bool
	if !isJSONNull(value) {
		var b bool
		if err := json.Unmarshal(value, &b); err != nil {
			fail(w, errorf(http.StatusBadRequest, "done must be a boolean or null"))
			return
		}
		done = &b
	}

	err = s.store.SetProjectItemDone(r.Context(), userID,
		chi.URLParam(r, "projectID"), chi.URLParam(r, "entryID"), done)
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

func (s *Server) handleReorderProjectItem(w http.ResponseWriter, r *http.Request) {
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

	err = s.store.MoveProjectItem(r.Context(), userID, chi.URLParam(r, "projectID"),
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

// handleEntryProjects returns which checklist projects an entry belongs to.
func (s *Server) handleEntryProjects(w http.ResponseWriter, r *http.Request) {
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

	ids, err := s.store.ProjectIDsForEntry(r.Context(), userID, chi.URLParam(r, "entryID"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project_ids": ids})
}
