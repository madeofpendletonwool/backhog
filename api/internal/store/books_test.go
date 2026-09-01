package store

import (
	"context"
	"testing"

	"github.com/collinpendleton/backhog/api/internal/metadata"
)

func hobbitBook() metadata.Book {
	year := 1937
	return metadata.Book{
		ID:               "OL1168083W",
		Title:            "The Hobbit",
		Authors:          []string{"J. R. R. Tolkien"},
		Description:      "In a hole in the ground there lived a hobbit.",
		CoverURL:         "https://covers.openlibrary.org/b/id/8225269-L.jpg",
		FirstPublishYear: &year,
		Subjects:         []string{"Fantasy", "Middle Earth"},
		Raw:              []byte(`{"title":"The Hobbit"}`),
	}
}

func hobbitEdition() *metadata.BookEdition {
	year := 1997
	pages := 366
	return &metadata.BookEdition{
		ID:            "OL7440402M",
		BookID:        "OL1168083W",
		ISBN10:        "0261102213",
		ISBN13:        "9780261102217",
		Publisher:     "HarperCollins",
		PublishedYear: &year,
		PageCount:     &pages,
		Binding:       "Paperback",
		Language:      "eng",
		CoverURL:      "https://covers.openlibrary.org/b/id/8225269-L.jpg",
		Raw:           []byte(`{"key":"/books/OL7440402M"}`),
	}
}

func TestUpsertBookWork(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	b := hobbitBook()
	if err := s.UpsertBook(ctx, b, "#aabbcc"); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := s.GetBook(ctx, b.ID)
	if err != nil {
		t.Fatalf("reread: %v", err)
	}
	if got.Title != b.Title {
		t.Errorf("Title = %q", got.Title)
	}
	if len(got.Authors) != 1 || got.Authors[0] != "J. R. R. Tolkien" {
		t.Errorf("Authors = %v", got.Authors)
	}
	if got.Description != b.Description {
		t.Errorf("Description = %q", got.Description)
	}
	if got.CoverURL != b.CoverURL {
		t.Errorf("CoverURL = %q", got.CoverURL)
	}
	if got.AccentHex != "#aabbcc" {
		t.Errorf("AccentHex = %q, want the sampled accent", got.AccentHex)
	}
	if got.FirstPublishYear == nil || *got.FirstPublishYear != 1937 {
		t.Errorf("FirstPublishYear = %v", got.FirstPublishYear)
	}
	if len(got.Subjects) != 2 || got.Subjects[0] != "Fantasy" {
		t.Errorf("Subjects = %v", got.Subjects)
	}
	if len(got.Editions) != 0 {
		t.Errorf("Editions = %v, want none", got.Editions)
	}

	// A lean search-shaped re-upsert must not wipe detail metadata or accent,
	// mirroring the games extras behaviour.
	lean := metadata.Book{ID: b.ID, Title: "The Hobbit", Authors: []string{"J. R. R. Tolkien"}}
	if err := s.UpsertBook(ctx, lean, ""); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	got, err = s.GetBook(ctx, b.ID)
	if err != nil {
		t.Fatalf("reread after lean upsert: %v", err)
	}
	if got.AccentHex != "#aabbcc" {
		t.Errorf("AccentHex after lean upsert = %q, want preserved", got.AccentHex)
	}
	if got.Description != b.Description {
		t.Errorf("Description after lean upsert = %q, want preserved", got.Description)
	}
	if len(got.Subjects) != 2 {
		t.Errorf("Subjects after lean upsert = %v, want preserved", got.Subjects)
	}
	if got.FirstPublishYear == nil || *got.FirstPublishYear != 1937 {
		t.Errorf("FirstPublishYear after lean upsert = %v, want preserved", got.FirstPublishYear)
	}
}

