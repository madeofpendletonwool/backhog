package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"

	"github.com/collinpendleton/backhog/api/booktext"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// The claims store (MAD-466, the optional last stage of the knowledge
// layer): structured statements an outside extractor grounded in the
// canonical text, written in through the batch import and served back
// under the same spoiler clamp every read path speaks. Backhog stores,
// validates and serves; it never produces. The door on the way in is the
// deterministic cite-or-drop check — the quoted span must match the
// canonical text at its offsets, inside one chapter, and a reveal never
// precedes its evidence — and every failure is rejected per item, never
// fixed up. On the way out the clamp works on statements the way it works
// on text: a claim exists only once its evidence has been read, and a
// version revealed past the position does not exist yet.
//
// The default clamp is the position for *every* caller, the name index's
// rule: nothing here moves a position, and the whole point of consulting
// claims mid-book is "what do I know so far".

// Caps that keep one import honest: a batch is an extractor's run, not a
// firehose, and a quote is an anchor, not an appendix.
const (
	claimsMaxBatch     = 200
	claimsMaxVersions  = 20
	claimsMaxStatement = 1000
	claimsMaxQuote     = 4000
	claimsMaxTriple    = 200
	claimsMaxSource    = 64
	claimsMaxSourceVer = 128
	claimsMaxEntities  = 200
	claimsMaxAliases   = 50
	claimsDefaultLimit = 50
	claimsMaxLimit     = 200
)

// claimImportItem is one claim of the batch: the statement, its optional
// triple, and the evidence that grounds it. Versions ride nested — a
// later truth of the same claim, revealed where the book says so.
type claimImportItem struct {
	Statement     string             `json:"statement"`
	Subject       *string            `json:"subject"`
	Predicate     *string            `json:"predicate"`
	Object        *string            `json:"object"`
	CharStart     int                `json:"char_start"`
	CharEnd       int                `json:"char_end"`
	Quote         string             `json:"quote"`
	Source        string             `json:"source"`
	SourceVersion string             `json:"source_version"`
	Versions      []claimVersionItem `json:"versions"`
}

type claimVersionItem struct {
	Statement string `json:"statement"`
	CharStart int    `json:"char_start"`
	CharEnd   int    `json:"char_end"`
	Quote     string `json:"quote"`
	// RevealOffset is where this truth arrives. It defaults to the
	// evidence's own start and may never precede the evidence.
	RevealOffset  *int   `json:"reveal_offset"`
	Source        string `json:"source"`
	SourceVersion string `json:"source_version"`
}

// entityImportItem registers a person, place or thing the claims are
// about, with the aliases it answers to.
type entityImportItem struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind"`
	Aliases []string `json:"aliases"`
}

type claimsImportRequest struct {
	Source        string             `json:"source"`
	SourceVersion string             `json:"source_version"`
	Claims        []claimImportItem  `json:"claims"`
	Entities      []entityImportItem `json:"entities"`
}

// claimImportResult is one item's verdict: stored, with the claim's id so
// a later run can speak of it, or rejected with the reason. Claims are
// indexed from 0; entities follow after them.
type claimImportResult struct {
	Index   int    `json:"index"`
	Status  string `json:"status"`
	ClaimID string `json:"claim_id,omitempty"`
	Error   string `json:"error,omitempty"`
}

// chapterDigest hashes one chapter's canonical slice — the staleness key.
// A re-ingest that changes a chapter changes its digest, and the claims
// anchored against the old text go quiet until an extractor re-anchors
// them against the new one.
func chapterDigest(text string, start, end int) string {
	sum := sha256.Sum256([]byte(text[start:end]))
	return hex.EncodeToString(sum[:])
}

// claimField trims one free-text field and reports whether it fits its
// slot. Blank or over-long both fail: an extractor that cannot say it in
// the slot has not said it.
func claimField(v string, max int) (string, bool) {
	v = strings.TrimSpace(v)
	if v == "" || len(v) > max {
		return "", false
	}
	return v, true
}

