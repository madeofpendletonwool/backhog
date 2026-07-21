# 🐗 Backhog

A self-hosted game backlog manager. Add games, pull metadata automatically from
IGDB, sort them into lists, and drag your play queue into the order you'll
actually get to them.

- **Library** — cover grid or dense table, filter by status, platform, genre,
  paged as it grows; your filters and sort are remembered between visits
- **Statuses** — backlog / playing / played / dropped / ignored, with automatic
  start and finish timestamps. *Ignored* is for games you own and have played but
  will never "beat" — endless titles that shouldn't sit in the backlog or drag
  down your completion
- **Play queue** — drag to reorder, or jump a game to the top/bottom (or nudge it
  up/down) with one click, with a running "how deep am I" hour count
- **Lists** — hand-curated and drag-sortable, or **smart lists** defined by rules
  that stay current on their own
- **Per-game detail** — a full IGDB dossier (developer/publisher, platforms, game
  modes, themes, screenshots, videos, similar games, DLC, age ratings…) alongside
  your rating, notes, playing-on platform, and lists
- **Wishlist** — track what you want separately from what you own; set from a
  game's detail page, and kept out of your backlog hours and completion percentage
- **Playtime** — log sessions by hand, because a process watcher measures how
  long the game was *open*, not how long you actually played
- **Pick for me** — a random backlog game, optionally "under 5 hours" or "85+"
- **Steam import** — bulk-import an owned library, matched to IGDB by appid
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
  internal/
    config/             env parsing
    db/                 sqlite open + embedded goose migrations
    store/              data access — one file per aggregate
    metadata/           IGDB client, cover cache, accent sampling
    auth/               argon2id, session cookie middleware
    http/               router and handlers
web/
  src/
    lib/                typed API client, formatters
    hooks/              TanStack Query wrappers
    components/         cards, queue rows, dialogs, rule builder
    pages/              library, queue, lists, detail, settings
```