func TestUpsertBookEdition(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	b := hobbitBook()
	b.Edition = hobbitEdition()
	if err := s.UpsertBook(ctx, b, ""); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := s.GetBook(ctx, b.ID)
	if err != nil {
		t.Fatalf("reread: %v", err)
	}
	if len(got.Editions) != 1 {
		t.Fatalf("Editions = %v, want 1", got.Editions)
	}
	ed := got.Editions[0]
	if ed.ID != "OL7440402M" || ed.BookID != b.ID {
		t.Errorf("edition keys = %q / %q", ed.ID, ed.BookID)
	}
	if ed.ISBN10 != "0261102213" || ed.ISBN13 != "9780261102217" {
		t.Errorf("ISBNs = %q / %q", ed.ISBN10, ed.ISBN13)
	}
	if ed.Publisher != "HarperCollins" {
		t.Errorf("Publisher = %q", ed.Publisher)
	}
	if ed.PublishedYear == nil || *ed.PublishedYear != 1997 {
		t.Errorf("PublishedYear = %v", ed.PublishedYear)
	}
	if ed.PageCount == nil || *ed.PageCount != 366 {
		t.Errorf("PageCount = %v", ed.PageCount)
	}
	if ed.Binding != "Paperback" || ed.Language != "eng" {
		t.Errorf("Binding/Language = %q / %q", ed.Binding, ed.Language)
	}

	// A sparse re-upsert keeps the richer stored fields (page count, year).
	sparse := hobbitBook()
	sparse.Edition = &metadata.BookEdition{ID: "OL7440402M", BookID: b.ID, ISBN13: "9780261102217"}
	if err := s.UpsertBook(ctx, sparse, ""); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	got, err = s.GetBook(ctx, b.ID)
	if err != nil {
		t.Fatalf("reread after sparse upsert: %v", err)
	}
	if len(got.Editions) != 1 {
		t.Fatalf("Editions after re-upsert = %v, want still 1", got.Editions)
	}
	ed = got.Editions[0]
	if ed.PageCount == nil || *ed.PageCount != 366 {
		t.Errorf("PageCount after sparse upsert = %v, want preserved", ed.PageCount)
	}
	if ed.PublishedYear == nil || *ed.PublishedYear != 1997 {
		t.Errorf("PublishedYear after sparse upsert = %v, want preserved", ed.PublishedYear)
	}
	if ed.ISBN10 != "0261102213" {
		t.Errorf("ISBN10 after sparse upsert = %q, want preserved", ed.ISBN10)
	}
}

func TestGetBookNotFound(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.GetBook(context.Background(), "OL0W"); err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestBooksByIDs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, b := range []metadata.Book{hobbitBook(), {ID: "OL27455W", Title: "Dune"}} {
		if err := s.UpsertBook(ctx, b, ""); err != nil {
			t.Fatalf("upsert %s: %v", b.ID, err)
		}
	}
	got, err := s.BooksByIDs(ctx, []string{"OL1168083W", "OL27455W", "OL0W"})
	if err != nil {
		t.Fatalf("BooksByIDs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d books, want 2", len(got))
	}
	if got["OL27455W"].Title != "Dune" {
		t.Errorf("Dune Title = %q", got["OL27455W"].Title)
	}
}

func TestOwnedBookIDsAndCoverHelpers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s, "reader@example.com", "reader")
	other := newTestUser(t, s, "other@example.com", "other")

	b := hobbitBook()
	if err := s.UpsertBook(ctx, b, ""); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// The library add path for books is a later stage; insert entries directly.
	if _, err := s.DB().ExecContext(ctx,
		`INSERT INTO library_entries (id, user_id, media_type, book_id, status)
		 VALUES ('be1', ?, 'book', ?, 'backlog')`, user, b.ID); err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	owned, err := s.OwnedBookIDs(ctx, user, []string{b.ID, "OL0W"})
	if err != nil {
		t.Fatalf("OwnedBookIDs: %v", err)
	}
	if !owned[b.ID] || owned["OL0W"] {
		t.Errorf("owned = %v, want only %q", owned, b.ID)
	}
	// A different user owns nothing.
	owned, err = s.OwnedBookIDs(ctx, other, []string{b.ID})
	if err != nil {
		t.Fatalf("OwnedBookIDs other: %v", err)
	}
	if owned[b.ID] {
		t.Error("another user's book reported as owned")
	}

	url, err := s.CoverURLForBook(ctx, b.ID)
	if err != nil || url != b.CoverURL {
		t.Errorf("CoverURLForBook = %q, %v", url, err)
	}
	if _, err := s.CoverURLForBook(ctx, "OL0W"); err != ErrNotFound {
		t.Errorf("CoverURLForBook unknown = %v, want ErrNotFound", err)
	}

	if err := s.RecordBookCover(ctx, b.ID, "/covers/OL1168083W.jpg", "#112233"); err != nil {
		t.Fatalf("RecordBookCover: %v", err)
	}
	var localPath, accent string
	if err := s.DB().QueryRowContext(ctx,
		`SELECT cover_local_path, accent_hex FROM books WHERE id = ?`, b.ID).Scan(&localPath, &accent); err != nil {
		t.Fatalf("reread cover columns: %v", err)
	}
	if localPath != "/covers/OL1168083W.jpg" || accent != "#112233" {
		t.Errorf("cover columns = %q / %q", localPath, accent)
	}
}
