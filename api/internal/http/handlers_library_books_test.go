package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/metadata"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// post is the JSON POST helper the library flow needs.
func (a *booksTestApp) post(t *testing.T, path string, body any) (int, []byte) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := a.client.Post(a.ts.URL+path, "application/json", bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.Bytes()
}

// patch is the JSON PATCH helper the status-change flow needs.
func (a *booksTestApp) patch(t *testing.T, path string, body any) (int, []byte) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPatch, a.ts.URL+path, bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("build PATCH %s: %v", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		t.Fatalf("PATCH %s: %v", path, err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.Bytes()
}

func (a *booksTestApp) delete(t *testing.T, path string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, a.ts.URL+path, nil)
	if err != nil {
		t.Fatalf("build DELETE %s: %v", path, err)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", path, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func decodeEntry(t *testing.T, body []byte) models.Entry {
	t.Helper()
	var entry models.Entry
	if err := json.Unmarshal(body, &entry); err != nil {
		t.Fatalf("decode entry: %v (body %s)", err, body)
	}
	return entry
}

// TestBookLibraryEndToEnd walks the whole write/read path over HTTP: add a
// book (enriching it from the provider on the way in), read it back through
// the shared library endpoints, queue it, list it, change its status, and
// delete it — the same endpoints the games arena uses.
func TestBookLibraryEndToEnd(t *testing.T) {
	app := newBooksTestApp(t)

	// Adding an uncached book pulls the work AND its editions from the
	// provider on the way in, so the add response can offer the printing
	// picker without a second round trip.
	status, body := app.post(t, "/api/library", map[string]any{"book_id": "OL1168083W"})
	if status != http.StatusCreated {
		t.Fatalf("add book: status %d, body %s", status, body)
	}
	entry := decodeEntry(t, body)
	if entry.MediaType != "book" || entry.Book == nil || entry.Book.Title != "The Hobbit" {
		t.Fatalf("created entry = %+v", entry)
	}
	if entry.Game != nil {
		t.Error("game subject serialised onto a book entry")
	}
	if len(entry.Book.Editions) != 2 {
		t.Errorf("editions on add = %d, want 2 (enrich-on-add)", len(entry.Book.Editions))
	}
	if entry.Book.Authors[0] != "J. R. R. Tolkien" {
		t.Errorf("authors = %v", entry.Book.Authors)
	}
	entryID := entry.ID

	// Adding the same work again is a conflict.
	if status, _ := app.post(t, "/api/library", map[string]any{"book_id": "OL1168083W"}); status != http.StatusConflict {
		t.Errorf("duplicate add: status %d, want 409", status)
	}

	// A book with an edition picked that belongs to another work: refused.
	status, body = app.post(t, "/api/library", map[string]any{
		"book_id": "OL1168083W", "edition_id": "OL9999999M"})
	if status != http.StatusBadRequest {
		t.Fatalf("foreign edition: status %d, want 400", status)
	}

	// Rejections: both subject ids, and neither.
	if status, _ := app.post(t, "/api/library", map[string]any{"game_id": 5, "book_id": "OL1168083W"}); status != http.StatusBadRequest {
		t.Errorf("both ids: status %d, want 400", status)
	}
	if status, _ := app.post(t, "/api/library", map[string]any{"status": "backlog"}); status != http.StatusBadRequest {
		t.Errorf("no id: status %d, want 400", status)
	}
	if status, _ := app.post(t, "/api/library", map[string]any{"book_id": "not-a-key"}); status != http.StatusBadRequest {
		t.Errorf("bad work key: status %d, want 400", status)
	}

	// Read it back through the entry endpoint.
	status, body = app.get(t, "/api/library/"+entryID)
	if status != http.StatusOK {
		t.Fatalf("get entry: status %d, body %s", status, body)
	}
	if entry = decodeEntry(t, body); entry.Book == nil || entry.Book.ID != "OL1168083W" {
		t.Errorf("get entry = %+v", entry)
	}

	// Seed a game alongside for the mixed page.
	if err := app.store.UpsertGame(context.Background(), detailGameForLibrary(), ""); err != nil {
		t.Fatalf("seed game: %v", err)
	}
	userID := app.store.DB().QueryRowContext(context.Background(), `SELECT id FROM users LIMIT 1`)
	var uid string
	if err := userID.Scan(&uid); err != nil {
		t.Fatal(err)
	}
	gameEntry, err := app.store.AddEntry(context.Background(), uid, 100, models.StatusBacklog, nil)
	if err != nil {
		t.Fatalf("seed game entry: %v", err)
	}

	// media=book returns only the book; no media returns both, sorted as one page.
	status, body = app.get(t, "/api/library?media=book")
	if status != http.StatusOK {
		t.Fatalf("list books: status %d, body %s", status, body)
	}
	var list struct {
		Entries []models.Entry `json:"entries"`
		Total   int            `json:"total"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Entries) != 1 || list.Total != 1 || list.Entries[0].Book == nil {
		t.Fatalf("media=book list = %+v (total %d)", list.Entries, list.Total)
	}

	status, body = app.get(t, "/api/library?sort=title")
	if status != http.StatusOK {
		t.Fatalf("mixed list: status %d", status)
	}
	list.Entries, list.Total = nil, 0
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if list.Total != 2 {
		t.Fatalf("mixed total = %d, want 2", list.Total)
	}
	// "Mass Effect" sorts before "The Hobbit".
	if list.Entries[0].MediaType != "game" || list.Entries[1].MediaType != "book" {
		t.Errorf("title sort order = %s then %s, want game before book",
			list.Entries[0].MediaType, list.Entries[1].MediaType)
	}

	// The queue carries the book alongside the game; a book can be reordered
	// within it.
	status, body = app.get(t, "/api/library/queue")
	if status != http.StatusOK {
		t.Fatalf("queue: status %d", status)
	}
	var queue struct {
		Entries []models.Entry `json:"entries"`
	}
	if err := json.Unmarshal(body, &queue); err != nil {
		t.Fatal(err)
	}
	if len(queue.Entries) != 2 {
		t.Fatalf("queue = %d entries, want 2", len(queue.Entries))
	}
	if queue.Entries[0].ID != entryID || queue.Entries[0].Book == nil {
		t.Errorf("queue head = %+v, want the book (added first)", queue.Entries[0])
	}
	// Move the book behind the game, then assert the order flipped.
	status, _ = app.post(t, "/api/library/reorder", map[string]any{
		"entry_id": entryID, "before_id": gameEntry.ID})
	if status != http.StatusOK {
		t.Errorf("reorder book: status %d, want 200", status)
	}
	status, body = app.get(t, "/api/library/queue")
	queue.Entries = nil
	json.Unmarshal(body, &queue)
	if len(queue.Entries) != 2 || queue.Entries[1].ID != entryID || queue.Entries[0].ID != gameEntry.ID {
		t.Errorf("book did not move behind the game: %+v", queue.Entries)
	}

	// Book stats and facets are served beside the game-scoped ones.
	status, body = app.get(t, "/api/library/stats?media=book")
	if status != http.StatusOK {
		t.Fatalf("book stats: status %d", status)
	}
	var bookStats models.BookStats
	if err := json.Unmarshal(body, &bookStats); err != nil {
		t.Fatal(err)
	}
	if bookStats.Total != 1 || bookStats.Backlog != 1 {
		t.Errorf("book stats = %+v", bookStats)
	}

	status, body = app.get(t, "/api/library/facets?media=book")
	if status != http.StatusOK {
		t.Fatalf("book facets: status %d", status)
	}
	var bookFacets models.BookFacets
	if err := json.Unmarshal(body, &bookFacets); err != nil {
		t.Fatal(err)
	}
	if len(bookFacets.Authors) != 1 || bookFacets.Authors[0] != "J. R. R. Tolkien" {
		t.Errorf("authors facet = %v", bookFacets.Authors)
	}
	if len(bookFacets.Languages) != 1 || bookFacets.Languages[0] != "eng" {
		t.Errorf("languages facet = %v (editions were enriched on add)", bookFacets.Languages)
	}

	// List membership works on a book entry.
	status, body = app.post(t, "/api/lists", map[string]any{"name": "Reading list"})
	if status != http.StatusCreated {
		t.Fatalf("create list: status %d, body %s", status, body)
	}
	var created struct {
		ID string `json:"id"`
	}
	json.Unmarshal(body, &created)
	if status, _ := app.post(t, "/api/lists/"+created.ID+"/items", map[string]any{"entry_id": entryID}); status != http.StatusOK {
		t.Errorf("add book to list: status %d", status)
	}
	status, body = app.get(t, "/api/lists/"+created.ID)
	if status != http.StatusOK {
		t.Fatalf("get list: status %d", status)
	}
	var listDetail struct {
		Entries []models.Entry `json:"entries"`
	}
	json.Unmarshal(body, &listDetail)
	if len(listDetail.Entries) != 1 || listDetail.Entries[0].Book == nil {
		t.Errorf("list entries = %+v, want the book", listDetail.Entries)
	}

	// Status change stamps history and pulls it out of the queue.
	status, body = app.patch(t, "/api/library/"+entryID, map[string]any{"status": "playing"})
	if status != http.StatusOK {
		t.Fatalf("update book status: status %d, body %s", status, body)
	}
	var updated struct {
		Entry models.Entry `json:"entry"`
	}
	json.Unmarshal(body, &updated)
	if updated.Entry.Status != "playing" || updated.Entry.StartedAt == nil {
		t.Errorf("updated entry = %+v", updated.Entry)
	}
	if updated.Entry.QueuePosition != nil {
		t.Error("playing book kept a queue position")
	}
	var history int
	if err := app.store.DB().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM entry_status_history WHERE entry_id = ? AND to_status = 'playing'`,
		entryID).Scan(&history); err != nil || history != 1 {
		t.Errorf("status history = %d (err %v), want 1", history, err)
	}

	// Delete removes it everywhere.
	if status := app.delete(t, "/api/library/"+entryID); status != http.StatusNoContent {
		t.Fatalf("delete book: status %d, want 204", status)
	}
	if status, _ := app.get(t, "/api/library/"+entryID); status != http.StatusNotFound {
		t.Errorf("get deleted book: status %d, want 404", status)
	}
	status, body = app.get(t, "/api/library?media=book")
	list.Entries, list.Total = nil, 0
	json.Unmarshal(body, &list)
	if list.Total != 0 {
		t.Errorf("books after delete = %d, want 0", list.Total)
	}
}

// detailGameForLibrary builds the one game the mixed-media assertions need.
func detailGameForLibrary() metadata.Game {
	release := int64(1167609600)
	g := metadata.Game{ID: 100, Name: "Mass Effect", FirstReleaseDate: &release}
	g.Extras = &metadata.GameExtras{DLCs: []metadata.RelatedGame{}, Expansions: []metadata.RelatedGame{}}
	return g
}

// TestBookAddCachedWithoutEditions covers the second enrich-on-add branch: a
// book already in the cache (say, from a search) still gets its edition list
// pulled when it is added.
func TestBookAddCachedWithoutEditions(t *testing.T) {
	app := newBooksTestApp(t)

	// A cached work with no editions — the shape a search hit leaves behind.
	if err := app.store.UpsertBook(context.Background(), metadata.Book{
		ID: "OL1168083W", Title: "The Hobbit", Authors: []string{"J. R. R. Tolkien"},
	}, ""); err != nil {
		t.Fatalf("seed: %v", err)
	}

	status, body := app.post(t, "/api/library", map[string]any{"book_id": "OL1168083W"})
	if status != http.StatusCreated {
		t.Fatalf("add cached book: status %d, body %s", status, body)
	}
	entry := decodeEntry(t, body)
	if len(entry.Book.Editions) != 2 {
		t.Errorf("editions = %d, want 2 fetched on add", len(entry.Book.Editions))
	}
}

// TestAddRequiresExactlyOneSubject pins the request contract: both-set and
// both-absent are rejected before any provider or store work happens.
func TestAddRequiresExactlyOneSubject(t *testing.T) {
	app := newBooksTestApp(t)

	start := time.Now()
	if status, _ := app.post(t, "/api/library", map[string]any{"game_id": 7, "book_id": "OL1168083W"}); status != http.StatusBadRequest {
		t.Errorf("both-set: status %d, want 400", status)
	}
	if status, _ := app.post(t, "/api/library", map[string]any{}); status != http.StatusBadRequest {
		t.Errorf("both-absent: status %d, want 400", status)
	}
	// Rejections must be immediate — nothing was fetched upstream.
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("rejections took %v; provider was consulted", elapsed)
	}
}
