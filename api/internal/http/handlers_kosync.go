package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/collinpendleton/backhog/api/internal/auth"
	"github.com/collinpendleton/backhog/api/internal/books"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// The KOReader sync protocol (MAD-831), as KOReader's progress-sync plugin
// and CrossPoint speak it, mounted at /api/kosync:
//
//	POST /users/create          never creates anything (see below)
//	GET  /users/auth            login check (handleKosyncAuth)
//	PUT  /syncs/progress        a device pushes where it is
//	GET  /syncs/progress/{doc}  a device asks where Backhog is
//
// A push writes through to book_progress — the one position truth — with
// the XPointer translated to a canonical offset (MAD-832). A pull answers
// with the device's own last push when Backhog has not moved since, and
// otherwise with Backhog's position translated back, so the web reader,
// the Android app and every device hand off to each other.
//
// The device names a book only by a document id (a hash; see
// internal/ereader). Ids resolve per user through kosync_documents (the
// downloads' hashes), then on-demand hashing, then the push's own title
// and author, and an id nothing places is parked in kosync_unmatched with
// a 200 — an error on a 6-inch screen helps nobody, and the owner can link
// it from the web.

// kosyncDevice names Backhog as the "device" of a position it computed.
const (
	kosyncDevice   = "Backhog"
	kosyncDeviceID = "backhog-server"
)

// kosyncHashBudget bounds the on-demand hashing one sync request may do for
// an unknown document: each candidate is a file open and twelve 1 KiB
// reads off the NAS. Entries hashed once are never hashed again (until a
// download refreshes them), so a large library converges over a few syncs.
const kosyncHashBudget = 200

// kosyncSessionGap is the longest stretch between two pushes from one
// device that still counts as one sitting of reading.
const kosyncSessionGap = 4 * time.Hour

type kosyncPush struct {
	Document   string  `json:"document"`
	Progress   string  `json:"progress"`
	Percentage float64 `json:"percentage"`
	Device     string  `json:"device"`
	DeviceID   string  `json:"device_id"`
	Metadata   *struct {
		Filename string `json:"filename"`
		Title    string `json:"title"`
		Authors  string `json:"authors"`
	} `json:"metadata"`
	// CrossPoint's rich position (server type "CrossPoint"/"Other"). Only
	// its xpath is used: the spine/paragraph ordinals are CrossPoint's own
	// layout and do not line up with the canonical blocks reliably.
	Position *struct {
		XPath string `json:"xpath"`
	} `json:"position"`
}

// handleKosyncCreate is registration, which kosync does not get to do:
// accounts are made in Backhog, and a device signs in with a token. The
// reference server answers an existing username with 402, which KOReader
// and CrossPoint both show as "already registered" — the nudge toward
// signing in instead — so that is the answer for a name that exists; any
// other gets 403 with the same advice.
func (s *Server) handleKosyncCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body)
	if name := strings.TrimSpace(body.Username); name != "" {
		if exists, err := s.store.UsernameExists(r.Context(), name); err == nil && exists {
			writeJSON(w, http.StatusPaymentRequired, map[string]any{
				"code": 2002, "message": "Username is already registered. Sign in with a Backhog device token as the password."})
			return
		}
	}
	writeJSON(w, http.StatusForbidden, map[string]any{
		"code": 2005, "message": "Registration is closed: create a device token in Backhog and sign in with it."})
}

