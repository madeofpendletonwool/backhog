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

The files are the user's: EPUBs, Kindle files and audiobooks live on
their NAS, mounted read-only. Backhog inventories and points; it never
uploads, copies or writes them.

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

And a book that has no text says so. Comics, manga, picture books and
scans have no canonical text to offset into — faking one is precisely the
plausible-wrong-answer failure this arena exists to prevent — so their
position is an honest page index, stored on a flagged second axis (see
[the paged position model](#the-paged-position-model)). One number is the
truth *per axis*, and exactly one book shape needs the second axis.

The maps that make the derivations possible are built by the two
pipelines this document covers: forced alignment (audio ↔ text) and page
anchoring (paper ↔ text). Both produce anchors into the *same* canonical
text, which is why one translator serves both.

---

## The canonical text

A book's **designated text file** — an EPUB, a MOBI/AZW/AZW3 through the
mobi parser, or a text-native PDF through the pdf parser — is parsed
exactly once into a canonical text: the book's prose as one normalized
UTF-8 string, in reading order, with every position in the arena measured
as a **byte offset** into it (Go string indexing — bytes, not runes, not
pages, not percentages).

*Designated*, because a book can have several text files attached and
only one of them can be the coordinate system. Owning the same title as
`X.epub` and `X.mobi` is ordinary — every Calibre export produces the
pair — but two parses of one book do not agree on where byte 161,929
falls, so `media_files.is_primary_text` names the one that counts (one
per book, enforced by a partial unique index). The rest are formats the
user owns: recorded, never parsed, never read from. Everything that
resolves "the book's text" — the reader, the page-anchor seed, the
achievement and insight sizing, the alignment lookup — goes through the
same predicate, because the failure mode of four subtly different
versions is not an error anyone sees, it is a percentage computed
against one text and an offset measured in another.

The designation only moves when someone moves it (`PUT
/books/{entry}/files/{id}/primary`), or when the current primary is
detached. Both paths migrate what was measured against the old text:
nothing at all when the two canonicalize to the same
`normalized_sha256` (the common case for a converted pair — the
coordinates are literally identical), otherwise the reader's position is
recomputed from its stored percentage, page anchors are scaled by the
length ratio, and any alignment is **deleted** rather than reinterpreted.
An alignment stretched onto a different text still answers every query,
just wrongly, and a plausible wrong answer is worse than none.

A missing primary does not fall back to a sibling. Serving the other
container's text while the NAS is unmounted would silently move the
reader's position, their percentage and their anchors onto coordinates
that mean something else; "the text is unavailable right now" is the
honest answer, and it heals on the next scan.

The pipeline (`api/internal/books/`):

1. **Extract** (`epub/`, `mobi/`, `pdf/`) walks the book in reading order —
   the EPUB spine documents, the mobi chapters (KF8 sections; MOBI6 TOC
   filepos ranges, else pagebreak sections), or the PDF's pages with their
   reading-order clustering — and emits the book as a
   sequence of blocks (paragraph-ish units), carrying each block's
   source position. The NCX/nav/outline TOC supplies chapter titles and
   depth; the parsers share block-extraction conventions so the
   rules cannot drift between formats.
2. **Normalize** (`api/booktext/normalize.go`) folds every block through
   the pinned rules (below). The result contains only letters, digits
   and single spaces.
3. **Index** — `epub_texts` gets one row per parsed text
   (`char_count`, `word_count`, `normalized_sha256`,
   `parser_version`); `epub_chapters` records each reading-order
   document's `[char_start, char_end)` range, which partitions
   `[0, char_count)` exactly — contiguous, no gaps, no overlaps,
   asserted by a property test. The text itself is a plain file at
   `{EPUB_TEXT_DIR}/{id}.txt` with a `{id}.blocks.json` sidecar for the
   char-offset ↔ (href, block) index — novels are multi-megabyte strings
   and SQLite would drag them through the WAL on every ranged read.

   The tables are format-agnostic in practice: they are keyed by
   `media_file_id`, and a MOBI-sourced text lands in them exactly like
   an EPUB-sourced one. No `format` column exists because no code path
   needs it — the few branches that care (which parser to run, whether
   the asset endpoint applies) dispatch on the media file's own
   extension.

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

### The PDF parser

`api/internal/books/pdf/` is the third container parser, built to the same
spine contract as `epub/` and `mobi/`: `Parse(io.ReaderAt, size)` returns
an `*epub.Document`, one Doc per page (`pdf:page:N`), blocks in reading
order, outline titles and depth, per-page image inventories, heading
evidence from display type. It is pure parsing, and it is wired into the
arena end to end: the scanner inventories `.pdf` in the text-side kind
(reading the Info dictionary and XMP packet for matcher evidence, refusing
`/Encrypt` files with `drm_pdf`), the ingester routes `.pdf` through this
parser in `parseBookFile`, and a text-native PDF lands in
`epub_texts`/`epub_chapters` exactly like any other container.

Two things live in the parser that must never move into the shared
normalizer:

- **Dehyphenation.** A line-end `hyphen-\nated` split is rejoined into one
  word — across lines *and across page breaks* — before `Normalize` runs,
  because its dash rule would otherwise freeze the split into the canonical
  text (`hyphen ated`, two words) and every stored offset after it.
- **Running-head stripping.** A line recurring at the same page edge across
  the book (masked so folio numbers count as one shape) is apparatus, not
  prose, and is dropped before blocking.

Reading order is clustered from geometry, not draw order: glyphs group
into words and baselines, a baseline splits at column-scale gaps, a page
with a clear un-crossed gutter reads its left column before its right,
and lines merge into paragraph blocks on spacing, indents and type-size
changes.

The quality gate is a *classification*, not a text. A PDF whose fonts
carry broken or missing ToUnicode maps extracts as plausible-looking
garbage, and a canonical text built from it would silently poison search,
alignment and the knowledge layer. So extraction is measured before it is
trusted — replacement-char and control-rune ratios first, dictionary
coverage only as a corroborating witness (so a clean-extracting book in a
language the word list doesn't cover can never be refused) — and the
outcome is one of `text-native`, `image-native`, `drm`, `corrupt`, surfaced
to the caller because an image-native file (comics, scans, garbage
victims) must route down a paged path instead of storing a fake text.

The verdict does not stay a return value: every parsed PDF gets a
`pdf_files` row — classification, page count, whether a text layer existed
at all, the parser version — keyed by `media_file_id`, replacing any
previous verdict the way a re-parse replaces a canonical text. That row is
what the position endpoints read to know a book answers on the page axis,
and what the sizing queries read to count a 32-page picture book as 32
pages. Text-native rows are kept too: their page count is the seed a
future page-map-from-PDF will want.

The ingester routes on that classification: text-native parses into the
canonical tables (and its `pdf_files` row records the fact); `drm` is
refused whole (the parse-time twin of the scanner's `drm_pdf` skip);
`corrupt` fails and is reported; and **image-native is refused a canonical
text with a named label** — the text endpoints answer 422 saying the file
is pages, not prose — while the classification row routes the book onto
the paged position model. No `epub_texts` row exists for an image-native
PDF, so nothing downstream can mistake it for a book it can read.

DRM is refused whole: any `/Encrypt` dictionary is an `ErrDRM` the caller
maps to a `drm_pdf` skip reason — including owner-password-only
"restrictions" files that decrypt with an empty user password. DRM-free
crowd, by decision, no half-support.

**The library decision** (the roadmap said pdfcpu; reality corrected it):
pdfcpu has *no text extraction* — it is an object-model and manipulation
toolkit, so "start from pdfcpu for extraction" was impossible. What the
arena actually uses, both pure Go, CGO-free and GPL-compatible:

- **github.com/ledongthuc/pdf** (BSD-3, the maintained rsc.io/pdf fork)
  reads the object model and decodes content-stream text into positioned
  runs — font encodings, ToUnicode CMaps, embedded TrueType charmaps.
  Unmappable glyphs surface as U+FFFD, which is the gate's raw signal.
- **github.com/pdfcpu/pdfcpu** (Apache-2.0) reads outlines only: its
  bookmark reader resolves destinations — including named ones — to page
  numbers, which the text reader does not expose.

`go-fitz` (CGO/MuPDF) and unipdf (AGPL) remain out by constraint. If
extraction quality disappoints on real-world books, the mobi-go move
applies unchanged: a focused standalone parser repo rather than weak
output.

---

## The designated audiobook

The same choice, one shape harder, on the audio side. A book can have
several **recordings** attached — the same title read by two different
narrators, an abridgement beside the unabridged rip — and exactly one of
them is the audiobook: the tape the timeline is built from, the one the
player plays, the one an alignment is measured against.

The unit being chosen is a *set*, because an audiobook is not one file but
N files that behave like a single tape. So the designation cannot live on
`media_files` the way `is_primary_text` does: `audio_editions` is one row
per recording, `media_files.audio_edition_id` points at it, and
`is_primary` names the one that counts (one per book, partial unique
index). Everything an edition displays — its label, its length, its track
count, its narrator — is derived from its files on read, so a directory
renamed on the NAS renames the edition and nothing goes stale.

Grouping is not only how a user picks a version; it is what makes owning
two survivable at all. Before it, a second rip's tracks were numbered
1..N beside the first's and `ORDER BY track_number, path` interleaved
them: chapter one in one voice, chapter one in another, chapter two.

One attach batch is one recording (`POST /books/{entry}/files` with
`kind=audio`), and attaching a second one never moves the designation —
the same promise the text side makes when a `.mobi` lands beside the
`.epub`. The designation moves when someone moves it (`PUT
/books/{entry}/audio-editions/{id}/primary`) or when the last file of the
current one is detached. Both paths pay the same two costs:

- **The listening position is carried by proportion.** It is stored as
  `(raw_audio_file_id, raw_audio_seconds)` — deliberately track-relative,
  since a global offset moves on its own when a track is re-measured —
  which makes it meaningless the moment that file is off the timeline. So
  it is converted to a global second on the tape being left, taken as a
  fraction of that tape's length, and placed at the same fraction of the
  new one. Two readings are not the same length, but they are the same
  book. A tape nobody could measure has no fraction to work with; those
  positions are cleared rather than guessed, and `percent_complete`
  stands.
- **The alignment is deleted.** Its anchors map char offsets onto the
  seconds of one specific performance, and a second narrator does not say
  the same words at the same times.

A recording whose files are all off the mount cannot be switched onto:
the request fails naming the reason, rather than designating a timeline
with no bytes behind it.

---

## The paged position model

Some books only exist as pages: comics, manga, picture books, scans. The
parser's quality gate classifies their PDFs **image-native**, and
everything the arena knows about position assumes a canonical text that
these books deliberately do not have. A `char_count` of zero with a stored
offset is the plausible-wrong-answer failure mode invariant 5 exists to
prevent, so the model says the honest thing out loud instead: **their
position is a page index.**

**The shape follows the `raw_audio_*` precedent: one row, one honest
exception, flagged.** `book_progress` carries a `position_mode` —
`'text' | 'page'` — and a nullable `page_index`: text rows are
byte-identical to everything that existed before the migration (mode
text, page NULL, char offset the truth); page rows carry the page index
and pin `char_offset` to 0, because there is no text axis to collect a
number on. The pairing is enforced by the store before it reaches SQLite,
the same way the raw-audio half-pair is. A separate `paged_progress`
table was the alternative and was rejected for the reason the raw audio
pair's shape was: a second table splits the "one row per entry" contract
every position-reading path already leans on — each one would have to
remember to check it, and the ones that forgot would silently serve
char zero as "the beginning" of a book that has no such coordinate.

What each piece of the arena does with it:

- **Classification home.** `pdf_files` (one row per parsed PDF, keyed by
  `media_file_id`) persists the gate's verdict — text-native or
  image-native, page count, has-text-layer, reason, parser version. The
  primary-text designation holds for a paged primary unchanged: a PDF
  with no text is still the file the book is *read* from, and the arena's
  primary predicate answers for it — `is_primary_text` points at it, the
  format rank orders it, only the axis it contributes differs.
- **Position.** `GET/PUT /api/books/{entry}/position` answers in page
  mode for a paged entry: `position_mode: "page"`, `page_index`,
  `page_count`; the chapter/audio-derived views that need a text are
  absent. Writes take `page_index` with `0 ≤ page < page_count` validated
  against the classified count; `char_offset` writes on a paged book are
  refused (422) rather than stored beside a meaning they cannot have.
  Percent is the page you are on over the reading span — page one is 0%,
  the last page is 100% — the same convention the char axis uses.
- **Sessions.** `reading_sessions` gains `pages_turned`; `mode` stays
  `read`, `chars_advanced` stays 0. No fake char deltas, ever.
- **Sizing.** The debt, insights and achievement sizing read the paged
  primary's `page_count` through the same predicate that reads
  `char_count`, one rung lower in the honesty ladder: measured chars,
  then measured pages, then the catalogue's page count. A 32-page picture
  book is a 32-page book, not the 12 words its absent text layer would
  have counted. The Reading Season rollup counts a paged finish like any
  finish, and its pages-read sums `pages_turned` beside the
  chars-to-pages conversion.
- **Text-side refusals stay refusals.** The reader's text endpoints
  (chapters, ranged text, display, search, passage matching, alignment
  eligibility) all answer 422 for an image-native primary. The paged book
  never enters a text-mode code path; there is no half-parse to find.

**Switching primary across an axis drops the old axis; it never
translates.** A page index and a char offset are not two encodings of one
position — they are positions in different spaces, and any mapping
between them would be an invented one that answers every query
confidently and wrongly. So switching between a text sibling and a paged
sibling (or detaching one into the other) mirrors the alignment-deletion
stance: entering page mode resets every entry of the book to page one,
deletes the paper page anchors (they are char offsets into the departed
text) and the alignment; leaving page mode resets to offset zero. The
percentage goes with the axis — unpositioned is honest, a rescaled guess
is not. What survives is the raw listening position, which was never
measured against any text. The text↔text rules (SHA-identical carries
everything; otherwise percent-recompute and anchor scaling) are
untouched.

The paged *reader* — serving page images, page-turn UI, peek-to-page — is
the next section.

---

## The paged reader

Reading a comic is turning pages, and the two candidate ways to put a PDF's
pages on a screen were evaluated and decided:

**(a) Companion page images — chosen.** The page's embedded image XObject
is extracted in the API and served as an ordinary raster; the reader is
plain `<img>` elements. Extraction is **lazy, one page at a time**: the
first request for a page decodes it and writes a companion file beside the
canonical texts (`{pdf-file-id}.page-N.png|jpg`, the `ingest.go`
companion pattern), and every later request is a disk read. Lazy is what
bounds the disk — a companion exists only for a page someone actually
looked at, and a comic read cover to cover costs at most roughly its own
image payload once, never a multiple of it.

**(b) Vendored PDF.js — declined, for now.** Streaming the raw file and
rasterizing in the browser would render vector pages and cost no companion
disk, but it ships a multi-megabyte vendored web dependency, moves the
reading surface onto canvas code we do not own, and abandons the
asset-endpoint containment model every other byte of a book flows through.
The Tesseract-WASM vendoring precedent made it defensible; nothing in real
libraries has yet made it necessary. The measured tradeoff under (a): the
web bundle carries no new dependency and grows only by the reader
component itself (+8.3 kB minified, +1.8 kB gzipped over the pre-paged
build), and the cost is the named refusals below.

pdfcpu does the extraction (`internal/books/pdf/pages.go`): it already
reads outlines (the TOC half of stage 1) and it is the one pure-Go
implementation that handles the real filter zoo — Flate, LZW, CCITT, DCT
with SMask compositing, colorspace conversion. A bitmap source re-encodes
as PNG; a DCT page passes through as its original JPEG bytes. The
whole-file validate+optimize pdfcpu wants happens once per file per
process (a four-entry LRU of open extraction contexts in the ingester);
each page after that is a single decode. Invariant 4 holds — pure Go, no
CGO, nothing new in the distroless image.

**Refusals named, never broken images.** Vector-only PDFs — pages of
drawings with no embedded images — cannot be served under (a), and a file
with zero image-bearing pages is refused whole (the `.kfx` honesty). A
*mixed* file opens; its image-less pages each render a labeled panel, as
do pages whose codec a browser cannot render (JPX, JBIG2, TIFF). A
multi-image page serves its **largest** image by pixel area — right for a
scan or a comic plate, one slice short for a genuinely sliced page; full
compositing would need content-stream placement tracking and has
deliberately not been built.

**The endpoints.** `GET /books/{entry}/pages` is the manifest — the
classified page count plus every page's image shape from stub lookups, so
"page N of M" and every placeholder are honest before any image bytes are
paid for. `GET /books/{entry}/pages/{page}` is one raster through the
asset-endpoint pattern (`handlers_epub.go`): authenticated per request,
path-contained, ETagged, cached hard and private, sandboxed. An entry is
dispatched by its classification, not its extension — `position_mode:
"page"` opens the paged reader; a text-native PDF (even one whose pages
are full of art) stays in the scrolled reader, and a paged book never
enters a text-mode code path.

**The reading rules.** Discrete page turns map one-to-one onto the paged
model's position put — turn events write `page_index`, no
scroll-percentage arithmetic anywhere. The position restores on reopen and
on a second device exactly like the text reader's offset, because it is
the same one-row store. And the MAD-441 peek rule holds on the page axis:
`?page=N&peek=1` lands on a page view without a single position write —
not the landing, not the checkpoint, not the leaving beacon — until the
reader deliberately ends it. Re-classifying a file clears its page
companions (a stale page image is a plausible-wrong answer with a
filename). Search hits from the [OCR lettering corpus](#the-second-corpus-ocr-lettering-search)
arrive as exactly this peek shape.

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

Lists and projects scope differently. A smart list carries its arena in
its own rule set (`media_type eq book`); the builder offers only that
arena's fields, and the server rejects the other arena's fields from a
scoped set. Projects are arena-scoped at the row level —
`projects.media_scope` is `'game' | 'book'`, set from the arena the
project was created in — so a count goal counts finished books, a rule
goal's match pool stays books, and a checklist refuses entries from the
other arena.

The book-specific hierarchy, one table per concept:

| Table | Is a | Keyed by | Notes |
|---|---|---|---|
| `books` | **Work** | Open Library work key (`OL12345W`) | Shared metadata cache, like `games`. Authors/subjects stay JSON until faceting needs them |
| `book_editions` | **Edition / Printing** | OL edition key (`OL12345M`) | ISBN10/13, publisher, page count, binding. Page numbers belong *here*, not to the work |
| `library_entries` | your copy of the work | + `media_type`, `book_id`, nullable `edition_id` | The spine. `edition_id` is the printing the entry is anchored to — chosen at add time, changed later with `PATCH /library/{id}` (null = "I don't know which") |
| `physical_copies` | the lump of paper | `(user, entry, edition)` UNIQUE | A printing the user holds, owned or borrowed (`acquisition`, `due_at`, `returned_at`). The thing page anchors attach to — a second printing is a second row with its own map |
| `media_files` | EPUB & audiobook files | `(root, path)` UNIQUE | The NAS inventory — pointed-at, never uploaded. `is_primary_text` names the one text file a book is read from, one per book; `audio_edition_id` names the recording an audio file belongs to |
| `audio_editions` | one **recording** of a work | per book, `is_primary` unique per book | The set of audio files that behave as one tape. `media_files.audio_edition_id` points here; only the designated edition is a timeline. Label, length and narrator are derived from the files, never stored |
| `media_sidecars` | parsed `.opf` metadata | `(root, path)` UNIQUE | Replaced per root each scan; the matcher's best evidence |
| `epub_texts` / `epub_chapters` | parsed canonical text | per media file | Only the designated primary is parsed. See above |
| `pdf_files` | the quality gate's verdict on a PDF | per media file, UNIQUE | text-native / image-native, page count, has-text-layer. The paged model's fact source; never a text |
| `pdf_pages` | a PDF's own pagination | `(media_file_id, page_number)` PK | Per-page `[char_start, char_end)` of a text-native PDF's canonical text — the epub_chapters precedent one level finer; replaced wholesale with each parse. The seed a paper copy's map can be grown from |
| `book_progress` | position | entry PK | One row per entry: `char_offset` is the truth — or, flagged `position_mode: 'page'` for a book with no text, `page_index` is |
| `reading_sessions` | consumption log | per user, per entry | `mode` ∈ read/listen; `chars_advanced`, `pages_turned` |
| `alignment_jobs` / `alignments` / `alignment_anchors` | audio↔text map | per entry | See [alignment](#the-alignment-pipeline) |
| `ocr_jobs` / `ocr_pages` | search-only lettering corpus | per media file | The second corpus: a paged book's drawn lettering, pinned per page to the image's sha256. Never canonical; the canonical pipeline cannot load it. See [the second corpus](#the-second-corpus-ocr-lettering-search) |
| `page_anchors` | paper↔text map | `(physical_copy_id, printed_page)` PK | See [page anchors](#the-page-anchor-map) |

Two shapes worth internalising:

- **EPUB and audiobook are siblings of Edition, not properties of it.**
  Files are `media_files` rows attached to the *work* (through the
  entry's attach flow, in explicit track order for audio); an edition is
  metadata about a printing. You can own the paperback of printing A,
  the EPUB, and the audiobook, all of one work — three ways to consume,
  one position.
- **`book_progress` stores one number and admits two honest exceptions.**
  `char_offset` + `char_offset_source` (`read` / `listen` / `scan` /
  `manual`) is the truth. Before an alignment exists there is no map
  from a listening position to a char offset, so those writes land in
  track-relative `raw_audio_seconds` / `raw_audio_file_id` and the API
  reports `derived: false` rather than fabricating an offset. And a book
  with no text (an image-native PDF primary) stores `page_index` with
  `position_mode: 'page'` and `char_offset` pinned to 0 — flagged as a
  different axis, never a faked offset.

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
  distinguishes six different statements: `drm_epub`, `drm_mobi` and
  `drm_pdf` (refusals named for the lock they found),
  `unsupported_extension` (which covers `.aax`/`.aaxc` and genuinely
  unrecognised files), `format_unhandled` for `.kfx`, the one Kindle
  format this tool recognises and chose not to parse, and
  `sidecar_metadata` for a `.opf`, which is not a book at all.
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
  candidates, and text files by directory-plus-filename-stem, and
  proposes (book, confidence) suggestions. Evidence is a
  ladder, expressed as ordering rather than arithmetic — the first source
  that yields a title wins: an OPF metadata block (a `.opf` beside the
  files, or an epub's own package document, or a pdf's Info dictionary /
  XMP packet), then ID3/MP4/Vorbis tags, then directory layout, then the
  bare filename. Above 0.72 the UI offers
  them for bulk confirmation; nothing auto-attaches.
- **A sibling is an answer already given.** A text file whose stem is
  already attached to a book — `Carrie.mobi` beside the `Carrie.epub`
  confirmed last month — resolves to that book directly: confidence 1,
  sourced from the library, no identifier lookup and no Open Library
  search. It is not a fuzzy title match; two files are siblings only when
  someone named them identically in the same folder, which is a statement
  of intent. The candidate is flagged `alternate_format` so the UI says
  "another format of a book you have" rather than presenting a fresh
  find, and confirming it records the format without disturbing the text
  the book is read from.
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
attaches to `physical_copies` — a printing the user holds, owned or
borrowed from the library — and one printing's map can never bleed into
another's. The borrowed lifecycle never touches the map: return stamps
`returned_at`, a re-checkout of the same printing reopens the same row
(its map intact) with a fresh due date, and buying the book you had out
flips the row to owned. Only "Forget this copy" ever deletes a map.

Which map the position endpoints *read* is a separate question, and it is
answered by `library_entries.edition_id` — the printing the entry itself is
anchored to. Registering the first copy of an unanchored entry adopts its
printing ("I own this on paper" is the strongest statement anyone makes
about which printing an entry is); after that the choice only moves when
someone moves it, from the Printings panel or from the copy that says it is
not the one being read. Changing it is cheap and reversible: each copy keeps
its own anchors, so the switch swaps which map is read and destroys nothing,
and clearing it (`edition_id: null`) is the honest "I don't know which
printing this is" — page numbers fall back to the even stretch.

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

The map is **seeded before the first scan**, from two places:

- **The catalogue stretch.** The printing's own page count (from the
  edition metadata) stretches page 1 across the start of the text and the
  last page across the end, at a deliberately low confidence (0.3 — a
  real catalogue number, but nobody has looked at the paper). That is
  enough to answer "roughly where am I in the paperback" the day a copy
  is registered, and every real scan lands inside it and tightens the
  segments it falls between; a seed that would contradict a real scan is
  dropped, because the catalogue being wrong and the reader being right
  is exactly the case.
- **The PDF seed** — *a PDF is a printing.* A text-native PDF's parse
  yields exact per-page char ranges (`pdf_pages`, one level finer than
  the chapters), and a "seed from PDF" action on a copy writes those
  ranges as `page_anchors` rows: per-page text density known, not
  stretched. The confidence is the same 0.3 class — the PDF's page N and
  *this* printing's page N are usually offset by front matter, so the
  slope is trustworthy and the intercept is not. The honesty rules match
  the stretch's exactly: seeds yield to any real scan (a scan overwrites
  its page's seed through the composite PK; a seed arriving on a scanned
  page is dropped, never stacked), re-seeding replaces yesterday's seeds
  wholesale so a re-parse can't leave stale offsets behind, and the
  anchor's `source: 'pdf'` provenance is never presented as a scan — the
  copy panel says "seeded from the PDF, unscanned" until paper
  contradicts it.

  The offsets are the *PDF's* canonical text's, so the seed writes only
  when that text is the one the book's page anchors are measured against:
  the PDF is the primary, or a sibling canonicalizing to the same
  `normalized_sha256` (the converted-pair common case). A different text
  is refused outright — rescaling a whole map onto another
  canonicalization is exactly the plausible-wrong-answer move this arena
  exists to prevent, and no rescale path exists to abuse.

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

## Searching the text

The cheapest feature in the arena, and the one that shows what the arena is
for. `GET /api/books/{entryID}/search?q=` finds a phrase in the canonical
text and answers with offsets — and because an offset is the one stored
position, the same translator that serves the reader hands every hit back
already carrying its chapter, its page of *this reader's printing* (with the
error bar), and its second of the audiobook. Enter opens the reader on the
paragraph as a **peek** — the passage is shown but the stored position does
not move until the reader goes on reading there or finds their own way back;
⌘↵ starts the player on the sentence.

The mechanics, in `api/internal/books/search/`:

- **The corpus is the canonical text, not `transcript_segments`.** Both
  describe the same book, but one is the author's words and the other is
  Whisper's guess at a narrator reading them — and a transcript only exists
  for a book that already has an EPUB, because alignment needs one. The
  worse text is never the better index.
- **Punctuation is already solved.** The canonical text has had case,
  quotes, dashes and apostrophes folded out of it, so folding the query
  through the same `booktext.Normalize` makes `“Don’t,” he said—finally.`
  and `dont he said finally` the same string, and finding one inside the
  other is `strings.Index` over well under a megabyte.
- **Two tiers, named in the response.** *Phrase* is a substring scan at
  token boundaries; the last word is allowed to match as a prefix only when
  the book contains no whole-word hit, which is what makes the box feel live
  without burying "the door" under every "theatre". *Loose* is the fallback
  when the phrase is nowhere: a token-window scan that forgives one
  misremembered word and any word order, driven from the rarest terms. The
  UI says which one it is showing rather than letting a fallback pass for a
  hit.
- **Snippets are prose, not the address space.** A hit is mapped through the
  block index onto `{id}.display.txt` and highlighted in place by
  `booktext.SpanInDisplay`, which reconstructs each display field's canonical
  range by re-folding it. The answer is word-aligned — half of "well-known"
  widens to the whole word — because the fold is not byte-for-byte inside a
  word and a highlight that ended mid-word would claim a precision the
  crossing does not survive.
- **The index is in memory, and that is deliberate.** Same reason the
  canonical text is a file: the database runs on one connection and this is a
  keystroke path. An FTS5 table would put every character typed into a search
  box in line behind whatever else wants to write. A novel's token index
  builds in milliseconds and lives in a four-entry LRU keyed by text id *and*
  normalized SHA, so a re-parse that reuses an id cannot serve offsets into a
  book that no longer exists. The derivation inputs behind the page and
  timecode — chapters, timeline, a few thousand anchors — sit behind a 30-second
  TTL cache for the same reason; every endpoint that reads or writes a real
  position still loads them fresh.

### The second corpus: OCR lettering search

A comic or picture book has no canonical text — that is the paged model's
whole premise — but its pages carry drawn lettering, and "which page does he
say X on" is a question that deserves an answer. So a second, **search-only**
corpus exists, and it never pretends to be the first one:

- **The worker is the align pattern applied a second time.** Tesseract lives
  in an optional container (`ocr/`, behind the `ocr` compose profile and its
  own `OCR_WORKER_TOKEN`) — no OCR code enters the distroless API image, and
  no token means the `/internal/ocr` queue answers 503 and the feature does
  not exist. The queue itself is the alignment queue's shape: claim,
  heartbeat, stale reclaim, one active job per media file.
- **The worker reads pages, never the file.** Each page image streams from
  the API's internal endpoint, which serves the exact companion-or-extract
  bytes the paged reader serves — no second decode path exists, and the
  worker's fetches warm the same companion cache. The image's sha256 rides
  the response; the per-page result is stored pinned to it (`ocr_pages`,
  keyed `(media_file_id, page_number)`), the extraction-ledger pattern: a
  re-run skips every page whose image did not change, so unchanged pages are
  never re-billed.
- **Hits carry page targets, never offsets.** Search over an image-native
  primary hits this corpus with the same two tiers (phrase, then loose),
  per page, and answers on the page axis: a hit opens the paged reader at
  `?page=N&peek=1`, a peek that moves nothing — the peek rule holds on the
  page axis exactly as on the text axis.
- **Honesty is the `low_confidence` pattern.** Coverage (pages that yielded
  any lettering over the classified page count) and mean per-word confidence
  are computed by the API from the corpus it owns, graded against
  `OCR_MIN_COVERAGE` / `OCR_MIN_CONFIDENCE` (defaults 0.30 / 0.60 — judgment
  numbers: stylized lettering is best-effort, and a sparse corpus is still
  searchable once it says so). Below the thresholds the corpus stays usable
  as `low_confidence` and the UI says what that means.
- **Never eligible for the canonical pipeline.** Nothing about `ocr_pages`
  is canonical: no `epub_texts` row can point at it, so the position axis,
  alignment, passage matching and any knowledge layer are *structurally*
  unable to load it — and the load-site tests pin that, not a convention.
  A re-classified PDF drops its companions, its corpus and its queue rows
  together (a stale page image is a plausible-wrong answer with a filename;
  lettering read off one is the same thing one louder).

---

## Invariants

Break these and the arena degrades in ways that are easy to miss in
review.

**1. `books.Normalize` is pinned and versioned.** Every stored offset
in the arena — reader position, alignment anchor, OCR page anchor —
assumes *this exact function* (`api/booktext/normalize.go`), on both
the ebook side and the transcript side; the worker imports it, never
copies it. Changing the rules (or the block extraction the epub and
mobi parsers share) requires
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

**4. Supported formats are epub / mobi / azw / azw3 / pdf / mp3 / m4a /
m4b / opus, and both DRM and KFX are out of scope by decision.** `.aax`,
`.aaxc`, DRM-wrapped epubs (`drm_epub`), DRM-wrapped Kindle files
(`drm_mobi`) and DRM-wrapped PDFs (`drm_pdf`) are skipped and *reported*
(`media_skipped`), never half-supported. This tool is for the DRM-free
crowd; do not add "just one container" of DRM circumvention.

`.mobi` / `.azw` / `.azw3` are parsed — by
[mobi-go](https://github.com/madeofpendletonwool/mobi-go), the pure-Go
Kindle reader built for exactly this: PalmDOC and HUFF/CDIC
decompression, KF8 reassembly, INDX NCX tables of contents. A Kindle
file inventories like an EPUB (same text-side kind, same EXTH-driven
matcher evidence), parses into the same canonical text through the same
`Canonicalize`, and its DRM'd siblings are detected at scan *and* at
parse time and refused whole.

`.pdf` is parsed too — and it is two populations wearing one extension.
A **text-native** PDF (rare ebooks, RPG books, technical titles) is just
a third container parser: it inventories in the text-side kind with its
Info/XMP metadata feeding the matcher, parses through
`internal/books/pdf` into the same canonical text, and gets the whole
arena — reader, search, passage matching, alignment eligibility —
unchanged. Its parse also records the file's own per-page char ranges
(`pdf_pages`), because a PDF is a printing: those ranges are what seeds
a paper sibling's page map (see [page anchors](#the-page-anchor-map)).
An **image-native** PDF (comics, manga, picture books, scans)
has no text layer to trust, so the parser's quality gate classifies it
at parse time, the ingester refuses it a canonical text with a named
label, and the persisted verdict routes it onto the paged position
model: its honest position is a page index, never a faked `char_count`.
Never half-parse either population into the canonical tables. `.kfx` is the remaining refusal
and is labelled as one (`format_unhandled`): no open reader exists for
it, and the honest answer is to name the format and point at the EPUB or
MOBI of the same book. Do not half-implement it inside the scanner.

Every container parser in the API is hand-rolled pure Go and stays that
way: the image is distroless with `CGO_ENABLED=0`, so there is no
ffprobe to reach for. Opus duration comes from the last Ogg page's
granule position minus the encoder's pre-skip, over the fixed 48 kHz
granule clock — header reads only, like the MP3 and MP4 parsers beside
it. (Ogg Opus needs Safari 17.4 or newer on the client; that is a
browser limitation, not a reason to transcode files we promised never to
write to.)

**5. One position per axis, and the text axis's is the char offset.**
Audio seconds and pages are derived views, never independently stored
truths — the exceptions are both flagged: the pre-alignment
`raw_audio_*` fallback (marked `derived: false` precisely because it
isn't a derivation) and the page axis of a book with no canonical text
(`position_mode: 'page'`, page index stored because a char offset
cannot exist). Audio anchors are on the **global** timeline;
track-relative offsets exist only inside the audio package. The axes
are never translated into each other — a text↔paged primary switch
drops the old axis rather than mistranslating it.

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
| `GET/POST /api/books/{entryID}/files`, `DELETE …/files/{fileID}` | attach / detach EPUB & audio (the GET also carries the book's `audio_editions`) |
| `PUT /api/books/{entryID}/files/{fileID}/primary`, `PUT …/audio-editions/{editionID}/primary` | choose the canonical text / the audiobook that plays |
| `GET /api/books/{entryID}/text[/chapters\|/display\|/asset]` | canonical text ranged reads |
| `GET /api/books/{entryID}/pages`, `GET …/pages/{page}` | the paged reader: page manifest + page rasters (image-native PDFs) |
| `GET /api/books/{entryID}/audio`, `GET …/audio/{trackID}` | timeline + track bytes (Range) |
| `GET/PUT /api/books/{entryID}/position`, `GET/POST …/sessions` | the one position, and reading sessions — in text mode (char offset) or page mode (page index of a paged book) |
| `POST/GET/DELETE /api/books/{entryID}/align` | alignment enqueue / status / delete |
| `POST /api/books/{entryID}/passage` | OCR / typed passage → offset (+ alternatives) |
| `GET /api/books/{entryID}/search` | search the text; hits carry chapter, page and timecode — or, on a paged book, page targets from the lettering corpus |
| `POST/GET/DELETE /api/books/{entryID}/ocr` | lettering enqueue / status / clear — the search-only second corpus for paged books |
| `GET/POST …/copies[…]`, `POST …/copies/{copyID}/return\|/reopen\|/own`, `GET/POST …/copies/{copyID}/pages`, `POST …/copies/{copyID}/seed-from-pdf` | physical copies (owned + borrowed) + page anchors (scanned, pinned, or seeded from a text-native PDF) |
| `/api/achievements/reading-season` | the per-year Reading Season rollup |

---

## Verifying a change

```bash
cd api && go test ./...
cd web && npm run typecheck && npm run build
cd align && go test ./...
cd ocr && go test ./...
docker compose build && docker compose up          # plain profile
docker compose --profile align up                  # + the alignment worker
docker compose --profile ocr up                    # + the lettering worker
```

Then walk it like a stranger would: add a book by ISBN, scan the NAS,
attach the files, read a chapter, enqueue alignment, hand off between
reader and player, scan a paper page and check the page number arrives
with its error bar. `go run ./cmd/alignbench` inside `align/` measures
whether an aligner change moved the numbers the ready/low_confidence
thresholds were chosen from.
