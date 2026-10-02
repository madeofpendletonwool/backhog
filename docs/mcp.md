# Asking your books with an AI assistant — backhog-mcp

Backhog does not embed an AI. Instead it ships **backhog-mcp**, a small MCP
(Model Context Protocol) server that turns your library into tools your own
assistant can use: Claude Code, Claude Desktop, any MCP-capable client, or a
local model for users who want the text to never leave their network. No
model runs inside backhog or this server — it is an adapter.

The spoiler safety is a server guarantee, not a model promise: the personal
API token this server holds reads nothing past your position **and** every
read defaults to `until=position`, so every tool result is clamped to *your
reading position*. An assistant using these tools cannot read ahead of you
unless you explicitly ask it to (`include_spoilers` on a tool call). The
one write the bridge carries is recording a quiz score — counts only, and
only with a token you gave the `quiz:write` scope.

## What your assistant sees

| Tool | What it does |
| --- | --- |
| `list_books(status?)` | Your library with reading status and progress percent. |
| `get_reading_position(book)` | Chapter, percent and character offset — the "as of" of every answer. |
| `list_chapters(book)` | Chapters; past your position they come back locked (title only). |
| `read_text(book, from?, to?, chapter?)` | The book's prose, one bounded page at a time (`next_from` to continue). |
| `search_book(book, query)` | Phrase search with exact quotes and citation links. |
| `search_library(query)` | Library-wide search: every book you can read, ranked. |
| `get_passage(book, char_start, char_end)` | The exact text at a character range — quote verification. |
| `find_mentions(book, name)` | Every place a name has come up so far (the name index, MAD-670). |
| `list_names(book)` | The book's own index of names so far — the cast you've actually met (MAD-670). |
| `list_series()` | Every series your shelf holds books of, with counts (MAD-469). |
| `get_series(series)` | Your books of one series in reading order, each with status and position (MAD-469). |
| `record_quiz_result(book, questions, correct, chapter_from?, chapter_to?)` | Report a quiz's outcome after grading it — the only write this server carries (needs a token with `quiz:write`, MAD-471). |

Every result carries the effective `bound` (where your reading stood) and
peek deep links (`…/read?offset=N&peek=1`) that jump your reader to a
passage without moving your saved position. `search_library` reports books
that matched only past your position as titles in `matched_beyond` — say
"the library knows, but you haven't read that far" rather than guessing
where. `find_mentions` and `list_names` read the name index: a name first
appearing past your reading position does not exist yet, and
`include_spoilers` lifts the clamp.

Series come from your library's own Calibre metadata: if your NAS shelf is
organized with sidecars, the books of a series are grouped and ordered by
their series index automatically (membership is named when files attach,
and a background walk heals libraries that predate it). A book you have
**finished** is served whole under the default clamp — nothing in it can
spoil you — while the book you're on stops exactly at your position.

## Prompts — the returning reader

The same server ships prompts (MCP `prompts/list`): instruction playbooks
your assistant runs with the tools above. Five ship today:

| Prompt | What it does |
| --- | --- |
| `previously_on(book)` | "Previously on…" — where you are, what has happened, who's who, and the open threads, every line cited with a peek link. |
| `cast_list(book)` | A dramatis personae of exactly the people introduced so far, each with a cited first appearance. |
| `series_so_far(series)` | The story so far across a series: the finished books in order, plus where you stand in the current one. |
| `quiz_me(book, chapters?)` | A quiz over the chapters you've actually read — questions first, answers after, every answer in the key cited with a passage link (MAD-471). |
| `discussion_prep(book, section?)` | Book-club questions grounded in cited passages, plus the ones the book doesn't settle (MAD-471). |

Each playbook carries the same rules: use the bounded tools only, never
set `include_spoilers`, cite every line with its deep link, cut any line
that cannot be cited, and answer "the book doesn't say (so far)" rather
than drawing on outside knowledge of the work. The recap's spoiler safety
is the server's clamp, not the model's judgment — a recap requested before
a mid-book reveal cannot contain it because the tools cannot serve it.

