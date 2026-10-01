package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The prompts (MAD-469): the returning-reader features, shipped as
// instruction playbooks the client executes with the bounded tools. No
// model runs here either — a prompt is a user message that tells the
// client's own model how to work the tool surface honestly.
//
// Every playbook shares three rules, stated per prompt because a prompt is
// all the model sees of them:
//
//   - cite or drop: every claim carries the deep_link of the passage that
//     grounds it, and any line that cannot be cited is cut, not guessed;
//   - the bound is the truth: the tools stop at the reading position, and
//     "the book doesn't say (so far)" is the right answer past it — never
//     outside knowledge of the work;
//   - no include_spoilers: the clamp exists so recaps cannot spoil, and a
//     recap that opts in has defeated itself.

// addPrompts registers the backhog prompt surface.
func addPrompts(srv *mcp.Server) {
	addPreviouslyOn(srv)
	addCastList(srv)
	addSeriesSoFar(srv)
}

// playbookRules is the contract every prompt's message ends with.
const playbookRules = `Rules you must not bend:
- Never set include_spoilers on any tool call. The reading position is the whole point.
- Every line of your answer must carry the deep_link of the passage that grounds it. Cut any line you cannot cite — an uncited line is a guess, and a guess can spoil.
- The tools stop where the user has read. When they return nothing or truncated text, that is your answer: "the book doesn't say (so far)". Never fill in from what you know about the work, its author, its genre, or its reputation.
- Treat locked chapter titles (list_chapters) cautiously: they sit ahead of the reading position, so do not quote or lean on them.
- Say where the user is (chapter and percent, from the position and its bound) so the recap has an "as of".`

// resolveBook instructs the client to turn a user-supplied book name into
// an entry ID, with the honest fallback when it cannot be done.
const resolveBook = `First resolve the book: call list_books and match the argument against titles (case-insensitive). If several match, prefer the one with status "playing"; if still ambiguous, ask the user which they mean rather than guessing. Use the matched entry_id for every later call.`

func addPreviouslyOn(srv *mcp.Server) {
	srv.AddPrompt( &mcp.Prompt{
		Name:        "previously_on",
		Title:       "Previously on…",
		Description: "A recap of everything the reader has actually read so far — events, people, and open threads — every line cited with a link that jumps right to the passage.",
		Arguments: []*mcp.PromptArgument{{
			Name: "book", Required: true,
			Description: "The book to recap, by title as the user says it.",
		}},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		book, err := promptArg(req, "book")
		if err != nil {
			return nil, err
		}
		return promptResult(
			"Previously on "+book,
			fmt.Sprintf(`The user is about to return to the book "%s" after time away and wants to know where they were and what has happened — like the "previously on" of a series, for a book.

%s

Then:
1. Call get_reading_position. That is the "as of" of the entire recap.
2. Call list_chapters to see the book's shape; chapters marked locked are ahead of the reader.
3. Read what the reader has read with read_text, starting from 0 and following next_from. For a long book, read the early chapters lightly and the most recent chapters closely — the recap should spend its words near where the reader is. Stop when the tools stop: the rest is ahead of the reader, not part of this story yet.
4. Call list_names for the people who exist so far, and find_mentions on the significant ones when a one-line reminder needs grounding.

Then write the recap in four short sections, every line cited with its deep_link:
- **Where you are** — chapter, percent, and one sentence that lands the reader back in the scene (from the last readable text).
- **What has happened** — the events that matter now, in order, a sentence each.
- **Who's who** — one grounded line per person who has appeared so far.
- **Open threads** — the questions the text has planted and not answered yet. Only the ones the reader has actually read the setup of.

%s`, book, resolveBook, playbookRules))
	})
}

func addCastList(srv *mcp.Server) {
	srv.AddPrompt( &mcp.Prompt{
		Name:        "cast_list",
		Title:       "Cast list",
		Description: "Who's who in the book as of the reader's position — a dramatis personae of exactly the people met so far, each with a cited introduction.",
		Arguments: []*mcp.PromptArgument{{
			Name: "book", Required: true,
			Description: "The book whose cast to list, by title as the user says it.",
		}},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		book, err := promptArg(req, "book")
		if err != nil {
			return nil, err
		}
		return promptResult(
			"Cast list for "+book,
			fmt.Sprintf(`The user is mid-book and asking "who are these people again?" Build the cast list of "%s" as of where they've read.

%s

Then:
1. Call get_reading_position — the list stops there, by decree of the server, not you.
2. Call list_names: that is the cast, exactly — every name that has come up so far, with how often.
3. For each significant name (lead with the most-mentioned; skip names the mentions show to be incidental), call find_mentions and read the first appearance's snippet. Ground each one-liner in that introduction: who they are as the book itself presents them, not as general knowledge names them.
4. If the backhog answers that it has no name index, fall back to read_text over the read chapters and pick the names out of the prose, citing the passages you found them in.

Then write the dramatis personae: name, one line, and the deep_link to their first appearance. Group lightly if the book makes the grouping obvious (say, a household, a town), otherwise plain order of appearance. People not yet introduced do not exist yet — they are not on the list, and do not get teased.

%s`, book, resolveBook, playbookRules))
	})
}

func addSeriesSoFar(srv *mcp.Server) {
	srv.AddPrompt( &mcp.Prompt{
		Name:        "series_so_far",
		Title:       "The story so far",
		Description: "Across a whole series: the finished books in order, plus where the reader stands in the one they're on — every line cited, nothing from books not yet begun.",
		Arguments: []*mcp.PromptArgument{{
			Name: "series", Required: true,
			Description: "The series to recap, by name as the user says it.",
		}},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		series, err := promptArg(req, "series")
		if err != nil {
			return nil, err
		}
		return promptResult(
			"The story so far: "+series,
			fmt.Sprintf(`The user has been reading a series and wants the whole story so far: "%s".

First resolve the series: call list_series and match the argument against the names (case-insensitive). If nothing matches, say so and list what their shelf holds — do not improvise a series from outside knowledge. Then:

1. Call get_series with the exact name. It answers the books in reading order, each with its reading status and position. That answer is your scope:
   - finished books ("finished": true) are read whole — their text tools serve everything;
   - the book mid-read serves exactly to its stored position;
   - books not started or dropped contribute their title and nothing else. Say plainly that the user hasn't read them yet, and invent nothing about their contents — not from their titles, not from anything.
2. Walk the in-scope books in order with read_text from 0, following next_from. A paragraph of the essential arc per book is the right size unless the user asked for depth; spend the most words on the book they're currently reading.
3. End with where they stand: the current book, its chapter and percent, and the open threads the read text has planted.

Cross-book continuity is yours to see: a character introduced in one book and reappearing in the next is the same person, and the recap should say so with the citations from both books. Write one section per book in order, every line carrying its book's deep_link.

%s`, series, playbookRules))
	})
}

// promptArg reads one required string argument from a prompts/get call.
func promptArg(req *mcp.GetPromptRequest, name string) (string, error) {
	v := strings.TrimSpace(req.Params.Arguments[name])
	if v == "" {
		return "", fmt.Errorf("the %q argument is required — say which %s this is for", name, name)
	}
	return v, nil
}

// promptResult wraps one instruction message as the prompt's answer: the
// client opens it as the conversation's opening user turn.
func promptResult(description, text string) (*mcp.GetPromptResult, error) {
	return &mcp.GetPromptResult{
		Description: description,
		Messages: []*mcp.PromptMessage{{
			Role: "user",
			Content: &mcp.TextContent{
				Text: text,
			},
		}},
	}, nil
}