// handleImportBookClaims answers POST /api/books/{entryID}/claims: the
// batch import every external extractor writes through. A cookie session
// writes like any library write; a token needs the claims:write scope,
// and every item still passes the same deterministic checks either way.
func (s *Server) handleImportBookClaims(w http.ResponseWriter, r *http.Request) {
	userID, entryID, bookID, _, ok := s.bookEntryOwned(w, r)
	if !ok {
		return
	}
	et, ok := s.ensureBookText(w, r)
	if !ok {
		return
	}

	var body claimsImportRequest
	if err := decodeMax(r, &body, 4<<20); err != nil {
		fail(w, err)
		return
	}
	if len(body.Claims) == 0 && len(body.Entities) == 0 {
		fail(w, errorf(http.StatusBadRequest, "nothing to import — send claims or entities"))
		return
	}
	if len(body.Claims) > claimsMaxBatch {
		fail(w, errorf(http.StatusBadRequest,
			"a batch carries at most "+strconv.Itoa(claimsMaxBatch)+" claims — split the run"))
		return
	}
	if len(body.Entities) > claimsMaxEntities {
		fail(w, errorf(http.StatusBadRequest,
			"a batch registers at most "+strconv.Itoa(claimsMaxEntities)+" entities"))
		return
	}

	chapters, err := s.store.ListEpubChapters(r.Context(), et.ID)
	if err != nil {
		fail(w, err)
		return
	}
	text, err := s.epubs.ReadText(r.Context(), et, 0, et.CharCount)
	if err != nil {
		fail(w, errorf(http.StatusInternalServerError, "could not read this ebook's text"))
		return
	}

	// Validate first, store second: an item that fails the door costs its
	// slot in the report and nothing else — its siblings land.
	results := make([]claimImportResult, 0, len(body.Claims)+len(body.Entities))
	claims := make([]models.BookClaim, 0, len(body.Claims))
	storedAt := make([]int, 0, len(body.Claims)) // result index of each stored claim
	for i, item := range body.Claims {
		claim, reason := validateClaimItem(item, body.Source, body.SourceVersion, chapters, text, et.CharCount)
		results = append(results, claimImportResult{Index: i, Status: "rejected", Error: reason})
		if reason != "" {
			continue
		}
		claims = append(claims, claim)
		storedAt = append(storedAt, len(results)-1)
		results[len(results)-1] = claimImportResult{Index: i, Status: "stored"}
	}

	entities := make([]models.BookEntity, 0, len(body.Entities))
	for i, item := range body.Entities {
		entity, reason := validateEntityItem(item)
		results = append(results, claimImportResult{
			Index: len(body.Claims) + i, Status: "rejected", Error: reason,
		})
		if reason != "" {
			continue
		}
		entity.UserID, entity.BookID = userID, bookID
		entities = append(entities, entity)
		results[len(results)-1] = claimImportResult{Index: len(body.Claims) + i, Status: "stored"}
	}

	if len(claims) > 0 || len(entities) > 0 {
		if err := s.store.ImportClaims(r.Context(), userID, entryID, claims, entities); err != nil {
			fail(w, err)
			return
		}
		for j, at := range storedAt {
			results[at].ClaimID = claims[j].ID
		}
	}

	rejected := 0
	for _, res := range results {
		if res.Status == "rejected" {
			rejected++
		}
	}
	status := http.StatusCreated
	if rejected > 0 && len(claims) == 0 && len(entities) == 0 {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]any{
		"stored":   len(claims),
		"entities": len(entities),
		"rejected": rejected,
		"results":  results,
	})
}

// validateEntityItem normalizes one entity registration. The row keeps
// the extractor's own spelling; identity at read time is the fold.
func validateEntityItem(item entityImportItem) (models.BookEntity, string) {
	name, ok := claimField(item.Name, claimsMaxTriple)
	if !ok {
		return models.BookEntity{}, "an entity needs a name"
	}
	if len(item.Aliases) > claimsMaxAliases {
		return models.BookEntity{}, "an entity carries at most " + strconv.Itoa(claimsMaxAliases) + " aliases"
	}
	kind, _ := claimField(item.Kind, claimsMaxTriple)
	aliases := make([]string, 0, len(item.Aliases))
	for _, a := range item.Aliases {
		a, ok := claimField(a, claimsMaxTriple)
		if !ok {
			return models.BookEntity{}, "an empty alias names nothing"
		}
		aliases = append(aliases, a)
	}
	return models.BookEntity{Name: name, Kind: kind, Aliases: aliases}, ""
}