// handleKosyncPush stores a device's position.
func (s *Server) handleKosyncPush(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	var push kosyncPush
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&push); err != nil {
		fail(w, errorf(http.StatusBadRequest, "invalid JSON"))
		return
	}
	push.Document = strings.ToLower(strings.TrimSpace(push.Document))
	if push.Document == "" || math.IsNaN(push.Percentage) {
		fail(w, errorf(http.StatusBadRequest, "document and progress are required"))
		return
	}
	push.Percentage = math.Max(0, math.Min(1, push.Percentage))
	now := time.Now().UTC()
	reply := map[string]any{"document": push.Document, "timestamp": now.Unix()}

	entryID, err := s.kosyncResolve(r.Context(), userID, push.Document, &push)
	if errors.Is(err, store.ErrNotFound) {
		u := store.KosyncUnmatched{DocHash: push.Document, Progress: push.Progress,
			Percentage: push.Percentage, Device: push.Device, SeenAt: now}
		if push.Metadata != nil {
			u.Title, u.Authors, u.Filename = push.Metadata.Title, push.Metadata.Authors, push.Metadata.Filename
		}
		if err := s.store.SaveKosyncUnmatched(r.Context(), userID, u); err != nil {
			fail(w, err)
			return
		}
		slog.InfoContext(r.Context(), "kosync push for an unknown document", "document", push.Document)
		writeJSON(w, http.StatusOK, reply)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}

	bookID, err := s.store.BookIDForEntry(r.Context(), userID, entryID)
	if err != nil {
		fail(w, err)
		return
	}
	files, err := s.store.CanAccessBookFiles(r.Context(), userID, bookID)
	if err != nil {
		fail(w, err)
		return
	}
	if s.epubs != nil && files {
		// A book first opened on the device has never had its text parsed;
		// parse it now so the push has a text to land in. A failure here
		// (no EPUB, DRM) just leaves nothing to place the position in.
		_, _ = s.epubs.EnsureForEntry(r.Context(), userID, entryID)
	}
	views, err := s.loadBookViews(r.Context(), userID, entryID, bookID, files)
	if err != nil {
		fail(w, err)
		return
	}
	if views.pageCount > 0 || views.charCount == 0 {
		// Nothing to place a text position in (a paged primary, or a text
		// never parsed): accept and keep the push, move nothing.
		writeJSON(w, http.StatusOK, reply)
		return
	}

	offset := s.kosyncPlace(r.Context(), userID, entryID, &push, views.charCount)

	previous, prevErr := s.store.GetKosyncProgress(r.Context(), entryID)
	write := store.ProgressWrite{
		Mode:       models.PositionModeText,
		CharOffset: offset,
		Source:     models.PositionSourceRead,
	}
	dropRawAudioIfDerivable(views, &write)
	write.PercentComplete = percentComplete(write, views)
	if _, err := s.store.SaveBookProgress(r.Context(), userID, entryID, write); err != nil {
		fail(w, err)
		return
	}
	if err := s.store.SaveKosyncProgress(r.Context(), store.KosyncProgress{
		EntryID: entryID, DocHash: push.Document, Progress: push.Progress,
		Percentage: push.Percentage, Device: push.Device, DeviceID: push.DeviceID,
		CharOffset: offset, PushedAt: now,
	}); err != nil {
		fail(w, err)
		return
	}

	// Two pushes from one device a sitting apart, the second further on,
	// bracket a stretch of reading. Pushes are sparse (CrossPoint only syncs
	// when asked), so this is an estimate, and the session says where it
	// came from by having no pages turned and a device-shaped gap.
	if prevErr == nil && previous.DeviceID == push.DeviceID && push.DeviceID != "" &&
		offset > previous.CharOffset && now.Sub(previous.PushedAt) <= kosyncSessionGap &&
		now.Sub(previous.PushedAt) >= time.Minute {
		if _, err := s.store.AddReadingSession(r.Context(), userID, entryID, models.ReadingSession{
			StartedAt: previous.PushedAt, EndedAt: now, Mode: models.ReadingModeRead,
			CharsAdvanced: offset - previous.CharOffset,
		}); err != nil {
			slog.WarnContext(r.Context(), "kosync reading session not recorded", "entry_id", entryID, "error", err)
		}
	}

	writeJSON(w, http.StatusOK, reply)
}

