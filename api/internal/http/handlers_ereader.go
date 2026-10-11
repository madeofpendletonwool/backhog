package http

import (
	"context"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/collinpendleton/backhog/api/internal/auth"
	"github.com/collinpendleton/backhog/api/internal/books"
	"github.com/collinpendleton/backhog/api/internal/ereader"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// handleEpubDownload serves a book's original EPUB, byte for byte, to an
// e-reader (MAD-829). It is the OPDS acquisition link's target: the path
// carries /epub/ and ends in .epub because CrossPoint prefers such hrefs,
// and the trailing filename segment is decoration — the entry id alone
// says which book, and ownership is re-checked on every request.
//
// http.ServeContent does Range, If-Range and conditional GETs, so a client
// that loses Wi-Fi halfway through a 40 MB illustrated book resumes rather
// than restarts. The route is exempt from the API's 30-second timeout for
// the same reason (see timeoutExcept).
//
// Serving the file also refreshes the entry's KOReader document ids: the
// partial MD5 of exactly these bytes, and the filename hash under each
// name CrossPoint might save it as. That is best-effort — a failure is
// logged and the download goes ahead, since the ids are recomputed on the
// next download or sync lookup anyway.
func (s *Server) handleEpubDownload(w http.ResponseWriter, r *http.Request) {
	if s.epubs == nil {
		fail(w, errorf(http.StatusServiceUnavailable, "canonical text storage unavailable"))
		return
	}
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	entryID := chi.URLParam(r, "entryID")

	ef, err := s.epubs.OpenEpubFile(r.Context(), userID, entryID)
	switch {
	case errors.Is(err, store.ErrNotFound), errors.Is(err, books.ErrNoEpub):
		// Not yours, no EPUB, gone from the NAS, escapes its root: alike.
		fail(w, errNotFound)
		return
	case err != nil:
		fail(w, err)
		return
	}
	defer ef.File.Close()

	book, err := s.store.GetBook(r.Context(), ef.BookID)
	if err != nil {
		fail(w, err)
		return
	}
	author, title := ereader.FeedAuthor(book.Authors), ereader.FeedTitle(book.Title)

	if err := s.refreshKosyncDocuments(r.Context(), entryID, ef, author, title); err != nil {
		slog.WarnContext(r.Context(), "kosync document ids not refreshed",
			"entry_id", entryID, "error", err)
	}

	name := ereader.BookFilename(author, title, ereader.FormatAuthorTitle)
	w.Header().Set("Content-Type", "application/epub+zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("ETag", audioETag(ef.Info.Size(), ef.Info.ModTime()))
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, name, ef.Info.ModTime(), ef.File)
}

// refreshKosyncDocuments recomputes an entry's document ids from the file
// about to be served. Twelve 1 KiB reads and four MD5s: cheaper than
// deciding whether to skip them, and it keeps the filename hashes current
// when a book's title or authors were edited since the last download.
func (s *Server) refreshKosyncDocuments(ctx context.Context, entryID string, ef books.EpubFile, author, title string) error {
	binary, err := ereader.PartialMD5(ef.File, ef.Info.Size())
	if err != nil {
		return err
	}
	docs := []store.KosyncDocument{{Hash: binary, Kind: "binary"}}
	for _, format := range ereader.FilenameFormats {
		docs = append(docs, store.KosyncDocument{
			Hash:           ereader.FilenameMD5(ereader.BookFilename(author, title, format)),
			Kind:           "filename",
			FilenameFormat: string(format),
		})
	}
	return s.store.ReplaceKosyncDocuments(ctx, entryID, ef.Media.ID,
		ef.Info.Size(), ef.Info.ModTime().UnixNano(), docs)
}

// handleKosyncAuth is the KOReader sync protocol's login check: GET
// /users/auth answers {"authorized": "OK"} for good credentials. The
// middleware has already resolved the x-auth (or Basic) pair to a user and
// RequireDevice turned everyone else away, so reaching here is the answer.
// The rest of the protocol arrives with MAD-831.
func (s *Server) handleKosyncAuth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"authorized": "OK"})
}

// isLongTransfer reports whether a request is a whole-file download that
// may legitimately outlast the API's request timeout: the e-reader EPUB
// route, /api/opds/books/{entryID}/epub/{filename}. An ESP32 on weak Wi-Fi
// can take minutes over a large book, and the timeout would cut it off.
func isLongTransfer(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	return len(parts) == 6 && parts[0] == "api" && parts[1] == "opds" &&
		parts[2] == "books" && parts[4] == "epub"
}