// validateClaimItem runs the cite-or-drop door on one claim: shape, span,
// quote, chapter containment, reveal order. The canonical text and
// chapter list come from the current parse — the same truth the read
// paths serve. A claim that passes is fully specified: digests computed,
// sources resolved, versions attached.
func validateClaimItem(item claimImportItem, bodySource, bodySourceVersion string,
	chapters []models.EpubChapter, text string, charCount int) (models.BookClaim, string) {

	statement, ok := claimField(item.Statement, claimsMaxStatement)
	if !ok {
		return models.BookClaim{}, "a claim needs a statement"
	}
	quote, ok := claimField(item.Quote, claimsMaxQuote)
	if !ok {
		return models.BookClaim{}, "a claim needs its quoted evidence"
	}
	source, srcOK := claimField(item.Source, claimsMaxSource)
	if !srcOK {
		source, srcOK = claimField(bodySource, claimsMaxSource)
	}
	if !srcOK {
		return models.BookClaim{}, "a claim needs a source — name the extractor"
	}
	sourceVersion, _ := claimField(item.SourceVersion, claimsMaxSourceVer)
	if sourceVersion == "" {
		sourceVersion, _ = claimField(bodySourceVersion, claimsMaxSourceVer)
	}

	chapterIndex, chapterHash, reason := anchorClaimSpan(item.CharStart, item.CharEnd, chapters, text, charCount)
	if reason != "" {
		return models.BookClaim{}, reason
	}
	if booktext.Normalize(quote) != booktext.Normalize(text[item.CharStart:item.CharEnd]) {
		return models.BookClaim{}, "the quote does not match the canonical text at those offsets"
	}

	claim := models.BookClaim{
		Statement:     statement,
		Quote:         quote,
		CharStart:     item.CharStart,
		CharEnd:       item.CharEnd,
		ChapterIndex:  chapterIndex,
		ChapterHash:   chapterHash,
		Source:        source,
		SourceVersion: sourceVersion,
	}
	for _, field := range []struct {
		name string
		v    *string
		set  func(string)
	}{
		{"subject", item.Subject, func(v string) { claim.Subject = &v }},
		{"predicate", item.Predicate, func(v string) { claim.Predicate = &v }},
		{"object", item.Object, func(v string) { claim.Object = &v }},
	} {
		if field.v == nil {
			continue
		}
		v, ok := claimField(*field.v, claimsMaxTriple)
		if !ok {
			return models.BookClaim{}, "the " + field.name + " is blank or too long"
		}
		field.set(v)
	}

	if len(item.Versions) > claimsMaxVersions {
		return models.BookClaim{}, "a claim carries at most " + strconv.Itoa(claimsMaxVersions) + " versions"
	}
	for _, v := range item.Versions {
		version, reason := validateVersionItem(v, source, sourceVersion, chapters, text, charCount)
		if reason != "" {
			return models.BookClaim{}, "version: " + reason
		}
		claim.Versions = append(claim.Versions, version)
	}
	return claim, ""
}

func validateVersionItem(item claimVersionItem, claimSource, claimSourceVersion string,
	chapters []models.EpubChapter, text string, charCount int) (models.ClaimVersion, string) {

	statement, ok := claimField(item.Statement, claimsMaxStatement)
	if !ok {
		return models.ClaimVersion{}, "a version needs a statement"
	}
	quote, ok := claimField(item.Quote, claimsMaxQuote)
	if !ok {
		return models.ClaimVersion{}, "a version needs its quoted evidence"
	}
	chapterIndex, chapterHash, reason := anchorClaimSpan(item.CharStart, item.CharEnd, chapters, text, charCount)
	if reason != "" {
		return models.ClaimVersion{}, reason
	}
	if booktext.Normalize(quote) != booktext.Normalize(text[item.CharStart:item.CharEnd]) {
		return models.ClaimVersion{}, "the quote does not match the canonical text at those offsets"
	}

	reveal := item.CharStart
	if item.RevealOffset != nil {
		reveal = *item.RevealOffset
	}
	if reveal < item.CharStart || reveal > charCount {
		return models.ClaimVersion{}, "a reveal never precedes its evidence nor runs past the book"
	}
	source, ok := claimField(item.Source, claimsMaxSource)
	if !ok {
		source = claimSource
	}
	sourceVersion, _ := claimField(item.SourceVersion, claimsMaxSourceVer)
	if sourceVersion == "" {
		sourceVersion = claimSourceVersion
	}
	return models.ClaimVersion{
		Statement:     statement,
		Quote:         quote,
		CharStart:     item.CharStart,
		CharEnd:       item.CharEnd,
		ChapterIndex:  chapterIndex,
		ChapterHash:   chapterHash,
		RevealOffset:  reveal,
		Source:        source,
		SourceVersion: sourceVersion,
	}, ""
}