// handleKosyncPull answers where Backhog has a book.
func (s *Server) handleKosyncPull(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	doc := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "document")))
	entryID, err := s.kosyncResolve(r.Context(), userID, doc, nil)
	if errors.Is(err, store.ErrNotFound) {
		// The reference server's "nothing stored": 200 with no fields.
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	progress, err := s.store.BookProgress(r.Context(), userID, entryID)
	if err != nil {
		fail(w, err)
		return
	}
	if progress.UpdatedAt.IsZero() || progress.PositionMode != models.PositionModeText {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}

	// Backhog has not moved since this book's last push: hand back the
	// push itself, exact to the character the device had.
	if last, err := s.store.GetKosyncProgress(r.Context(), entryID); err == nil &&
		last.CharOffset == progress.CharOffset {
		writeJSON(w, http.StatusOK, map[string]any{
			"document": doc, "progress": last.Progress, "percentage": last.Percentage,
			"device": last.Device, "device_id": last.DeviceID, "timestamp": last.PushedAt.Unix(),
		})
		return
	}

	out := map[string]any{
		"document":   doc,
		"percentage": math.Round(progress.PercentComplete*100) / 10000,
		"device":     kosyncDevice,
		"device_id":  kosyncDeviceID,
		"timestamp":  progress.UpdatedAt.Unix(),
		"progress":   "",
	}
	if s.epubs != nil {
		if loc, err := s.epubs.LocatorForEntry(r.Context(), userID, entryID); err == nil {
			if xp, ok := loc.ToXPointer(progress.CharOffset); ok {
				out["progress"] = xp
			}
			if n := loc.CharCount(); n > 0 {
				out["percentage"] = math.Round(float64(progress.CharOffset)/float64(n)*10000) / 10000
			}
		} else {
			slog.WarnContext(r.Context(), "kosync pull without an XPointer", "entry_id", entryID, "error", err)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// kosyncResolve finds the entry a document id names: the stored ids and
// bindings first, then — on a miss — hashing the entries never hashed, and
// finally (for a push) the push's own title and author. ErrNotFound when
// nothing places it.
func (s *Server) kosyncResolve(ctx context.Context, userID, doc string, push *kosyncPush) (string, error) {
	if doc == "" {
		return "", store.ErrNotFound
	}
	entryID, err := s.store.KosyncEntryForHash(ctx, userID, doc)
	if !errors.Is(err, store.ErrNotFound) {
		return entryID, err
	}

	if s.epubs != nil {
		pending, err := s.store.EntriesWithoutDocumentIDs(ctx, userID, kosyncHashBudget)
		if err != nil {
			return "", err
		}
		for _, id := range pending {
			s.hashEntry(ctx, userID, id)
		}
		if len(pending) > 0 {
			entryID, err = s.store.KosyncEntryForHash(ctx, userID, doc)
			if !errors.Is(err, store.ErrNotFound) {
				return entryID, err
			}
		}
	}

	if push != nil && push.Metadata != nil && strings.TrimSpace(push.Metadata.Title) != "" {
		if id, ok := s.matchByMetadata(ctx, userID, push.Metadata.Title, push.Metadata.Authors); ok {
			if err := s.store.BindKosyncDocument(ctx, userID, doc, id, "metadata"); err != nil {
				return "", err
			}
			return id, nil
		}
	}
	return "", store.ErrNotFound
}

// hashEntry computes and stores one entry's document ids, quietly: an entry
// that cannot be opened simply stays unhashed.
func (s *Server) hashEntry(ctx context.Context, userID, entryID string) {
	ef, err := s.epubs.OpenEpubFile(ctx, userID, entryID)
	if err != nil {
		return
	}
	defer ef.File.Close()
	book, err := s.store.GetBook(ctx, ef.BookID)
	if err != nil {
		return
	}
	author, title := feedStrings(book)
	if err := s.refreshKosyncDocuments(ctx, entryID, ef, author, title); err != nil {
		slog.WarnContext(ctx, "kosync document ids not computed", "entry_id", entryID, "error", err)
	}
}

// matchByMetadata places a push by its title and author when exactly one
// readable entry of the user's carries that title and shares an author
// surname with it — strict, because a wrong match silently moves the wrong
// book.
func (s *Server) matchByMetadata(ctx context.Context, userID, title, authors string) (string, bool) {
	candidates, err := s.store.EntriesByTitle(ctx, userID, strings.TrimSpace(title))
	if err != nil || len(candidates) == 0 {
		return "", false
	}
	want := strings.ToLower(authors)
	var match string
	for id, names := range candidates {
		ok := want == "" && len(candidates) == 1
		for _, n := range names {
			fields := strings.Fields(strings.ToLower(n))
			if len(fields) > 0 && strings.Contains(want, fields[len(fields)-1]) {
				ok = true
			}
		}
		if ok {
			if match != "" {
				return "", false
			}
			match = id
		}
	}
	return match, match != ""
}

// kosyncPlace turns a push into a canonical offset at the best precision
// available: the XPointer, then CrossPoint's rich-position xpath, then the
// percentage on a block boundary, then the bare percentage.
func (s *Server) kosyncPlace(ctx context.Context, userID, entryID string, push *kosyncPush, charCount int) int {
	if s.epubs != nil {
		loc, err := s.epubs.LocatorForEntry(ctx, userID, entryID)
		if err == nil {
			if off, ok := loc.FromXPointer(push.Progress); ok {
				return off
			}
			if push.Position != nil {
				if off, ok := loc.FromXPointer(push.Position.XPath); ok {
					return off
				}
			}
			slog.DebugContext(ctx, "kosync push placed by percentage", "entry_id", entryID)
			return loc.FromPercent(push.Percentage)
		}
		slog.WarnContext(ctx, "kosync locator unavailable", "entry_id", entryID, "error", err)
	}
	return books.PercentOffset(nil, charCount, push.Percentage)
}

// handleKosyncUnmatched lists the caller's device pushes that no book
// claimed, for the settings page's "link to a book" fix-up.
func (s *Server) handleKosyncUnmatched(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	rows, err := s.store.ListKosyncUnmatched(r.Context(), userID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"unmatched": rows})
}

// handleKosyncLink binds an unmatched document id to one of the caller's
// books. The next push or pull for that id lands on it.
func (s *Server) handleKosyncLink(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	var body struct {
		EntryID string `json:"entry_id"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	doc := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "document")))
	err = s.store.BindKosyncDocument(r.Context(), userID, doc, body.EntryID, "manual")
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
