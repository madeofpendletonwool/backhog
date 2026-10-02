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
	addQuizMe(srv)
	addDiscussionPrep(srv)
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
	srv.AddPrompt(&mcp.Prompt{
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
	srv.AddPrompt(&mcp.Prompt{
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
	srv.AddPrompt(&mcp.Prompt{
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

// addQuizMe (MAD-471): the comprehension half of the knowledge layer. The
// model writes questions from what the reader has actually read, withholds
// the answers, grades against cited passages, and reports the honest count
// — backhog's AI-free endpoint does the rest.
func addQuizMe(srv *mcp.Server) {
	srv.AddPrompt(&mcp.Prompt{
		Name:        "quiz_me",
		Title:       "Quiz me",
		Description: "A quiz over the chapters actually read — questions first, answers after, every answer in the key cited with a passage link, and the honest score reported at the end.",
		Arguments: []*mcp.PromptArgument{{
			Name: "book", Required: true,
			Description: "The book to be quizzed on, by title as the user says it.",
		}, {
			Name:        "chapters",
			Description: "Optional: which chapters to cover — a number like 4, a range like 4-6, or \"last 2\". Default: the most recently read chapters.",
		}},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		book, err := promptArg(req, "book")
		if err != nil {
			return nil, err
		}
		chapters := promptArgOpt(req, "chapters")
		scope := "the most recently read chapters — the last few the reader has finished, not the whole book"
		if chapters != "" {
			scope = "the chapters the user asked for: \"" + chapters + "\" (read it as a number, a range, or a count from the end; if it names chapters the reader has not reached, clamp to what they have read and say you did)"
		}
		return promptResult(
			"Quiz me on "+book,
			fmt.Sprintf(`The user wants to be quizzed on the book "%s" — a check of what they actually retained, not an exam. The quiz covers %s.

%s

Then:
1. Call get_reading_position — the quiz asks only about text at or before it, by decree of the server.
2. Call list_chapters to see the book's shape and which chapters are still locked. Locked chapters are off the table; treat their titles cautiously too.
3. Read the chapters in scope with read_text (use the chapter number), enough to write questions grounded in specific passages. Follow next_from until you have the material.
4. Write 5 to 8 questions: a mix of recall (what happened, who said it) and inference (why did they, what does it mean for later). Every question must be answerable from the text the reader has read — never from outside knowledge of the work, and never from beyond the reading position. Vary the difficulty; the point is to find what stuck, not to stump.

Present the questions numbered, WITHOUT any answers, and stop. Wait for the user's answers — do not answer for them, do not hint.

When the user answers, grade each one against the text (read_text or get_passage to check before judging):
- for each question: their answer, whether it counts (right, half-right, wrong), and the citation — the quote and its deep_link from the passage that settles it;
- a wrong answer gets the passage that would have answered it, so the quiz teaches on the way out;
- an answer the read text does not settle is not correct just because it sounds plausible — the book is the arbiter, and only the part of it the reader has read.

Then report the score plainly and call record_quiz_result with the honest count: the number of questions you asked, and the number you graded correct (half-right counts only if you said so while grading, and then only as one count or the other — pick one). Never round up. Tell the user you recorded it and what it unlocked; if they would rather not record it, say so before calling — but never record a count you did not grade.

%s`, book, scope, resolveBook, playbookRules))
	})
}

// addDiscussionPrep (MAD-471): the book-club half. Questions grounded in
// cited passages, plus the honest kind no quiz can have — the ones the
// book, so far, refuses to settle.
func addDiscussionPrep(srv *mcp.Server) {
	srv.AddPrompt(&mcp.Prompt{
		Name:        "discussion_prep",
		Title:       "Discussion prep",
		Description: "Book-club questions over what's been read — each grounded in a cited passage, plus the open questions the book hasn't settled yet.",
		Arguments: []*mcp.PromptArgument{{
			Name: "book", Required: true,
			Description: "The book to discuss, by title as the user says it.",
		}, {
			Name:        "section",
			Description: "Optional: what to focus the questions on — a chapter number or title, a character, or a theme.",
		}},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		book, err := promptArg(req, "book")
		if err != nil {
			return nil, err
		}
		section := promptArgOpt(req, "section")
		focus := "the whole read-so-far"
		if section != "" {
			focus = "the angle the user named: \"" + section + "\" — a chapter, a character, or a theme; find the passages that speak to it with search_book, read_text and find_mentions"
		}
		return promptResult(
			"Discussion prep for "+book,
			fmt.Sprintf(`The user is preparing to talk about the book "%s" — a book club, a reading group, or just a think. Build the question set a good discussion needs, over %s.

%s

Then:
1. Call get_reading_position — the discussion covers what the user has read, full stop. Say where they are so everyone knows the room's "as of".
2. Call list_chapters for the book's shape; locked chapters are ahead of the reader and out of bounds.
3. Gather the material: read_text over the read chapters (lightly for sweep, closely near the position), search_book for the passages that bear on the focus, find_mentions for the people who matter. Every question you write must stand on a passage you actually saw.

Write 8 to 12 discussion questions in two kinds, each labeled:

**Grounded in the text** — questions a passage can open: why a character did the thing at the deep_link; what a choice costs; what the book seems to believe, as shown at the deep_link; what the user would have done in the scene at the deep_link. Each carries its citation. A grounded question asks what the reader makes of the passage — it shows the passage, it does not quiz on it.

**The book doesn't settle this** — the open questions: what the read text raises and pointedly does not answer, where reasonable readers could disagree, what the book seems to be asking the reader to decide. Mark these honestly: they are questions *about* the read text, and the answer is the discussion itself. Never smuggle in something only a later chapter resolves — if the tools would not serve the passage, the question cannot lean on it.

Order them so the discussion travels: start concrete (a scene, a choice), move outward (theme, structure), end on the unsettled ones. Do not answer your own questions — one line of framing at most, then hand them over.

%s`, book, focus, resolveBook, playbookRules))
	})
}
func promptArg(req *mcp.GetPromptRequest, name string) (string, error) {
	v := strings.TrimSpace(req.Params.Arguments[name])
	if v == "" {
		return "", fmt.Errorf("the %q argument is required — say which %s this is for", name, name)
	}
	return v, nil
}

// promptArgOpt reads one optional string argument from a prompts/get call.
func promptArgOpt(req *mcp.GetPromptRequest, name string) string {
	return strings.TrimSpace(req.Params.Arguments[name])
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