// anchorClaimSpan places one evidence span: inside the text, inside one
// chapter, and answered with that chapter's 1-based index and digest.
func anchorClaimSpan(start, end int, chapters []models.EpubChapter, text string, charCount int) (int, string, string) {
	if start < 0 || end <= start {
		return 0, "", "the evidence span is empty or inverted"
	}
	if end > charCount {
		return 0, "", "the evidence span runs past the end of the text"
	}
	for i, ch := range chapters {
		if ch.CharEnd <= ch.CharStart {
			continue // an empty chapter holds nothing to cite
		}
		if start >= ch.CharStart && start < ch.CharEnd {
			if end > ch.CharEnd {
				return 0, "", "the evidence span crosses a chapter boundary — anchor it inside one chapter"
			}
			return i + 1, chapterDigest(text, ch.CharStart, ch.CharEnd), ""
		}
	}
	return 0, "", "no chapter holds the evidence span"
}

// effectiveClaim is one claim as it stands at a bound: which version's
// words are true, and the evidence those words stand on.
type effectiveClaim struct {
	claim   *models.BookClaim
	version *models.ClaimVersion
}

func (e *effectiveClaim) statement() string {
	if e.version != nil {
		return e.version.Statement
	}
	return e.claim.Statement
}

func (e *effectiveClaim) quote() string {
	if e.version != nil {
		return e.version.Quote
	}
	return e.claim.Quote
}

func (e *effectiveClaim) span() (int, int) {
	if e.version != nil {
		return e.version.CharStart, e.version.CharEnd
	}
	return e.claim.CharStart, e.claim.CharEnd
}

func (e *effectiveClaim) chapterKey() (int, string) {
	if e.version != nil {
		return e.version.ChapterIndex, e.version.ChapterHash
	}
	return e.claim.ChapterIndex, e.claim.ChapterHash
}

func (e *effectiveClaim) reveal() int {
	if e.version != nil {
		return e.version.RevealOffset
	}
	return e.claim.CharStart
}

// effectiveClaims resolves every claim against a bound and the current
// parse. The rules, all of them spoiler safety:
//
//   - a truth exists only once its evidence has been read in full — a
//     claim whose quote straddles the bound is as absent as one past it;
//   - among the truths a reader may hold, the one they hold is the latest
//     revealed at or before their position: a version revealed past the
//     bound does not exist yet, and nothing it says is served either;
//   - a claim whose evidence chapter changed since it was anchored (a
//     re-ingest) is stale and hidden — a citation nobody can verify is
//     worth nothing — until an extractor re-imports it against the new
//     text. The check is the chapter digest, plus the cheap geometric
//     fact that the chapter still holds the offset.
func effectiveClaims(all []models.BookClaim, chapters []models.EpubChapter, text string,
	bound readBound) ([]effectiveClaim, int) {

	digests := make(map[int]string, len(chapters))
	for i, ch := range chapters {
		if ch.CharEnd > ch.CharStart {
			digests[i+1] = chapterDigest(text, ch.CharStart, ch.CharEnd)
		}
	}
	holds := func(index, start int) bool {
		if index < 1 || index > len(chapters) {
			return false
		}
		ch := chapters[index-1]
		return start >= ch.CharStart && start < ch.CharEnd
	}

	out := make([]effectiveClaim, 0, len(all))
	stale := 0
	for i := range all {
		c := &all[i]
		if c.CharEnd > bound.offset {
			continue // evidence not fully read
		}
		best := effectiveClaim{claim: c}
		for vi := range c.Versions {
			v := &c.Versions[vi]
			if v.CharEnd > bound.offset || v.RevealOffset > bound.offset {
				continue
			}
			if v.RevealOffset > best.reveal() {
				best = effectiveClaim{claim: c, version: v}
			}
		}
		chapterIndex, hash := best.chapterKey()
		start, _ := best.span()
		if !holds(chapterIndex, start) || digests[chapterIndex] != hash {
			stale++
			continue
		}
		out = append(out, best)
	}
	return out, stale
}

