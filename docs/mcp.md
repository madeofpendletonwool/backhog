# Asking your books with an AI assistant — backhog-mcp

Backhog does not embed an AI. Instead it ships **backhog-mcp**, a small MCP
(Model Context Protocol) server that turns your library into tools your own
assistant can use: Claude Code, Claude Desktop, any MCP-capable client, or a
local model for users who want the text to never leave their network. No
model runs inside backhog or this server — it is an adapter.

The spoiler safety is a server guarantee, not a model promise: the personal
API token this server holds is read-only **and** defaults to
`until=position`, so every tool result is clamped to *your reading
position*. An assistant using these tools cannot read ahead of you unless
you explicitly ask it to (`include_spoilers` on a tool call).

## What your assistant sees

| Tool | What it does |
| --- | --- |
| `list_books(status?)` | Your library with reading status and progress percent. |
| `get_reading_position(book)` | Chapter, percent and character offset — the "as of" of every answer. |
| `list_chapters(book)` | Chapters; past your position they come back locked (title only). |
| `read_text(book, from?, to?, chapter?)` | The book's prose, one bounded page at a time (`next_from` to continue). |
| `search_book(book, query)` | Phrase search with exact quotes and citation links. |
| `search_library(query)` | Library-wide search (needs a backhog with MAD-470). |
| `get_passage(book, char_start, char_end)` | The exact text at a character range — quote verification. |
| `find_mentions(book, name)` | Every place a name has come up so far (needs MAD-670). |

Every result carries the effective `bound` (where your reading stood) and
peek deep links (`…/read?offset=N&peek=1`) that jump your reader to a
passage without moving your saved position. `search_library` and
`find_mentions` return a clear "not available on this backhog yet" error
until the library search and name index land.

## Prerequisites

1. A backhog account with books in your library.
2. A personal API token: **Settings → API tokens → create**, leave it
   read-only (`books:read`), copy the `bh_…` secret when it is shown — it
   is displayed exactly once.

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
you have read, with links you can click to check.

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
| `BACKHOG_TOKEN` | `-backhog-token` | Personal read-only token, `bh_…`. |
| `BACKHOG_PUBLIC_URL` | `-public-url` | Origin for deep links when it differs from `BACKHOG_URL`. |
| `BACKHOG_MCP_HTTP` | `-http addr` | Serve streamable HTTP on `addr` instead of stdio. |
| — | `-healthcheck` | Probe `/healthz` once and exit (container healthchecks). |
