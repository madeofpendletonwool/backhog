package http

import (
	"errors"
	"net/http"

	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// quizResultRequest is the payload a quizzing client reports after a quiz
// (MAD-471): how many questions it asked, how many the reader got right,
// and the chapter span the quiz covered. Backhog stores and counts; it
// never grades or generates — the client's own model did both, and the
// achievements fed by these rows say so in their descriptions.
type quizResultRequest struct {
	Questions int `json:"questions"`
	Correct   int `json:"correct"`
	// ChapterRange is the inclusive 1-based chapter span the quiz covered,
	// optional. Bookkeeping for display, never validated against the spine.
	ChapterRange *quizChapterRange `json:"chapter_range"`
	// Source names the client that gave the quiz; only "mcp" exists today.
	Source string `json:"source"`
}

type quizChapterRange struct {
	From int `json:"from"`
	To   int `json:"to"`
}

// handleRecordQuizResult answers POST /api/books/{entryID}/quiz-results.
//
// The endpoint is deliberately AI-free: no questions pass through it, only
// counts. A cookie session may write it like any library write; a token
// needs the quiz:write scope — the one POST that scope names, enforced in
// the auth middleware before the route is ever resolved. The response is
// the stored row plus any achievements the result unlocked, for the toast.
func (s *Server) handleRecordQuizResult(w http.ResponseWriter, r *http.Request) {
	userID, entryID, _, _, ok := s.bookEntryOwned(w, r)
	if !ok {
		return
	}

	var body quizResultRequest
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	if body.Questions < 1 || body.Questions > 100 {
		fail(w, errorf(http.StatusBadRequest, "questions must be between 1 and 100 — that is a log, not a quiz"))
		return
	}
	if body.Correct < 0 || body.Correct > body.Questions {
		fail(w, errorf(http.StatusBadRequest, "correct must be between 0 and the questions asked"))
		return
	}
	if !models.ValidQuizSource(body.Source) {
		fail(w, errorf(http.StatusBadRequest, `source must be "mcp" — name the client that gave the quiz`))
		return
	}

	res := models.QuizResult{
		Questions: body.Questions,
		Correct:   body.Correct,
		Source:    body.Source,
	}
	if body.ChapterRange != nil {
		from, to := body.ChapterRange.From, body.ChapterRange.To
		if from < 1 || to < from {
			fail(w, errorf(http.StatusBadRequest, "chapter_range must run from a chapter to the same or a later one"))
			return
		}
		res.ChapterStart = &from
		res.ChapterEnd = &to
	}

	result, unlocked, err := s.store.RecordQuizResult(r.Context(), userID, entryID, res)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fail(w, errNotFound)
			return
		}
		fail(w, errorf(http.StatusBadRequest, err.Error()))
		return
	}

	unlockPayload := make([]models.AchievementStatus, 0, len(unlocked))
	for _, a := range unlocked {
		unlockPayload = append(unlockPayload, a)
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"quiz_result":  result,
		"achievements": unlockPayload,
	})
}