// entityRef is the cast member a claim's triple names, resolved through
// the shelf's own entity table by folded name or alias.
type entityRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind,omitempty"`
}

// claimView is one served truth: the statement, the triple that named it,
// the quote and where it sits, and where its truth was revealed.
type claimView struct {
	ID            string         `json:"id"`
	Statement     string         `json:"statement"`
	Subject       *string        `json:"subject,omitempty"`
	Predicate     *string        `json:"predicate,omitempty"`
	Object        *string        `json:"object,omitempty"`
	SubjectEntity *entityRef     `json:"subject_entity,omitempty"`
	ObjectEntity  *entityRef     `json:"object_entity,omitempty"`
	Quote         string         `json:"quote"`
	Provenance    provenanceView `json:"provenance"`
	RevealOffset  int            `json:"reveal_offset"`
	Superseded    bool           `json:"superseded"`
	Source        string         `json:"source"`
	SourceVersion string         `json:"source_version,omitempty"`
	VersionCount  int            `json:"versions"`
}

type claimsResponse struct {
	Claims      []claimView `json:"claims"`
	Total       int         `json:"total"`
	StaleHidden int         `json:"stale_hidden"`
	Bound       boundView   `json:"bound"`
}

// handleBookClaims serves the store: GET /api/books/{entryID}/claims?until=
// &entity=&limit=. Every truth is the latest one the reader has the
// evidence and the reveal for; every quote cites a chapter, offsets and a
// peek link, so an answer built on claims can be checked like one built
// on text. `entity=` filters to one cast member by folded name or alias.
func (s *Server) handleBookClaims(w http.ResponseWriter, r *http.Request) {
	userID, entryID, bookID, ok := s.bookEntry(w, r)
	if !ok {
		return
	}
	et, ok := s.ensureBookText(w, r)
	if !ok {
		return
	}
	limit := claimsDefaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > claimsMaxLimit {
			fail(w, errorf(http.StatusBadRequest,
				"limit must be between 1 and "+strconv.Itoa(claimsMaxLimit)))
			return
		}
		limit = n
	}

	bound, ok := s.resolveReadBoundDefault(w, r, userID, entryID, bookID, et.CharCount, untilPosition)
	if !ok {
		return
	}
	visible, stale, chapters, ok := s.visibleClaims(w, r, userID, entryID, et, bound)
	if !ok {
		return
	}
	ix, err := s.claimEntityIndex(r.Context(), userID, bookID)
	if err != nil {
		fail(w, err)
		return
	}

	views := make([]claimView, 0, len(visible))
	for i := range visible {
		views = append(views, renderClaim(&visible[i], ix, chapters))
	}
	if name := strings.TrimSpace(r.URL.Query().Get("entity")); name != "" {
		views = filterClaimsByEntity(views, ix, name)
	}
	total := len(views)
	if len(views) > limit {
		views = views[:limit]
	}
	writeJSON(w, http.StatusOK, claimsResponse{
		Claims: views, Total: total, StaleHidden: stale, Bound: bound.view,
	})
}

// visibleClaims loads the entry's claims and resolves them against a
// bound over the current parse: the shared front half of the claims and
// entities reads. The chapter list rides along for the citations.
func (s *Server) visibleClaims(w http.ResponseWriter, r *http.Request,
	userID, entryID string, et models.EpubText, bound readBound) ([]effectiveClaim, int, []models.EpubChapter, bool) {

	all, err := s.store.ListBookClaims(r.Context(), userID, entryID)
	if err != nil {
		fail(w, err)
		return nil, 0, nil, false
	}
	chapters, err := s.store.ListEpubChapters(r.Context(), et.ID)
	if err != nil {
		fail(w, err)
		return nil, 0, nil, false
	}
	text, err := s.epubs.ReadText(r.Context(), et, 0, et.CharCount)
	if err != nil {
		fail(w, errorf(http.StatusInternalServerError, "could not read this ebook's text"))
		return nil, 0, nil, false
	}
	visible, stale := effectiveClaims(all, chapters, text, bound)
	return visible, stale, chapters, true
}

