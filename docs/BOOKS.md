# Backhog Books

How the books arena is built, and what you have to preserve when you add
to it.

Backhog Games tracks progress. Backhog Books **translates position**: the
same book consumed as paper, audio and EPUB is one book at three
coordinates, and the arena's whole job is converting between them. Stop
listening in the car at 4:12:33, open the paperback on the right
sentence. Scan a page with a phone, get told where that page sits in the
audiobook. No external database of page numbers — the map is built from
the user's own printing, and it improves every time they use it.

The files are the user's: EPUB and audiobook live on their NAS, mounted
read-only. Backhog inventories and points; it never uploads, copies or
writes them.

**If you read nothing else, read [Invariants](#invariants).**

---

## The idea

> One number is the truth. Every other "where am I" is derived from it.

The canonical character offset — a byte offset into the book's normalized
EPUB text — is the only stored position. The audiobook second, the
printed page and the percentage are all *views*, computed on read from
maps of sampled anchors. A reader that stops at offset 412,900 and a car
ride that resumes from "the same place" cannot drift apart, because
there is nothing to drift: one number, three derivations.

The maps that make the derivations possible are built by the two
pipelines this document covers: forced alignment (audio ↔ text) and page
anchoring (paper ↔ text). Both produce anchors into the *same* canonical
text, which is why one translator serves both.

---

## The canonical text

Every EPUB attached to a book gets parsed exactly once into a canonical
text: the book's prose as one normalized UTF-8 string, in spine order,
with every position in the arena measured as a **byte offset** into it
(Go string indexing — bytes, not runes, not pages, not percentages).

The pipeline (`api/internal/books/`):

1. **Extract** (`epub/`) walks the spine documents in reading order and
   emits the book as a sequence of blocks (paragraph-ish units), carrying
   each block's source href. The NCX/nav TOC supplies chapter titles and
   depth.
2. **Normalize** (`api/booktext/normalize.go`) folds every block through
   the pinned rules (below). The result contains only letters, digits
   and single spaces.
3. **Index** — `epub_texts` gets one row per parsed EPUB (`char_count`,
   `word_count`, `normalized_sha256`, `parser_version`); `epub_chapters`
   records each spine document's `[char_start, char_end)` range, which
   partitions `[0, char_count)` exactly — contiguous, no gaps, no
   overlaps, asserted by a property test. The text itself is a plain
   file at `{EPUB_TEXT_DIR}/{id}.txt` with a
   `{id}.blocks.json` sidecar for the char-offset ↔ (href, block) index
   — novels are multi-megabyte strings and SQLite would drag them
   through the WAL on every ranged read.

### The pinned normalizer

`booktext.Normalize` is applied to **everything before matching or
offset math**: the EPUB text, Whisper transcripts, OCR output, typed
passages. The rules, in order:

1. Unicode NFKC (folds ligatures ﬁ→fi, non-breaking spaces, fullwidth
   Latin…)
2. Lowercase
3. Quotes (curly, guillemets, ASCII `"`) are **dropped** — they mark
   speech, not content, and OCR renders them inconsistently
4. Dashes (every Unicode dash) become a **space** — speech renders an
   em-dash as a word boundary, so a space, never a join
5. Apostrophes are **dropped**, not spaced — transcripts write "dont"
   for "don't", so the words must join
6. Everything that is not a letter or digit is dropped
7. Whitespace runs collapse to single spaces; ends trimmed

It is pure, deterministic and idempotent, and it sits in `api/booktext`
— *outside* `internal/` — because the alignment worker is a separate Go
module that imports the very same function rather than a copy. Two
normalizers that drifted apart would silently rot every stored offset
in the arena.

---

## The data model

The arena rides the games spine (`library_entries`), it does not build a
parallel one. `media_type` is `'game' | 'book'`; the subject columns are
nullable `game_id` / `book_id` with a CHECK that exactly one is set.
Queue, lists, projects, smart lists, status history, achievements and
sessions all key on `library_entries.id` and were already
media-agnostic; books got the same statuses, the same drag queue and
the same achievement ledger (tagged `domain` — see
[ACHIEVEMENTS.md](ACHIEVEMENTS.md)).

The book-specific hierarchy, one table per concept:

| Table | Is a | Keyed by | Notes |
|---|---|---|---|
| `books` | **Work** | Open Library work key (`OL12345W`) | Shared metadata cache, like `games`. Authors/subjects stay JSON until faceting needs them |
| `book_editions` | **Edition / Printing** | OL edition key (`OL12345M`) | ISBN10/13, publisher, page count, binding. Page numbers belong *here*, not to the work |
| `library_entries` | your copy of the work | + `media_type`, `book_id`, nullable `edition_id` | The spine. `edition_id` is the printing the entry is anchored to, recorded at add time |
| `physical_copies` | the lump of paper | `(user, entry, edition)` UNIQUE | The thing page anchors attach to — a second printing is a second row with its own map |
| `media_files` | EPUB & audiobook files | `(root, path)` UNIQUE | The NAS inventory — pointed-at, never uploaded |
| `media_sidecars` | parsed `.opf` metadata | `(root, path)` UNIQUE | Replaced per root each scan; the matcher's best evidence |
| `epub_texts` / `epub_chapters` | parsed canonical text | per media file | See above |
| `book_progress` | position | entry PK | One row per entry: `char_offset` is the truth |
| `reading_sessions` | consumption log | per user, per entry | `mode` ∈ read/listen; `chars_advanced` |
| `alignment_jobs` / `alignments` / `alignment_anchors` | audio↔text map | per entry | See [alignment](#the-alignment-pipeline) |
| `page_anchors` | paper↔text map | `(physical_copy_id, printed_page)` PK | See [page anchors](#the-page-anchor-map) |

Two shapes worth internalising:

- **EPUB and audiobook are siblings of Edition, not properties of it.**
  Files are `media_files` rows attached to the *work* (through the
  entry's attach flow, in explicit track order for audio); an edition is
  metadata about a printing. You can own the paperback of printing A,
  the EPUB, and the audiobook, all of one work — three ways to consume,
  one position.
- **`book_progress` stores one number and admits one honest exception.**
  `char_offset` + `char_offset_source` (`read` / `listen` / `scan` /
  `manual`) is the truth. Before an alignment exists there is no map
  from a listening position to a char offset, so those writes land in
  track-relative `raw_audio_seconds` / `raw_audio_file_id` and the API
  reports `derived: false` rather than fabricating an offset.

### The NAS inventory

`MEDIA_DIR` names colon-separated read-only roots; the scanner
(`api/internal/media/`) walks them and upserts `media_files` by
`(root, path)` with cheap `(size, mtime)` change detection. Rules:

- **Rows are never deleted.** A path that disappears is flagged
  `missing_at`, so an unmounted NAS doesn't destroy the book
  associations; the next scan that sees it clears the flag.
- **Files that are not inventoried are counted and shown, not hidden**:
  `media_skipped` records each with a reason, so a user whose library is
  half Audible sees *why* those files aren't there. The reason
  distinguishes four different statements: `drm_epub` and
  `unsupported_extension` (which covers `.aax`/`.aaxc` and genuinely
  unrecognised files), `format_unhandled` for Kindle formats this tool
  recognises and chose not to parse, and `sidecar_metadata` for a `.opf`,
  which is not a book at all.
- **`.opf` sidecars are mined, not skipped**: `media_sidecars` holds the
  parsed metadata block of every `.opf` found next to the books — title,
  author, series, ISBN, work key. Rows are replaced per root on each scan
  like `media_skipped`, because a sidecar describes what is on disk right
  now and carries no user state. The matcher reads them all in one query
  and never touches the filesystem itself: the candidates endpoint is
  polled every 1.5s while a scan runs.
- **`meta_version` gates the fast path**: `(size, mtime)` alone would mean
  a better metadata extractor only ever reached files that happened to
  change afterwards. The scanner's constant joins the comparison, so
  bumping it re-reads every file's metadata exactly once and then goes
  quiet — `books.ParserVersion`'s trick, applied to the inventory.
- `media_ignores` is the per-user "stop asking me about this file".
- The attach matcher (`match.go`) groups audio directories into ordered
  candidates and proposes (book, confidence) suggestions. Evidence is a
  ladder, expressed as ordering rather than arithmetic — the first source
  that yields a title wins: an OPF metadata block (a `.opf` beside the
  files, or an epub's own package document), then ID3/MP4/Vorbis tags,
  then directory layout, then the bare filename. Above 0.72 the UI offers
  them for bulk confirmation; nothing auto-attaches.
- **An identifier is an identity, not a resemblance.** When that metadata
  carries an ISBN or an Open Library work key, the matcher resolves it
  through `GetByISBN`/`GetByWorkKey` and suggests the result outright
  instead of scoring a title against it — a printing whose subtitle
  differs from the work's would otherwise be marked down for being
  correct. These lookups share the memo cache, the inline budget and the
  background queue with title searches.

Audio playback (`api/internal/books/audio/`) treats N files as one
tape: everything outside the package works in **global seconds**, track
boundaries are resolved here and nowhere else. Tracks whose duration
the container headers don't yield make the timeline *degraded* —
surfaced, not papered over. Streaming is `http.ServeContent` over the
read-only mount (proper Range/If-Range handling), with path containment
re-checked on every request so a hand-edited row can't turn the endpoint
into an arbitrary-file read.

### Open Library

Book metadata comes from [Open Library](https://openlibrary.org) — no
API key, no OAuth, no credentials of any kind. The client
(`api/internal/metadata/books.go`) identifies itself with a descriptive
`User-Agent` (their request: throttle offenders specifically instead of
blocking the app) and rate-limits itself. Works and editions are cached
in `books` / `book_editions` exactly like IGDB games — shared across
users, so two people adding *Dune* make one row and two entries.

---

## The alignment pipeline

The expensive piece: mapping an audiobook onto the canonical text so
"where in the audio" becomes "where in the text". Whisper, ffmpeg and a
multi-hundred-MB model neither fit nor belong in the small CGO-free API
image, so alignment runs in a separate **optional worker container**
(`align/`, behind the `align` compose profile). A deployment that never
sets `ALIGN_WORKER_TOKEN` keeps a fully working arena — the queue
simply stays empty.

End to end, a job's life:

1. **Enqueue** — the user requests alignment on an entry with an
   attached EPUB and audiobook. `alignment_jobs` gets a row pinned to
   the exact `audio_timeline_hash` (ordered file+duration sequence), so
   re-attaching or re-ordering tracks later is detectable against the
   alignment produced. A partial unique index enforces **at most one
   active job per entry** — hammering the button cannot stack
   duplicates.
2. **Claim** — the worker polls the `/internal` claim API
   (token-authenticated, disabled entirely when the token is empty) and
   moves the job to `claimed`, then `transcribing`. Heartbeats mark it
   alive; a dead heartbeat is reclaimed (re-queued, or failed once
   attempts run out) the next time anyone claims. A worker killed
   mid-book is picked back up, not stranded.
3. **Transcribe** — ffmpeg decodes the audio in 600-second chunks (5s
   overlap as context; peak disk and memory stay flat regardless of
   book length) and whisper.cpp transcribes each chunk. Segments stream
   back to the API in batches, already on the **global timeline**.
4. **Align** (`align/internal/align/`) — two passes, because a single
   global alignment of half a million characters against a hundred
   thousand transcript words is both slow and fragile:
   - **Coarse** — each minute of narration is located to within a few
     dozen words by a 5-word shingle index over the canonical text,
     under the one constraint an audiobook really obeys: it is read
     front to back, so located positions must never go backwards.
   - **Fine** — a banded word-level alignment inside each located
     region reads off one anchor per transcript segment.
   Both sides speak `booktext.Normalize` plus a matching fold that
   reconciles numerals ("42" vs "forty-two") and abbreviations ("Dr."
   vs "Doctor") — the two systematic disagreements between print and
   speech.
5. **Publish** — anchors stream back in batches and land in
   `alignment_anchors` as `(char_offset, audio_seconds, confidence)` on
   the global timeline. `transcript_segments` keeps the raw Whisper
   output for debugging a bad alignment (and a future search-the-audio).

### The confidence model

A finished alignment reports two numbers:

- **Coverage** — the fraction of the canonical text spanned by
  confident anchors (gaps under 8,000 chars still count as covered;
  beyond that the text was bracketed, not aligned).
- **Mean confidence** — the aligner's own per-anchor belief, averaged.

Against the shipped thresholds (`ALIGN_MIN_COVERAGE` 0.80,
`ALIGN_MIN_CONFIDENCE` 0.60, chosen from the synthetic measurements in
`align/cmd/alignbench`), the outcome is:

- **`ready`** — published; the map is trustworthy.
- **`low_confidence`** — *a usable alignment whose anchors should be
  treated skeptically, not a failure.* An unabridged reading covers
  essentially the whole book at any usable transcript quality; a
  55%-abridged reading covers 0.55; the wrong book covers nothing.
  Below the thresholds the anchors are still stored — a partial map of
  a book is genuinely useful — but the state says so, the UI shows it,
  and the user can delete and retry. What is *expected* to fail small:
  publisher intros/outros that appear in no EPUB (unmatched head and
  tail, deliberately not bridged), illustrations, footnotes, the
  occasional skipped line — interpolation between anchors covers those.

---

## The page-anchor map

Page numbers are a property of a **printing**, not a work: "page 120"
means nothing until you know which edition's pages. So the page bridge
attaches to `physical_copies` — a printing the user actually holds — and
one printing's map can never bleed into another's.

The flow, from a phone:

1. **Scan** — the page scanner runs OCR **in the browser** (Tesseract.js
   WASM + English model, vendored onto our own origin at build time —
   no CDN, no page load tells anyone which book is being read; works on
   a LAN with no internet). Camera, photo, or a typed sentence — the
   API accepts any raw text.
2. **Find** (`api/internal/books/passage/`) — the query is normalized
   with the same pinned `booktext.Normalize`, then a 4-word shingle
   index over the canonical text votes on where the passage starts (a
   shingle that survives OCR noise anywhere in the query pins the whole
   window, so garbage only abstains, never misleads), and the top
   candidates are confirmed by edit-distance ratio over the full query.
   Below ten words it refuses rather than guesses. A passage that
   genuinely recurs returns its alternatives and the UI asks which
   occurrence the reader is holding.
3. **Record** — the offset becomes a `page_anchors` row for the user's
   physical copy: `(printed_page, char_offset)` with source
   `ocr`/`manual` and the match confidence. The composite PK makes
   re-scanning a page an **overwrite** — a bad anchor is corrected by
   the next scan, so the map self-heals instead of accumulating
   contradictions.

The map is **seeded before the first scan**: the printing's own page
count (from the edition metadata) stretches page 1 across the start of
the text and the last page across the end, at a deliberately low
confidence (0.3 — a real catalogue number, but nobody has looked at
the paper). That is enough to answer "roughly where am I in the
paperback" the day a copy is registered, and every real scan lands
inside it and tightens the segments it falls between; a seed that
would contradict a real scan is dropped, because the catalogue being
wrong and the reader being right is exactly the case.

### Why the error bar is always shown

Between two scanned pages the translator interpolates linearly, and
linear interpolation is a lie that is only locally true: front matter,
illustrations, plates and chapter-break whitespace all stretch the page
axis against the text. So every page answer carries a margin — "page
214 ± 3" — derived from how far the query sat from the nearest anchor
and how much that anchor was trusted (a segment bounded by two
half-trusted anchors is twice as loose as one bounded by two certain
ones). The UI also always shows the **passage text** the number came
from, so a reader self-corrects instantly when the number is off. A
copy with a single scanned page knows where one page is and nothing
about how fast pages go by: it reports no bar rather than a made-up
one, and the UI says "scan another page".

---

## Position translation

`api/internal/books/position/` is the seam everything feeds:
`Translator.Load` pulls a entry's audio anchors and page anchors and
answers in any direction — char↔audio seconds, char↔page. All pure
functions over sorted anchor maps; no database, no EPUB, no audiobook
needed to test it.

The mechanics that matter:

- **Exact anchor hit** — that anchor's own value and confidence, no
  bar.
- **Between anchors** — linear interpolation; the segment's confidence
  is the *lower* of its two ends (a chain is as trustworthy as its
  weaker link).
- **Past the ends, the two maps part company.** The audio map
  **clamps**: extrapolating a tape past its ends invents seconds that
  don't exist. The page map **extrapolates** along the nearest pair's
  slope: a reader thirty pages past their last scan is still somewhere,
  and that slope is the only estimate of where that exists. Either way
  confidence is damped by half.
- **Monotonicity is enforced at construction.** Anchors that don't
  strictly advance both axes are dropped — one noisy page cannot poison
  its neighbours' interpolations.
- **Every answer carries its honesty**: value, confidence, distance
  from the nearest anchor, and the margin in the units a person reads.

---

## Invariants

Break these and the arena degrades in ways that are easy to miss in
review.

**1. `books.Normalize` is pinned and versioned.** Every stored offset
in the arena — reader position, alignment anchor, OCR page anchor —
assumes *this exact function* (`api/booktext/normalize.go`), on both
the EPUB side and the transcript side; the worker imports it, never
copies it. Changing the rules (or the EPUB block extraction) requires
bumping `books.ParserVersion` so `epub_texts` rows are re-parsed and
every derived offset rebuilt together. A silent rule change rots every
offset ever stored.

**2. Anchors are always monotonic.** Reading is monotonic and an
audiobook only moves forwards. `newAnchorMap` drops any anchor that
does not strictly advance both axes, and the alignment queue's partial
unique index allows at most one active job per entry. Never store a
"corrected" anchor that contradicts its neighbours — re-scan the page
instead (the PK overwrite is the supported correction path).

**3. The media mount is read-only and never written to.** The scanner,
the attach flow, the audio streamer and the alignment worker only ever
*read* `/media/...`; the compose file mounts them `:ro` on every
service that gets them. Backhog inventories the NAS; it does not own
it. Path containment is re-checked on every served request.

**4. Supported formats are epub / mp3 / m4a / m4b / opus, and both DRM
and Kindle formats are out of scope by decision.** `.aax`, `.aaxc` and
DRM-wrapped epubs are skipped and *reported* (`media_skipped`), never
half-supported. This tool is for the DRM-free crowd; do not add "just
one container" of DRM circumvention.

`.mobi` / `.azw` / `.azw3` are a *different* refusal and are labelled as
one (`format_unhandled`). They carry no DRM here — they are simply not
parsed, because reading them means PalmDOC LZ77, HUFF/CDIC Huffman
decompression and KF8 fragment reassembly, with no pure-Go reader in
existence to build on. That is a library in its own repo, and until it
exists the honest answer is to name the format and point at the EPUB of
the same book. Do not half-implement it inside the scanner.

Every container parser in the API is hand-rolled pure Go and stays that
way: the image is distroless with `CGO_ENABLED=0`, so there is no
ffprobe to reach for. Opus duration comes from the last Ogg page's
granule position minus the encoder's pre-skip, over the fixed 48 kHz
granule clock — header reads only, like the MP3 and MP4 parsers beside
it. (Ogg Opus needs Safari 17.4 or newer on the client; that is a
browser limitation, not a reason to transcode files we promised never to
write to.)

**5. One position, and it's the char offset.** Audio seconds and pages
are derived views, never independently stored truths — the single
exception is the pre-alignment `raw_audio_*` fallback, which is flagged
`derived: false` precisely because it isn't one. Audio anchors are on
the **global** timeline; track-relative offsets exist only inside the
audio package.

**6. Page numbers belong to an edition.** Page anchors attach to
`physical_copies` (user, entry, *edition*) — never to a work, never to
an entry directly. "Page 120" of a different printing is a different
row.

**7. `epub_chapters` partitions `[0, char_count)` exactly.** Contiguous,
gapless, non-overlapping, in spine order. The reader's ranged fetches
and every "which chapter am I in" query assume it.

**8. Alignment is optional infrastructure.** No `ALIGN_WORKER_TOKEN`,
no worker container: reading, listening, tracking, scanning pages and
every achievement still work. The queue is additive. Nothing outside
`/internal` may assume an alignment exists — only the audio↔text
handoff degrades, by asking the user to say where they were.

---

## Endpoint map

| Route | What |
|---|---|
| `GET /api/books/search`, `GET /api/books/isbn/{isbn}`, `GET /api/books/{bookID}` | Open Library search / ISBN lookup / cached work |
| `GET/POST /api/media/scan`, `GET /api/media/files`, `GET /api/media/candidates`, `POST /api/media/ignore`, `DELETE /api/media/ignore/{fileID}` | NAS inventory + attach candidates |
| `GET/POST /api/books/{entryID}/files`, `DELETE …/files/{fileID}` | attach / detach EPUB & audio |
| `GET /api/books/{entryID}/text[/chapters\|/display\|/asset]` | canonical text ranged reads |
| `GET /api/books/{entryID}/audio`, `GET …/audio/{trackID}` | timeline + track bytes (Range) |
| `GET/PUT /api/books/{entryID}/position`, `GET/POST …/sessions` | the one position, and reading sessions |
| `POST/GET/DELETE /api/books/{entryID}/align` | alignment enqueue / status / delete |
| `POST /api/books/{entryID}/passage` | OCR / typed passage → offset (+ alternatives) |
| `GET/POST …/copies[…]`, `GET/POST …/copies/{copyID}/pages` | physical copies + page anchors |
| `/api/achievements/reading-season` | the per-year Reading Season rollup |

---

## Verifying a change

```bash
cd api && go test ./...
cd web && npm run typecheck && npm run build
cd align && go test ./...
docker compose build && docker compose up          # plain profile
docker compose --profile align up                  # + the worker
```

Then walk it like a stranger would: add a book by ISBN, scan the NAS,
attach the files, read a chapter, enqueue alignment, hand off between
reader and player, scan a paper page and check the page number arrives
with its error bar. `go run ./cmd/alignbench` inside `align/` measures
whether an aligner change moved the numbers the ready/low_confidence
thresholds were chosen from.
