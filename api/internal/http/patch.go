package http

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/collinpendleton/backhog/api/internal/store"
)

// decodeRaw decodes a JSON body into a generic map. PATCH handlers use this so
// they can distinguish an explicit null (clear the field) from an absent key
// (leave the field alone) — a distinction typed structs cannot express.
func decodeRaw(r *http.Request, dst *map[string]any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	if err := dec.Decode(dst); err != nil {
		return errorf(http.StatusBadRequest, "invalid request body: "+err.Error())
	}
	return nil
}

var entryPatchFields = map[string]bool{
	"status": true, "platform_id": true, "user_rating": true, "notes": true,
}

// parseUpdateEntry converts a decoded PATCH body into a store.EntryUpdate.
func parseUpdateEntry(raw map[string]any) (store.EntryUpdate, error) {
	var u store.EntryUpdate

	for key := range raw {
		if !entryPatchFields[key] {
			return u, errorf(http.StatusBadRequest, fmt.Sprintf("unknown field %q", key))
		}
	}

	if v, ok := raw["status"]; ok {
		s, ok := v.(string)
		if !ok {
			return u, errorf(http.StatusBadRequest, "status must be a string")
		}
		u.Status = &s
	}

	if v, ok := raw["platform_id"]; ok {
		if v == nil {
			u.ClearPlatform = true
		} else {
			n, ok := v.(float64)
			if !ok {
				return u, errorf(http.StatusBadRequest, "platform_id must be a number or null")
			}
			id := int64(n)
			u.PlatformID = &id
		}
	}

	if v, ok := raw["user_rating"]; ok {
		if v == nil {
			u.ClearRating = true
		} else {
			n, ok := v.(float64)
			if !ok {
				return u, errorf(http.StatusBadRequest, "user_rating must be a number or null")
			}
			rating := int(n)
			u.UserRating = &rating
		}
	}

	if v, ok := raw["notes"]; ok {
		s, ok := v.(string)
		if !ok {
			return u, errorf(http.StatusBadRequest, "notes must be a string")
		}
		u.Notes = &s
	}

	return u, nil
}