// claimEntityIndex maps folded names and aliases to the entity they name
// — the deterministic resolver the claims and the cast list share.
type claimIndex struct {
	byKey map[string]models.BookEntity
}

func (ix *claimIndex) find(name string) *models.BookEntity {
	if e, ok := ix.byKey[booktext.Normalize(name)]; ok {
		return &e
	}
	return nil
}

func (s *Server) claimEntityIndex(ctx context.Context, userID, bookID string) (*claimIndex, error) {
	entities, err := s.store.ListBookEntities(ctx, userID, bookID)
	if err != nil {
		return nil, err
	}
	ix := &claimIndex{byKey: make(map[string]models.BookEntity, len(entities))}
	for i := range entities {
		e := entities[i]
		ix.byKey[booktext.Normalize(e.Name)] = e
		for _, a := range e.Aliases {
			if _, exists := ix.byKey[booktext.Normalize(a)]; !exists {
				ix.byKey[booktext.Normalize(a)] = e
			}
		}
	}
	return ix, nil
}

// renderClaim builds one served truth. Provenance cites the chapter the
// evidence starts in, exactly like every span the arena serves.
func renderClaim(e *effectiveClaim, ix *claimIndex, chapters []models.EpubChapter) claimView {
	start, end := e.span()
	v := claimView{
		ID:           e.claim.ID,
		Statement:    e.statement(),
		Quote:        e.quote(),
		Provenance:   spanProvenance(e.claim.Entry, chapters, start, end),
		RevealOffset: e.reveal(),
		Superseded:   e.version != nil,
		Source:       e.claim.Source,
		VersionCount: len(e.claim.Versions),
	}
	if e.version != nil {
		v.Source = e.version.Source
		v.SourceVersion = e.version.SourceVersion
	} else {
		v.SourceVersion = e.claim.SourceVersion
	}
	if e.claim.Subject != nil {
		subject := *e.claim.Subject
		v.Subject = &subject
		if entity := ix.find(subject); entity != nil {
			v.SubjectEntity = &entityRef{ID: entity.ID, Name: entity.Name, Kind: entity.Kind}
		}
	}
	if e.claim.Predicate != nil {
		predicate := *e.claim.Predicate
		v.Predicate = &predicate
	}
	if e.claim.Object != nil {
		object := *e.claim.Object
		v.Object = &object
		if entity := ix.find(object); entity != nil {
			v.ObjectEntity = &entityRef{ID: entity.ID, Name: entity.Name, Kind: entity.Kind}
		}
	}
	return v
}

// filterClaimsByEntity keeps the claims whose subject or object names one
// cast member — the entity as registered (by id), or by folded name or
// alias when the shelf has not registered it at all.
func filterClaimsByEntity(views []claimView, ix *claimIndex, name string) []claimView {
	key := booktext.Normalize(name)
	entity := ix.find(name)
	out := make([]claimView, 0, len(views))
	for _, v := range views {
		if matchesEntity(v.Subject, v.SubjectEntity, entity, key) ||
			matchesEntity(v.Object, v.ObjectEntity, entity, key) {
			out = append(out, v)
		}
	}
	return out
}

func matchesEntity(part *string, ref *entityRef, entity *models.BookEntity, key string) bool {
	if part == nil {
		return false
	}
	if entity != nil && ref != nil && ref.ID == entity.ID {
		return true
	}
	if entity == nil && ref == nil && booktext.Normalize(*part) == key {
		return true
	}
	return false
}

// entityFirstSeen cites where an entity's story with the reader began:
// the earliest evidence among the claims that name it.
type entityFirstSeen struct {
	CharStart int          `json:"char_start"`
	Chapter   *chapterView `json:"chapter"`
	Percent   float64      `json:"percent"`
	DeepLink  string       `json:"deep_link"`
}

type entityView struct {
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name"`
	Kind      string          `json:"kind,omitempty"`
	Aliases   []string        `json:"aliases,omitempty"`
	Claims    int             `json:"claims"`
	FirstSeen entityFirstSeen `json:"first_seen"`
}

