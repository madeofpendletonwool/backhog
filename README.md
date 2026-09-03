# 🐗 Backhog

A self-hosted game backlog manager and books library. Add games, pull
metadata automatically from IGDB, sort them into lists, and drag your play
queue into the order you'll actually get to them. Or add books from Open
Library and read, listen, and track them on the same spine — see
[Books](#books).

- **Library** — cover grid or dense table, filter by status, platform, genre,
  paged as it grows; your filters and sort are remembered between visits
- **Dashboard** — "Your Gaming Problem": a diagnosis of the pile (games owned,
  unplayed, hours owed, years at your pace) plus ridiculous superlatives — the
  oldest game you never touched, the genre you keep buying but never playing,
  the platform with the worst backlog
- **Statuses** — backlog / playing / played / dropped / ignored, with automatic
  start and finish timestamps. *Ignored* is for games you own and have played but
  will never "beat" — endless titles that shouldn't sit in the backlog or drag
  down your completion
- **Play queue** — drag to reorder, or jump a game to the top/bottom (or nudge it
  up/down) with one click, with a running "how deep am I" hour count
- **Series** — franchises and collections as first-class journeys: one Mass
  Effect card instead of three rows, with completion, hours remaining, a
  play-order selector (release, chronological with DLC nested under its game,
  recommended, custom drag, or "just the good ones"), and DLC awareness that
  feeds the backlog-debt breakdown
- **Lists** — hand-curated and drag-sortable, or **smart lists** defined by rules
  that stay current on their own
- **Per-game detail** — a full IGDB dossier (developer/publisher, platforms, game
  modes, themes, screenshots, videos, similar games, DLC, age ratings…) alongside
  your rating, notes, playing-on platform, and lists
- **Wishlist** — track what you want separately from what you own; set from a
  game's detail page, and kept out of your backlog hours and completion percentage
- **Playtime** — log sessions by hand, because a process watcher measures how
  long the game was *open*, not how long you actually played
- **What should I play?** — give it tonight's time budget and get four picks
  with human reasons: continue something, a short win that fits, a wildcard
  you've never touched, and the longest-owned least-played rescue — or just
  roll one at random
- **Achievements & Backlog Season** — trophies for progress through the pile,
  not hours in it: first finish, five-game cleanup crew, the 5-year
  archaeologist dig, honest abandonment, and more. Plus a per-year
  "Backlog Challenge" card tracking completions, hours, franchises cleared,
  and rescues of long-owned games
- **Steam import** — bulk-import an owned library, matched to IGDB by appid
- **Books** — the same backlog spine carrying books instead of games, plus
  position translation between paper, audio and EPUB: read in the browser,
  listen at speed, stop in the car and resume on the right sentence, scan a
  paper page with your phone to pin *your printing* into the map (see
  [Books](#books) and [docs/BOOKS.md](docs/BOOKS.md))
- **Multi-user** — real accounts, fully isolated libraries, shared metadata cache

Go API · SQLite · React + Vite + Tailwind · Docker Compose.

---

## Quick start

```bash
cp .env.example .env      # then add your IGDB credentials (see below)
docker compose up --build
```

Open <http://localhost:8080> and register an account. The first account you
create is just a normal account — there's no admin tier.

### Getting IGDB credentials

Game search needs IGDB, which authenticates through Twitch. It's free:

1. Sign in at <https://dev.twitch.tv/console/apps>
2. **Register Your Application** — any name, OAuth Redirect URL
   `http://localhost`, Category *Application Integration*
3. Copy the **Client ID**, then click **New Secret** for the **Client Secret**
4. Put both in `.env`:

```env
IGDB_CLIENT_ID=your_client_id
IGDB_CLIENT_SECRET=your_client_secret
```

Without them Backhog still runs and serves everything already in its cache —
only *adding new games* is disabled, with a clear message saying so.

### Importing from Steam (optional)

Set `STEAM_API_KEY` in `.env` from
<https://steamcommunity.com/dev/apikey>. This is one key for the whole
deployment, not a per-user secret — it can read any *public* profile, so each
user just supplies their own SteamID. Their Steam privacy setting for **Game
details** must be Public.

Steam appids are matched to IGDB through IGDB's `external_games` table, which is
an exact join. Name matching would mangle cases like *Prey* (2006 vs 2017).

---

## Books

The second arena: the same statuses, queue, lists and achievements,
carrying books — plus the thing a book tracker can't do. **Position
translation**: one book consumed as paper, audio and EPUB is one book
at three coordinates, and Backhog converts between them. Read the EPUB
in the browser reader; listen to the audiobook; stop in the car and
the reader opens on the right sentence. Scan a paper page with your
phone and Backhog pins that page of *your printing* into the same map —
no external page-number database, and the map gets better every time
you use it. How it works is [docs/BOOKS.md](docs/BOOKS.md).

### Your files, your NAS — there is no upload

Backhog never uploads or copies media. Your library lives on the NAS
and is bind-mounted into the container **read-only**; Backhog
inventories it and points at it. Uncomment and edit the mounts in
`docker-compose.yml` (shown for the `api` service; the `align` worker
wants the same two lines):

```yaml
volumes:
  - /mnt/nas/audiobooks:/media/audiobooks:ro
  - /mnt/nas/ebooks:/media/ebooks:ro
```

…then list the container-side paths in `.env`:

```env
MEDIA_DIR=/media/audiobooks:/media/ebooks
```

Scan from the Books library page and Backhog walks the roots, grouping
audiobook directories into ordered candidates and proposing matches
from the files' own tags. A temporarily unmounted NAS flags files as
missing; it never destroys the associations you made. Nothing under
those mounts is ever written to.

### Formats and DRM

Supported: **epub** for text, **mp3 / m4a / m4b** for audio. Anything
else — Audible `.aax` / `.aaxc`, DRM-wrapped epubs — is skipped and
*shown* as unsupported with a reason, not silently ignored. DRM is out
of scope by decision: this is a tool for the DRM-free crowd (Libro.fm
downloads, Libby rips, indie EPUBs).

### Metadata

Books come from [Open Library](https://openlibrary.org) — no API key,
no OAuth, nothing to configure. Just search, or paste an ISBN.

### Alignment (optional)

The reading↔listening handoff needs the audiobook transcribed
(Whisper) and force-aligned to the EPUB. That runs in a separate
**optional** worker container:

1. Generate a shared secret: `openssl rand -hex 32`
2. Put it in `.env` as `ALIGN_WORKER_TOKEN=…`
3. `docker compose --profile align up --build`

Know what you're opting into. The worker image bakes in ffmpeg,
whisper.cpp and the speech model itself (with the default `base.en`
model: 148 MB; `tiny.en` 75 MB, `small.en` 488 MB, `medium.en` 1.5 GB —
switch with `WHISPER_MODEL`, a rebuild not a restart), so a plain
`docker compose up` never builds or pulls any of it. And alignment is
**real CPU time: roughly 30–90 minutes for an 11-hour book** on a
decent desktop CPU with `base.en` (`small.en` is noticeably more
accurate and about three times slower). It's a one-time per-book
background cost; jobs are resumable and a killed worker's job is
picked back up.

Everything else works without the worker — reading, listening (as its
own timeline), tracking, page scanning, achievements, the Reading
Season. Alignment only unlocks the audio↔text handoff.

### Serving over HTTPS

If you put Backhog behind an HTTPS reverse proxy, set `COOKIE_SECURE=true` in
`.env`. Leave it `false` for plain HTTP on a LAN address — browsers silently
discard `Secure` cookies on non-HTTPS origins, and login just appears to do
nothing.

---

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `IGDB_CLIENT_ID` | — | Twitch app client ID; enables game search |
| `IGDB_CLIENT_SECRET` | — | Twitch app secret |
| `STEAM_API_KEY` | — | Steam Web API key; enables bulk library import |
| `MEDIA_DIR` | — | Books: read-only library roots, colon-separated (container-side paths of the `:ro` mounts) |
| `ALIGN_WORKER_TOKEN` | — | Shared secret for the optional alignment worker; enables the `/internal` worker API |
| `WHISPER_MODEL` | `base.en` | Alignment worker: which Whisper model to bake in (a build arg, not a restart) |
| `WHISPER_THREADS` | all cores | Alignment worker: CPU threads for transcription |
| `ALIGN_MIN_COVERAGE` | `0.80` | Alignment: fraction of the book anchors must span to publish as `ready` |
| `ALIGN_MIN_CONFIDENCE` | `0.60` | Alignment: mean anchor confidence required to publish as `ready` |
| `PORT` | `8080` | Host port for the web UI |
| `COOKIE_SECURE` | `false` | Mark the session cookie `Secure` (HTTPS only) |
| `DATABASE_PATH` | `/data/backhog.db` | SQLite file |
| `COVER_DIR` | `/data/covers` | Cached cover art |

Everything lives in the `backhog-data` volume — the database and the cover
cache. Back that up and you've backed up Backhog.

---

## Development

Run the API and the frontend separately, with hot reload on both:

```bash
# terminal 1 — API on :8080
cd api
DATABASE_PATH=./backhog.db COVER_DIR=./covers \
IGDB_CLIENT_ID=... IGDB_CLIENT_SECRET=... \
go run ./cmd/backhog

# terminal 2 — Vite dev server on :5173, proxying /api to :8080
cd web
npm install
npm run dev
```

```bash
cd api && go test ./...      # store + auth unit tests
cd web && npm run typecheck
```

---

## How it works

**Shared metadata, private libraries.** `games` is a global cache keyed by IGDB
id — if two users both add Hollow Knight, there's one `games` row and two
`library_entries`. Every user-scoped store method takes `userID` as an argument
(`GetEntry(ctx, userID, entryID)`), so ownership filtering can't be forgotten at
the handler layer. Reaching for another user's entry returns 404, not 403 —
distinguishing the two would let you enumerate other people's libraries.

**Ordering is a fractional index.** `queue_position` is a `REAL`, and moving an
entry writes exactly one row: the new position is the midpoint of its
neighbours. When repeated inserts into the same slot exhaust float precision
(about 30 in the worst case), the queue renormalises back to even spacing and
retries. No O(n) rewrite on every drag.

**Smart lists compile to parameterised SQL.** Rules are `{field, op, value}`
triples resolved through a whitelist in `store/smartlists.go` — field names and
operators are never interpolated from user input, and values are always bound
parameters. An unknown field is a 400, not a SQL error. `hours_to_beat < 8`
also excludes games with *no* known playtime, since "unknown" shouldn't count as
"short".

**Covers are cached locally.** On first add, the cover is downloaded to the data
volume and a representative accent colour is sampled from the artwork (weighting
saturated pixels, so the accent isn't muddy brown). The UI tints each card with
it. The grid never hits IGDB's CDN at render time, and keeps working if IGDB is
down.

**The API image is distroless.** `modernc.org/sqlite` is pure Go, so the binary
builds with `CGO_ENABLED=0` onto `distroless/static`. There's no shell in the
image, so the container healthcheck re-invokes the binary itself
(`/app/backhog -healthcheck`).

---

## AI Disclosure

Yes AI helped me make this program. If you don't like it don't use it. I made this for my own personal usage so your opinions make no difference to me in scope of this project. 

---

## Project layout

```
api/
  cmd/backhog/          entrypoint, healthcheck probe
  booktext/             the pinned text normalizer every offset depends on
  internal/
    config/             env parsing
    db/                 sqlite open + embedded goose migrations
    store/              data access — one file per aggregate
    metadata/           IGDB client, Open Library client, cover cache
    media/              NAS inventory: scan, match, attach candidates
    books/              canonical text, position translation, passage matching
    achievements/       code-defined achievement catalogue + predicates
    auth/               argon2id, session cookie middleware
    http/               router and handlers
align/                   optional alignment worker (whisper.cpp + ffmpeg)
web/
  src/
    lib/                typed API client, formatters, OCR runtime
    hooks/              TanStack Query wrappers
    components/         cards, queue rows, dialogs, rule builder
    pages/              library, queue, lists, detail, settings, books
```
