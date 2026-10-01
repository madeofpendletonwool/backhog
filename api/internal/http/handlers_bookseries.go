package http

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/collinpendleton/backhog/api/internal/auth"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// Book series read surface (MAD-469): the deterministic half of the
// returning-reader feature. Membership is declared by Calibre sidecar
// evidence (written at attach, healed by the boot walker) and read here as
// the caller's own shelf, in reading order, with each member's status and
// stored position — everything an external client needs to scope a
// "story so far" to finished books plus the current position, with the
// spoiler clamp doing the actual withholding on every text read.

// seriesIndexRow is one series the caller's shelf holds a member of.
type seriesIndexRow struct {
	Name     string `json:"name"`
	Books    int    `json:"books"`
	Finished int    `json:"finished"`
	Reading  int    `json:"reading"`
}

type seriesIndexResponse struct {
	Series []seriesIndexRow `json:"series"`
}

// seriesPositionView is the stored reading position, the same facts the
// position endpoint reports, reduced to what a series scope needs.
type seriesPositionView struct {
	Mode       string  `json:"position_mode"`
	CharOffset int     `json:"char_offset"`
	Percent    float64 `json:"percent"`
}

// seriesBookView is one member of a series. Finished says the reader has
// been through the whole book — for it, every clamped read serves the full
// text — while a mid-read member's position is exactly where the clamp
// stops it.
type seriesBookView struct {
	EntryID          string             `json:"entry_id"`
	BookID           string             `json:"book_id"`
	Title            string             `json:"title"`
	Authors          []string           `json:"authors"`
	Status           string             `json:"status"`
	Finished         bool               `json:"finished"`
	SeriesNumber     *float64           `json:"series_number,omitempty"`
	FirstPublishYear *int               `json:"first_publish_year,omitempty"`
	Position         seriesPositionView `json:"position"`
	DeepLink         string             `json:"deep_link"`
}

type seriesDetailResponse struct {
	Name  string           `json:"name"`
	Books []seriesBookView `json:"books"`
}

// handleBookSeriesIndex lists every series the caller's shelf holds a
// member of, with counts. The books arena's own series surface: the games
// series endpoints answer a different catalogue.
func (s *Server) handleBookSeriesIndex(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	rows, err := s.store.BookSeriesIndex(r.Context(), userID)
	if err != nil {
		fail(w, err)
		return
	}
	out := seriesIndexResponse{Series: make([]seriesIndexRow, 0, len(rows))}
	for _, row := range rows {
		out.Series = append(out.Series, seriesIndexRow{
			Name: row.Name, Books: row.Books,
			Finished: row.Finished, Reading: row.Reading,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleBookSeriesDetail lists the caller's books of one series in reading
// order with per-book status and position. A series the caller holds
// nothing from is indistinguishable from one that does not exist: 404,
// the same scoping every shelf row gets.
func (s *Server) handleBookSeriesDetail(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	name := strings.TrimSpace(chi.URLParam(r, "seriesName"))
	if name == "" {
		fail(w, errNotFound)
		return
	}
	books, err := s.store.BookSeriesDetail(r.Context(), userID, name)
	if err != nil {
		fail(w, err)
		return
	}
	if len(books) == 0 {
		fail(w, errNotFound)
		return
	}
	out := seriesDetailResponse{Name: name, Books: make([]seriesBookView, 0, len(books))}
	for _, b := range books {
		view := seriesBookView{
			EntryID: b.EntryID, BookID: b.BookID, Title: b.Title,
			Authors: decodeAuthorsJSON(b.AuthorsJSON), Status: b.Status,
			Finished:     b.Status == models.StatusPlayed,
			SeriesNumber: b.SeriesNumber, FirstPublishYear: b.FirstPublishYear,
			Position: seriesPositionView{
				Mode: b.PositionMode, CharOffset: b.CharOffset, Percent: b.Percent,
			},
		}
		view.DeepLink = seriesDeepLink(b)
		out.Books = append(out.Books, view)
	}
	writeJSON(w, http.StatusOK, out)
}

// seriesDeepLink jumps the reader to the book's stored position without
// moving it: a peek link for a text book that is underway, the plain reader
// for one that is not (nothing to peek at) and for a paged book (whose
// position the offset link cannot express).
func seriesDeepLink(b store.SeriesBook) string {
	if b.PositionMode == models.PositionModeText && b.CharOffset > 0 {
		return deepLink(b.EntryID, b.CharOffset)
	}
	return "/books/" + b.EntryID + "/read"
}

// decodeAuthorsJSON reads a work's authors column. An unreadable value is
// an empty list, not an error: the column is written by the same store that
// wrote the row, and a series list must not 500 over a display field.
func decodeAuthorsJSON(raw string) []string {
	var authors []string
	if raw == "" {
		return []string{}
	}
	if err := json.Unmarshal([]byte(raw), &authors); err != nil {
		return []string{}
	}
	if authors == nil {
		return []string{}
	}
	return authors
}