The quiz playbook ends the same honest way: after grading your answers
against cited passages, the assistant calls `record_quiz_result` with the
count it actually graded — never a flattering one — and tells you what
the result unlocked.

## An honest word about quiz achievements

Quiz results are **self-reported by whichever assistant gave the quiz**.
Backhog stores the counts; it does not grade, and nothing in it can verify
what your model scored you. The comprehension achievements (Book Report,
Gold Star, Honor Roll) and the Reading Season quiz stat are a game to play
with your assistant — **for fun, not proof** — and their descriptions say
so. If a score sounds too good to be true, it was graded by a friend who
likes you.

## Prerequisites

1. A backhog account with books in your library.
2. A personal API token: **Settings → API tokens → create**, copy the
   `bh_…` secret when it is shown — it is displayed exactly once.
   Read-only (`books:read`) covers every tool except `record_quiz_result`;
   tick **quiz:write** if you want your assistant to be able to record
   quiz results — the single write that scope names.

## Claude Code

The server ships as a single static binary. Build it from the repo:

```sh
cd mcp && go build -o ~/bin/backhog-mcp ./cmd/backhog-mcp
```

Then register it (token and URL come from your environment):

```sh
claude mcp add backhog \
  --env BACKHOG_URL=http://localhost:8080 \
  --env BACKHOG_TOKEN=bh_… \
  -- ~/bin/backhog-mcp
```

Now, mid-book, you can ask things like *"what did the detective find in the
pantry?"* or *"who is Iris so far?"* — and get answers grounded only in what
you have read, with links you can click to check. Say *"quiz me"* and the
same grounding becomes a comprehension check: questions from the chapters
you've read, graded against the passages that answer them, the honest score
recorded when you want it counted.

## Claude Desktop

Add to `claude_desktop_config.json` (Settings → Developer → Edit Config):

```json
{
  "mcpServers": {
    "backhog": {
      "command": "/Users/you/bin/backhog-mcp",
      "env": {
        "BACKHOG_URL": "http://localhost:8080",
        "BACKHOG_TOKEN": "bh_…"
      }
    }
  }
}
```

## Docker (streamable HTTP)

The compose file carries the server behind the `mcp` profile, for clients
that connect to a remote MCP endpoint instead of spawning a process:

```sh
BACKHOG_TOKEN=bh_… BACKHOG_PUBLIC_URL=http://your-backhog.example \
  docker compose --profile mcp up -d mcp
```

It serves streamable HTTP on `/mcp` (port 8081) with `/healthz` beside it.
`BACKHOG_PUBLIC_URL` sets the origin the citation deep links point at —
use the URL you open in a browser, which may differ from the API's
container-internal address.

## A local model

Any MCP-capable local client works the same way: point it at the binary
(stdio) or the HTTP endpoint, give it the same two variables, and the text
goes to your model and nowhere else. If the model runs on your machine,
nothing leaves your network — see the privacy note.

## Privacy

The server itself holds no model and no secrets beyond your token, and
talks only to your backhog. But **book text goes to whichever model you
connect it to**: Claude answers mean the text transits Anthropic's servers
under your account; a local model (Ollama, llama.cpp, …) keeps it entirely
on your machine. Spoiler clamping applies either way — an assistant that
has only been shown your first three chapters cannot quote the fourth.

## Configuration reference

| Variable | Flag | Meaning |
| --- | --- | --- |
| `BACKHOG_URL` | `-backhog-url` | Backhog origin (an `/api` suffix is tolerated). |
| `BACKHOG_TOKEN` | `-backhog-token` | Personal token, `bh_…` — read-only, or read + `quiz:write` to let it record quiz results. |
| `BACKHOG_PUBLIC_URL` | `-public-url` | Origin for deep links when it differs from `BACKHOG_URL`. |
| `BACKHOG_MCP_HTTP` | `-http addr` | Serve streamable HTTP on `addr` instead of stdio. |
| — | `-healthcheck` | Probe `/healthz` once and exit (container healthchecks). |