type entitiesResponse struct {
	Entities []entityView `json:"entities"`
	Bound    boundView    `json:"bound"`
}

// handleBookEntities serves the cast the claims speak about:
// GET /api/books/{entryID}/entities?until=&name=. An entity exists for a
// reader only when a claim they may see names it — an entity whose every
// mention sits past the position does not exist yet, the name index's
// rule. `name=` filters to one entity by folded name or alias, the shape
// the MCP get_entity tool resolves through.
func (s *Server) handleBookEntities(w http.ResponseWriter, r *http.Request) {
	userID, entryID, bookID, ok := s.bookEntry(w, r)
	if !ok {
		return
	}
	et, ok := s.ensureBookText(w, r)
	if !ok {
		return
	}
	bound, ok := s.resolveReadBoundDefault(w, r, userID, entryID, bookID, et.CharCount, untilPosition)
	if !ok {
		return
	}
	visible, _, _, ok := s.visibleClaims(w, r, userID, entryID, et, bound)
	if !ok {
		return
	}
	ix, err := s.claimEntityIndex(r.Context(), userID, bookID)
	if err != nil {
		fail(w, err)
		return
	}

	// One pass: tally every name the visible truths speak of. Registered
	// entities tally under their row (whichever spelling the claim used —
	// the fold and the alias table decide identity); bare names stand for
	// themselves.
	type tally struct {
		entity models.BookEntity
		count  int
		first  int
	}
	tallies := make(map[string]*tally)
	touch := func(name string, start int) {
		key := booktext.Normalize(name)
		t, seen := tallies[key]
		if !seen {
			t = &tally{first: start}
			if e := ix.find(name); e != nil {
				t.entity = *e
			}
			tallies[key] = t
		}
		t.count++
		if start < t.first {
			t.first = start
		}
	}
	for i := range visible {
		e := &visible[i]
		start, _ := e.span()
		if e.claim.Subject != nil {
			touch(*e.claim.Subject, start)
		}
		if e.claim.Object != nil {
			touch(*e.claim.Object, start)
		}
	}

	views := make([]entityView, 0, len(tallies))
	for _, t := range tallies {
		v := entityView{
			Claims:    t.count,
			FirstSeen: entityFirstSeen{CharStart: t.first},
		}
		if t.entity.ID != "" {
			v.ID = t.entity.ID
			v.Name = t.entity.Name
			v.Kind = t.entity.Kind
			v.Aliases = t.entity.Aliases
		} else {
			// A bare name: the fold is all the identity it has, so the
			// view shows the folded spelling rather than one claim's
			// capitals over another's.
			for key := range tallies {
				if tallies[key] == t {
					v.Name = key
					break
				}
			}
		}
		views = append(views, v)
	}
	sortEntityViews(views)

	if name := strings.TrimSpace(r.URL.Query().Get("name")); name != "" {
		key := booktext.Normalize(name)
		filtered := views[:0]
		for _, v := range views {
			if booktext.Normalize(v.Name) == key {
				filtered = append(filtered, v)
				continue
			}
			for _, a := range v.Aliases {
				if booktext.Normalize(a) == key {
					filtered = append(filtered, v)
					break
				}
			}
		}
		views = filtered
	}

	// Place the citations: chapter, percent and the peek link that lands
	// a reader on the first evidence.
	bookViews, err := s.searchViewsFor(r.Context(), userID, entryID, bookID)
	if err != nil {
		fail(w, err)
		return
	}
	for i := range views {
		views[i].FirstSeen.Chapter = chapterAt(bookViews.chapters, views[i].FirstSeen.CharStart)
		views[i].FirstSeen.Percent = percentAtOffset(views[i].FirstSeen.CharStart, bookViews)
		views[i].FirstSeen.DeepLink = deepLink(entryID, views[i].FirstSeen.CharStart)
	}
	writeJSON(w, http.StatusOK, entitiesResponse{Entities: views, Bound: bound.view})
}

func sortEntityViews(views []entityView) {
	for i := 1; i < len(views); i++ {
		for j := i; j > 0 && strings.ToLower(views[j].Name) < strings.ToLower(views[j-1].Name); j-- {
			views[j], views[j-1] = views[j-1], views[j]
		}
	}
}
