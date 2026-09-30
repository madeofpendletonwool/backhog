package http

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/collinpendleton/backhog/api/booktext"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// The "copy what I've read" surface: everything the caller has actually
// read, as one Markdown document. This is the zero-setup bridge to any
// chatbot — paste it in, ask questions, and nothing past the reading
// position ever left the server to get there.
//
// The export runs on the display text (capitals and punctuation intact),
// not the folded canonical text the /text endpoint serves: an export exists
// to be read by a person or pasted into a model, and the folded text is an
// address space, not prose. Canonical offsets still govern the cut, which
// is the same single clamp every other read path answers to.

// handleBookTextExport serves the book up to the request's bound as
// Markdown: GET /api/books/{entryID}/text/export?until=.
//
// Chapter headings carry the chapter's own title (or its number when nobody
// gave it one) and a source anchor — a peek deep link that jumps a reader to
// where the section begins without moving their saved position. A token
// request that omits `until` gets its position, the same as every read path;
// a cookie request gets the whole book unless it says otherwise, so the
// in-app action passes until=position explicitly.
func (s *Server) handleBookTextExport(w http.ResponseWriter, r *http.Request) {
	userID, entryID, bookID, ok := s.bookEntry(w, r)
	if !ok {
		return
	}
	if s.epubs == nil {
		fail(w, errorf(http.StatusServiceUnavailable, "canonical text storage unavailable"))
		return
	}
	et, ok := s.ensureBookText(w, r)
	if !ok {
		return
	}

	bound, ok := s.resolveReadBound(w, r, userID, entryID, bookID, et.CharCount)
	if !ok {
		return
	}

	chapters, err := s.store.ListEpubChapters(r.Context(), et.ID)
	if err != nil {
		fail(w, err)
		return
	}
	index, err := s.epubs.LoadIndex(r.Context(), et)
	if err != nil {
		fail(w, err)
		return
	}
	// One read of the display companion for the whole export: the reader
	// slices it per document as it scrolls, but an export wants all of it
	// at once and the file is the largest thing in the arena.
	display, err := os.ReadFile(s.epubs.DisplayPath(et.ID))
	if err != nil {
		fail(w, err)
		return
	}

	title := "Untitled"
	if book, err := s.store.GetBook(r.Context(), bookID); err == nil && book.Title != "" {
		title = book.Title
	}

	var md strings.Builder
	md.WriteString("# " + title + "\n\n")
	md.WriteString("> Exported from Backhog — " + exportScope(bound) + ".\n\n")

	number := 0
	current := ""
	wroteAny := false
	for i := range index.Documents {
		doc := index.Documents[i]
		ch := chapterForOffset(chapters, doc.CharStart)
		if ch == nil {
			continue
		}
		if doc.CharStart >= bound.offset {
			break // blocks ascend with the spine; nothing later is readable
		}
		if ch.ID != current {
			current = ch.ID
			if ch.CharEnd > ch.CharStart {
				number++
			}
			heading := ch.Title
			if heading == "" {
				heading = "Section " + strconv.Itoa(number)
			}
			md.WriteString("## " + strconv.Itoa(number) + ". " + heading + "\n\n")
			md.WriteString("[Source: jump to this spot](" + deepLink(entryID, ch.CharStart) + ")\n\n")
		}
		if doc.DisplayStart < 0 || doc.DisplayEnd < doc.DisplayStart || doc.DisplayEnd > len(display) {
			continue
		}
		blocks := strings.Split(string(display[doc.DisplayStart:doc.DisplayEnd]), "\n")
		for bi, block := range blocks {
			start := doc.CharEnd
			if bi < len(doc.Blocks) {
				start = doc.Blocks[bi]
			}
			if start >= bound.offset {
				break
			}
			end := doc.CharEnd
			if bi+1 < len(doc.Blocks) {
				end = doc.Blocks[bi+1]
			}
			if end > bound.offset {
				// The paragraph the reading stops inside: keep the prefix
				// the position reached, drop the rest of the sentence.
				if _, cut, ok := booktext.SpanInDisplay(block, 0, bound.offset-start); ok {
					block = block[:cut]
				} else {
					break
				}
			}
			if strings.TrimSpace(block) == "" {
				continue
			}
			md.WriteString(block + "\n\n")
			wroteAny = true
		}
	}
	if !wroteAny {
		md.WriteString("_Nothing read yet._\n")
	}

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(md.String()))
}

// exportScope names what an export contains, in the words a person would
// use: where they are, or the whole book.
func exportScope(b readBound) string {
	if b.until == untilNone {
		return "the whole book"
	}
	if b.view.Chapter != nil && b.view.Chapter.Title != "" {
		return "read up to chapter " + strconv.Itoa(b.view.Chapter.Number) +
			" (" + strconv.FormatFloat(b.view.Percent, 'f', 1, 64) + "%)"
	}
	return "read up to " + strconv.FormatFloat(b.view.Percent, 'f', 1, 64) + "%"
}

// chapterForOffset finds the chapter owning a canonical offset — the export's
// document-to-chapter walk. chapterAt answers the *view*; this keeps the row
// so the walk can tell one chapter from the next by identity.
func chapterForOffset(chapters []models.EpubChapter, offset int) *models.EpubChapter {
	var last *models.EpubChapter
	for i := range chapters {
		if offset >= chapters[i].CharStart && offset < chapters[i].CharEnd {
			return &chapters[i]
		}
		if chapters[i].CharEnd > chapters[i].CharStart {
			last = &chapters[i]
		}
	}
	if last != nil && offset >= last.CharEnd {
		return last
	}
	return nil
}
