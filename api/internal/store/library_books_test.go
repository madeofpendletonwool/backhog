package store

import (
	"context"
	"testing"

	"github.com/collinpendleton/backhog/api/internal/metadata"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// seedBook caches a work (and optionally one edition) and returns its id.
func seedBook(t *testing.T, s *Store, id, title string, authors []string, year *int, subjects []string, edition *metadata.BookEdition) {
	t.Helper()
	b := metadata.Book{ID: id, Title: title, Authors: authors, FirstPublishYear: year, Subjects: subjects}
	if err := s.UpsertBook(context.Background(), b, ""); err != nil {
		t.Fatalf("seed book %s: %v", id, err)
	}
	if edition != nil {
		if err := s.UpsertEditions(context.Background(), id, []metadata.BookEdition{*edition}); err != nil {
			t.Fatalf("seed edition for %s: %v", id, err)
		}
	}
}

func year(v int) *int { return &v }
func pages(v int) *int { return &v }

// titlesOf flattens entries to their book titles (or game names), for order
// assertions.
func bookTitles(entries []models.Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Book != nil {
			out = append(out, e.Book.Title)
		} else {
			out = append(out, "(game)")
		}
	}
	return out
}

// TestBookLibraryRoundTrip covers the core seam this task closes: a book can
// be added, read back through the shared entry projection, sorted, queued,
// listed, updated and deleted — all through the same endpoints a game uses.
func TestBookLibraryRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := newTestUser(t, s, "reader@example.com", "reader")

	seedBook(t, s, "OL1W", "Dune", []string{"Frank Herbert"}, year(1965), []string{"Science Fiction"},
		&metadata.BookEdition{ID: "OL1M", BookID: "OL1W", PageCount: pages(412), Language: "eng", PublishedYear: year(1965)})
	seedBook(t, s, "OL2W", "The Hobbit", []string{"J. R. R. Tolkien"}, year(1937), []string{"Fantasy"},
		&metadata.BookEdition{ID: "OL2M", BookID: "OL2W", PageCount: pages(366), Language: "eng", PublishedYear: year(1997)})
	seedBook(t, s, "OL3W", "A Wizard of Earthsea", []string{"Ursula K. Le Guin"}, year(1968), []string{"Fantasy"},
		&metadata.BookEdition{ID: "OL3M", BookID: "OL3W", PageCount: pages(183), Language: "eng", PublishedYear: year(1968)})

	hobbit, err := s.AddBookEntry(ctx, userID, "OL2W", nil, models.StatusBacklog)
	if err != nil {
		t.Fatalf("add book: %v", err)
	}
	if hobbit.MediaType != models.MediaBook || hobbit.Book == nil || hobbit.Book.Title != "The Hobbit" {
		t.Fatalf("added entry = %+v", hobbit)
	}
	if hobbit.Game != nil {
		t.Error("game subject serialised onto a book entry")
	}
	if hobbit.QueuePosition == nil {
		t.Error("backlog book did not land in the queue")
	}
	if hobbit.StartedAt != nil {
		t.Error("backlog book stamped started_at")
	}

	// Duplicate add → conflict.
	if _, err := s.AddBookEntry(ctx, userID, "OL2W", nil, models.StatusBacklog); err != ErrConflict {
		t.Errorf("duplicate add err = %v, want ErrConflict", err)
	}

	// Edition anchoring: the right edition sticks, a foreign one is refused.
	ed := "OL1M"
	withEd, err := s.AddBookEntry(ctx, userID, "OL1W", &ed, models.StatusBacklog)
	if err != nil {
		t.Fatalf("add book with edition: %v", err)
	}
	var stored string
	if err := s.db.QueryRowContext(ctx,
		`SELECT edition_id FROM library_entries WHERE id = ?`, withEd.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != ed {
		t.Errorf("edition_id = %q, want %q", stored, ed)
	}
	foreign := "OL3M"
	if _, err := s.AddBookEntry(ctx, userID, "OL1W", &foreign, models.StatusBacklog); err != ErrEditionMismatch {
		t.Errorf("foreign edition err = %v, want ErrEditionMismatch", err)
	}

	if _, err := s.AddBookEntry(ctx, userID, "OL3W", nil, models.StatusBacklog); err != nil {
		t.Fatal(err)
	}

	// A game alongside, for the mixed page.
	applyCluster(t, s)
	if _, err := s.AddEntry(ctx, userID, 100, models.StatusBacklog, nil); err != nil {
		t.Fatal(err)
	}

	// Read back: media=book returns only books, hydrated.
	books, err := s.ListEntries(ctx, userID, LibraryFilter{MediaType: models.MediaBook})
	if err != nil {
		t.Fatalf("list books: %v", err)
	}
	if len(books) != 3 {
		t.Fatalf("media=book returned %d entries, want 3", len(books))
	}
	for _, e := range books {
		if e.Book == nil || e.Game != nil {
			t.Errorf("book entry hydration wrong: %+v", e)
		}
	}

	// Mixed page: both kinds, one list.
	mixed, err := s.ListEntries(ctx, userID, LibraryFilter{})
	if err != nil {
		t.Fatalf("list mixed: %v", err)
	}
	if len(mixed) != 4 {
		t.Fatalf("mixed list returned %d entries, want 4", len(mixed))
	}
	kinds := map[string]int{}
	for _, e := range mixed {
		kinds[e.MediaType]++
	}
	if kinds[models.MediaGame] != 1 || kinds[models.MediaBook] != 3 {
		t.Errorf("mixed kinds = %v", kinds)
	}

	// Mixed sorted by title stays one page: alphabetically across subjects.
	titleSorted, err := s.ListEntries(ctx, userID, LibraryFilter{Sort: "title"})
	if err != nil {
		t.Fatalf("sort title: %v", err)
	}
	want := []string{"A Wizard of Earthsea", "Dune", "(game)", "The Hobbit"} // the game is Mass Effect
	got := bookTitles(titleSorted)
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("title sort[%d] = %v, want %v (full: %v)", i, got[i], want[i], got)
			break
		}
	}

	// Book sorts.
	sortCases := []struct {
		sort string
		want []string
	}{
		{"title", []string{"A Wizard of Earthsea", "Dune", "The Hobbit"}},
		{"author", []string{"Dune", "The Hobbit", "A Wizard of Earthsea"}}, // Frank, J. R. R., Ursula
		{"published", []string{"A Wizard of Earthsea", "Dune", "The Hobbit"}},
		{"pages", []string{"A Wizard of Earthsea", "The Hobbit", "Dune"}},
	}
	for _, tc := range sortCases {
		sorted, err := s.ListEntries(ctx, userID, LibraryFilter{MediaType: models.MediaBook, Sort: tc.sort})
		if err != nil {
			t.Fatalf("sort %s: %v", tc.sort, err)
		}
		got := bookTitles(sorted)
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Errorf("sort %s[%d] = %q, want %q (full: %v)", tc.sort, i, got[i], tc.want[i], got)
				break
			}
		}
	}

	// Search matches titles and authors alike.
	hits, err := s.ListEntries(ctx, userID, LibraryFilter{MediaType: models.MediaBook, Query: "tolkien"})
	if err != nil || len(hits) != 1 || hits[0].Book.Title != "The Hobbit" {
		t.Errorf("author search = %v (err %v)", bookTitles(hits), err)
	}
	hits, err = s.ListEntries(ctx, userID, LibraryFilter{Query: "hobbit"})
	if err != nil || len(hits) != 1 {
		t.Errorf("title search = %v (err %v)", bookTitles(hits), err)
	}

	// Facet filters.
	if hits, err = s.ListEntries(ctx, userID, LibraryFilter{MediaType: models.MediaBook, Author: "Ursula K. Le Guin"}); err != nil || len(hits) != 1 || hits[0].Book.Title != "A Wizard of Earthsea" {
		t.Errorf("author filter = %v (err %v)", bookTitles(hits), err)
	}
	if hits, err = s.ListEntries(ctx, userID, LibraryFilter{MediaType: models.MediaBook, Subject: "Fantasy"}); err != nil || len(hits) != 2 {
		t.Errorf("subject filter = %v (err %v)", bookTitles(hits), err)
	}
	if hits, err = s.ListEntries(ctx, userID, LibraryFilter{MediaType: models.MediaBook, Language: "eng"}); err != nil || len(hits) != 3 {
		t.Errorf("language filter = %v (err %v)", bookTitles(hits), err)
	}

	// Text search and book filters count consistently.
	total, err := s.CountEntries(ctx, userID, LibraryFilter{MediaType: models.MediaBook, Query: "tolkien"})
	if err != nil || total != 1 {
		t.Errorf("count author search = %d (err %v)", total, err)
	}

	// The queue carries books alongside games.
	queue, err := s.Queue(ctx, userID)
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	if len(queue) != 4 {
		t.Fatalf("queue has %d entries, want 4", len(queue))
	}
	if err := s.MoveEntry(ctx, userID, hobbit.ID, queue[3].ID, ""); err != nil {
		t.Fatalf("reorder around a book: %v", err)
	}
	queue, err = s.Queue(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if queue[3].ID != hobbit.ID {
		t.Errorf("moved book is at %d, want the end", 3)
	}

	// Status changes stamp timestamps, record history, and move the queue.
	playing := models.StatusPlaying
	updated, _, err := s.UpdateEntry(ctx, userID, hobbit.ID, EntryUpdate{Status: &playing})
	if err != nil {
		t.Fatalf("update book status: %v", err)
	}
	if updated.Status != models.StatusPlaying || updated.StartedAt == nil || updated.QueuePosition != nil {
		t.Errorf("updated book = %+v", updated)
	}
	var history int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM entry_status_history WHERE entry_id = ? AND to_status = 'playing'`,
		hobbit.ID).Scan(&history); err != nil || history != 1 {
		t.Errorf("status history rows = %d (err %v), want 1", history, err)
	}

	// Facets and stats see only the books.
	facets, err := s.BookFacets(ctx, userID)
	if err != nil {
		t.Fatalf("book facets: %v", err)
	}
	if len(facets.Authors) != 3 || facets.Authors[0] != "Frank Herbert" {
		t.Errorf("authors facet = %v", facets.Authors)
	}
	if len(facets.Subjects) != 2 || facets.Subjects[0] != "Fantasy" {
		t.Errorf("subjects facet = %v", facets.Subjects)
	}
	if len(facets.Languages) != 1 || facets.Languages[0] != "eng" {
		t.Errorf("languages facet = %v", facets.Languages)
	}
	if len(facets.Statuses) != 2 { // backlog + playing
		t.Errorf("statuses facet = %v", facets.Statuses)
	}

	stats, err := s.BookStats(ctx, userID)
	if err != nil {
		t.Fatalf("book stats: %v", err)
	}
	if stats.Total != 3 || stats.Backlog != 2 || stats.Reading != 1 || stats.Read != 0 {
		t.Errorf("book stats = %+v", stats)
	}
	if stats.Completion != 0 {
		t.Errorf("completion = %v, want 0", stats.Completion)
	}

	// Game stats must not have absorbed the books.
	gameStats, err := s.Stats(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if gameStats.Total != 1 {
		t.Errorf("game stats total = %d, want 1 (books leaked in)", gameStats.Total)
	}

	// Delete removes it; a second delete is not found.
	if err := s.DeleteEntry(ctx, userID, hobbit.ID); err != nil {
		t.Fatalf("delete book entry: %v", err)
	}
	if err := s.DeleteEntry(ctx, userID, hobbit.ID); err != ErrNotFound {
		t.Errorf("second delete err = %v, want ErrNotFound", err)
	}
}
