package books

// The name index (MAD-670): candidate proper names lifted out of a book's
// display text by heuristics alone — no model, no truth, just the classic
// back-of-the-book index built deterministically. It answers "where has
// this name come up so far?", and the spoiler clamp does the "so far":
// occurrences past a reading position are dropped at read time, so a
// character introduced in chapter five does not exist for a reader still
// in chapter three.
//
// The signal is capitalization, and capitalization lives in the display
// text — the canonical text is folded to lowercase for matching. But every
// offset the arena stores is canonical, so the extractor walks the two in
// parallel: per block, per field, it accumulates the canonical bytes each
// display field contributes (the same field distribution Normalize's own
// tests assert), which turns any display range into its canonical range
// exactly.
//
// Heuristic quality is fine — it is an index, not a cast list. False
// positives are the reader's to hide (stored per book, never regenerated);
// false negatives are an honest gap, the same trade every concordance
// ever typeset by hand made. When the heuristics move, NamesVersion moves
// with them and the walk re-indexes the library.

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/collinpendleton/backhog/api/booktext"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// NamesVersion pins the extractor: bump it whenever the heuristics below
// change, and every book's pass goes stale for the ensure path and the
// index walk to rebuild. It is independent of ParserVersion — the text and
// its names go stale on their own schedules.
const NamesVersion = "1"

// namesMinOccurrences is the frequency floor: a name seen once is noise,
// however capitalized. Two is the smallest count that says the book means
// it, and it is the whole threshold — the index is cheap to hide a false
// positive from and impossible to guess a real one back.
const namesMinOccurrences = 2

// ExtractNames walks one parse's block index and display text and returns
// every occurrence of every candidate name, in book order. The caller
// (the ingester's indexNames) has both companions in hand; chapters are
// not an input because a chapter is derivable from any offset and a
// stored copy of it would only go stale.
//
// Confidence is two-tier. A capitalized run *not* at a sentence start is
// strong evidence — English only capitalizes there for names — and one
// such sighting promotes the run's folded key to a name for the whole
// book, so "Cecilia sang, and Cecilia danced" indexes both Cecilia
// sightings even though the first opens its sentence. A capitalized run
// that only ever opens sentences is never promoted on its own: that is
// the shape of every common word in the language, which is exactly the
// noise the rule keeps out.
func ExtractNames(index *BlockIndex, display []byte) []models.NameOccurrence {
	type sighting struct {
		occ       models.NameOccurrence
		confident bool
	}
	var seen []sighting
	var run nameRun
	runStartAtSentence := false
	record := func() {
		if occ, ok := run.occurrence(); ok {
			seen = append(seen, sighting{occ: occ, confident: !runStartAtSentence})
		}
		run = nameRun{}
	}

	for _, doc := range index.Documents {
		if len(doc.Blocks) == 0 ||
			doc.DisplayStart < 0 || doc.DisplayEnd < doc.DisplayStart || doc.DisplayEnd > len(display) {
			continue
		}
		blocks := strings.Split(string(display[doc.DisplayStart:doc.DisplayEnd]), "\n")
		for i, block := range blocks {
			if i >= len(doc.Blocks) {
				break
			}
			walkBlock(block, doc.Blocks[i], func(tok nameToken, atSentenceStart bool) {
				switch {
				case tok.kind == tokenHonorific && run.empty():
					// "Mrs." opens a name but is never one alone; it is
					// dropped from the run's identity, so "Mrs Norris"
					// and plain "Norris" are one index entry.
				case tok.kind == tokenName:
					if !run.empty() && atSentenceStart {
						// A sentence boundary closes the run; a capital
						// opening the next sentence starts a new one.
						record()
					}
					if run.empty() {
						// Where the run began decides its confidence; a
						// name that follows an honorific's period is not
						// really a sentence opener.
						runStartAtSentence = atSentenceStart
					}
					run.add(tok)
				case tok.kind == tokenConnective && !run.empty():
					// "Anne of Green Gables": lowercase particles glue a
					// run across its middle, mid-run only.
					run.add(tok)
				default:
					// A sentence boundary, a lowercase word, a number: a
					// paragraph break ends the run too (record below).
					record()
				}
			})
			// Blocks are paragraphs; a run never crosses one.
			record()
		}
	}

	// Promotion: a key becomes a name on its first confident sighting,
	// and then counts everywhere it appears. The frequency floor applies
	// to the total — one sighting of any kind is noise, however placed.
	totals := make(map[string]int, len(seen))
	confident := make(map[string]bool, len(seen))
	for _, s := range seen {
		totals[s.occ.Name]++
		confident[s.occ.Name] = confident[s.occ.Name] || s.confident
	}
	out := make([]models.NameOccurrence, 0, len(seen))
	for _, s := range seen {
		if confident[s.occ.Name] && totals[s.occ.Name] >= namesMinOccurrences {
			out = append(out, s.occ)
		}
	}
	return out
}

// walkBlock tokenizes one display block and reports each field as a
// classified token with its canonical span (absolute: base is the block's
// canonical start). atSentenceStart is the classic rule — the first word
// of the block, or any word following sentence-ending punctuation — which
// is how the ordinary capitalized openers of English prose stay out of the
// index. An honorific's period ("Mr. Darcy") does not close a sentence.
func walkBlock(block string, base int, visit func(tok nameToken, atSentenceStart bool)) {
	canon := 0       // canonical bytes contributed inside this block so far
	contributed := 0 // fields that contributed; fold-emptied fields take no separator
	prevText := ""
	prevEnded := true   // a block opens like a sentence
	prevAbbrev := false // ... unless that sentence-end was an honorific's period

	for _, f := range fieldSpans(block) {
		text := block[f[0]:f[1]]
		norm := booktext.Normalize(text)
		if norm == "" {
			// A lone dash or ellipsis: no canonical bytes, no separator,
			// and no say in where the sentence stands.
			continue
		}
		if contributed > 0 {
			canon++ // Normalize left one space between the two fields
		}
		cs, ce := base+canon, base+canon+len(norm)
		canon += len(norm)
		contributed++

		visit(tokenizeField(text, cs, ce), prevEnded && !prevAbbrev)
		prevText, prevEnded, prevAbbrev = text, endsSentence(text), honorificAbbreviation(text)
	}
	_ = prevText
}

// nameToken is one display field classified for the index.
type nameToken struct {
	kind tokenKind
	// Text is the field's word: surrounding punctuation trimmed, the
	// book's capitals kept, interior apostrophes and hyphens kept.
	text string
	// Canon [start, end) is the field's canonical span, absolute offsets —
	// pulled back past a possessive's "'s" so "Elizabeth's" and
	// "Elizabeth" are one name.
	canonStart, canonEnd int
}

type tokenKind int

const (
	tokenOther tokenKind = iota
	tokenName
	tokenHonorific
	tokenConnective
)

// tokenizeField classifies one display field. The canonical span is the
// field's whole contribution to the canonical text, which for a word is
// exactly the word — the fold keeps letters and digits, drops the rest.
func tokenizeField(field string, canonStart, canonEnd int) nameToken {
	text := trimPunctuation(field)
	if text == "" {
		return nameToken{canonStart: canonStart, canonEnd: canonStart}
	}

	folded := booktext.Normalize(text)
	kind := tokenOther
	switch {
	case honorifics[folded]:
		kind = tokenHonorific
	case connectives[folded]:
		kind = tokenConnective
	case isNameShaped(text):
		kind = tokenName
	}

	ce := canonEnd
	if base, ok := stripPossessive(text); ok && (kind == tokenName || kind == tokenHonorific) {
		// The possessive folds to a bare "s"; cutting it from both the
		// word and the canonical end keeps "Elizabeth's" and "Elizabeth"
		// one name with one span.
		text = base
		if n := len(booktext.Normalize(base)); n <= ce-canonStart {
			ce = canonStart + n
		}
	}
	return nameToken{kind: kind, text: text, canonStart: canonStart, canonEnd: ce}
}

// nameRun is a capitalized run under construction: "Mrs. Norris",
// "Anne of Green Gables", "New York". Honorifics open one but never join
// it; connectives join it but never open it.
type nameRun struct {
	tokens []nameToken
}

func (r *nameRun) empty() bool       { return len(r.tokens) == 0 }
func (r *nameRun) add(tok nameToken) { r.tokens = append(r.tokens, tok) }

// occurrence renders the run into one index entry, or !ok when the run is
// not one: empty, all stop-words, a roman numeral, or dangling a trailing
// connective ("the walls of" was never a name).
func (r nameRun) occurrence() (models.NameOccurrence, bool) {
	tokens := r.tokens
	for len(tokens) > 0 && tokens[len(tokens)-1].kind == tokenConnective {
		tokens = tokens[:len(tokens)-1]
	}
	if len(tokens) == 0 {
		return models.NameOccurrence{}, false
	}
	parts := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		parts = append(parts, tok.text)
	}
	display := strings.Join(parts, " ")
	key := booktext.Normalize(display)
	if nameStopList[key] || isRomanNumeral(key) {
		return models.NameOccurrence{}, false
	}
	return models.NameOccurrence{
		Name:      key,
		Display:   display,
		CharStart: tokens[0].canonStart,
		CharEnd:   tokens[len(tokens)-1].canonEnd,
	}, true
}

// isNameShaped is the capitalization test: an uppercase first letter with
// the rest lowercase ("Elizabeth", "McDonald"), or a short all-caps run
// ("FBI", "NASA") that acronyms and shouting share — the stop-list sorts
// out which is which.
func isNameShaped(word string) bool {
	if len(word) < 2 {
		return false
	}
	first, size := utf8.DecodeRuneInString(word)
	if !unicode.IsUpper(first) {
		return false
	}
	rest := word[size:]
	for _, r := range rest {
		if !unicode.IsUpper(r) {
			// A lowercase letter after the capital: the classic shape.
			return true
		}
	}
	// All caps: an acronym if it is short, shouting otherwise.
	return utf8.RuneCountInString(word) <= 5
}

// trimPunctuation strips the non-letter, non-digit runes from both ends of
// a field — quotes, commas, periods a name carries in prose — while
// leaving interior apostrophes and hyphens ("O'Hara", "Jean-Pierre")
// alone.
func trimPunctuation(field string) string {
	start, end := 0, len(field)
	trim := func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }
	for start < end {
		r, size := utf8.DecodeRuneInString(field[start:])
		if !trim(r) {
			break
		}
		start += size
	}
	for end > start {
		r, size := utf8.DecodeLastRuneInString(field[:end])
		if !trim(r) {
			break
		}
		end -= size
	}
	return field[start:end]
}

// stripPossessive cuts a trailing "'s" — straight or curly — off a word,
// reporting whether there was one to cut.
func stripPossessive(word string) (string, bool) {
	last, lastSize := utf8.DecodeLastRuneInString(word)
	if last != 's' && last != 'S' {
		return word, false
	}
	apos, aposSize := utf8.DecodeLastRuneInString(word[:len(word)-lastSize])
	switch apos {
	case '\'', '’', '‘':
		return word[:len(word)-lastSize-aposSize], true
	}
	return word, false
}

// endsSentence reports whether a field's tail closes a sentence: a period,
// question, exclamation or ellipsis, behind any closing quotes or brackets
// it also carries. Colons and dialogue dashes do not close, so
// "said Elizabeth:" keeps a run open where a period would not.
func endsSentence(field string) bool {
	end := len(field)
	for end > 0 {
		r, size := utf8.DecodeLastRuneInString(field[:end])
		if isClosing(r) {
			end -= size
			continue
		}
		return r == '.' || r == '!' || r == '?' || r == '…'
	}
	return false
}

func isClosing(r rune) bool {
	switch r {
	case '"', '\'', '”', '’', '»', '›', ')', ']', '}':
		return true
	}
	return false
}

// honorificAbbreviation reports whether a field is an honorific written
// with its period — "Mr.", "Mrs.", "St." — whose full stop belongs to the
// title and not to the sentence.
func honorificAbbreviation(field string) bool {
	return strings.HasSuffix(field, ".") && honorifics[booktext.Normalize(trimPunctuation(field))]
}

// fieldSpans is strings.Fields that keeps its byte offsets — the same walk
// booktext's displayFields does, restated here because the fold's field
// distribution is the invariant both sides rely on. Fields split on ASCII
// whitespace only, exactly as the fold joins them.
func fieldSpans(s string) [][2]int {
	var out [][2]int
	start := -1
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ', '\t', '\n', '\r', '\v', '\f':
			if start >= 0 {
				out = append(out, [2]int{start, i})
				start = -1
			}
		default:
			if start < 0 {
				start = i
			}
		}
	}
	if start >= 0 {
		out = append(out, [2]int{start, len(s)})
	}
	return out
}

// isRomanNumeral keeps chapter numbering out of the index: "II", "VII",
// "XIV" are capitalized, mid-sentence often enough, and name nothing.
func isRomanNumeral(folded string) bool {
	if folded == "" || len(folded) > 6 {
		return false
	}
	value := 0
	for _, r := range folded {
		var v int
		switch r {
		case 'i':
			v = 1
		case 'v':
			v = 5
		case 'x':
			v = 10
		case 'l':
			v = 50
		case 'c':
			v = 100
		case 'd':
			v = 500
		case 'm':
			v = 1000
		default:
			return false
		}
		if value > 0 && v > value {
			// Subtractive pairs only: iv, ix, xl, xc, cd, cm.
			if !(value == 1 && (v == 5 || v == 10)) &&
				!(value == 10 && (v == 50 || v == 100)) &&
				!(value == 100 && (v == 500 || v == 1000)) {
				return false
			}
			value = v - value
			continue
		}
		value += v
	}
	return true
}

// honorifics are the titles and kinship words that prefix a name and are
// dropped from the run's identity: "Mrs. Norris" indexes as Norris, so
// the book's plain "Norris heard her" joins it. Folded keys.
var honorifics = map[string]bool{
	"mr": true, "mrs": true, "ms": true, "miss": true, "dr": true,
	"prof": true, "professor": true, "sir": true, "dame": true,
	"lord": true, "lady": true, "capt": true, "captain": true,
	"col": true, "colonel": true, "gen": true, "general": true,
	"lt": true, "lieutenant": true, "sgt": true, "sergeant": true,
	"rev": true, "reverend": true, "st": true, "saint": true,
	"aunt": true, "uncle": true, "cousin": true, "grandma": true,
	"grandpa": true, "granny": true, "nana": true, "papa": true,
	"mama": true, "mother": true, "father": true, "brother": true,
	"sister": true, "doctor": true, "madam": true, "madame": true,
	"monsieur": true, "mademoiselle": true, "herr": true, "frau": true,
	"count": true, "countess": true, "duke": true, "duchess": true,
	"king": true, "queen": true, "president": true, "judge": true,
	"officer": true, "detective": true, "nurse": true, "warden": true,
}

// connectives are the lowercase particles that glue a multi-word name
// across its middle — "Anne of Green Gables", "the Duchess of Malfi".
// Folded keys; mid-run only, never openers.
var connectives = map[string]bool{
	"of": true, "the": true, "de": true, "der": true, "den": true,
	"van": true, "von": true, "del": true, "della": true, "di": true,
	"da": true, "du": true, "la": true, "le": true, "les": true,
	"al": true, "el": true, "ibn": true, "mac": true, "mc": true,
	"o": true, "y": true, "e": true,
}

// nameStopList holds the words that stay capitalized mid-sentence for
// reasons that are not names: the sentence openers that follow dialogue,
// the shouted words of all-caps prose, the apparatus of chapters. Folded
// keys, matched against whole runs only — "The Manor" passes where "The"
// does not, because a run is a name and a lone word is a word.
var nameStopList = map[string]bool{
	"i": true, "oh": true, "ah": true, "yes": true, "no": true,
	"ok": true, "okay": true, "hey": true, "hello": true, "ha": true,
	"and": true, "but": true, "or": true, "nor": true, "for": true,
	"yet": true, "so": true, "the": true, "a": true, "an": true,
	"he": true, "she": true, "it": true, "they": true, "we": true,
	"you": true, "who": true, "what": true, "when": true, "where": true,
	"why": true, "how": true, "which": true, "whose": true,
	"chapter": true, "part": true, "book": true, "volume": true,
	"prologue": true, "epilogue": true, "foreword": true, "preface": true,
	"interlude": true, "afterword": true, "illustration": true,
	"monday": true, "tuesday": true, "wednesday": true, "thursday": true,
	"friday": true, "saturday": true, "sunday": true,
	"january": true, "february": true, "march": true, "april": true,
	"may": true, "june": true, "july": true, "august": true,
	"september": true, "october": true, "november": true, "december": true,
	"jan": true, "feb": true, "mar": true, "apr": true, "jun": true,
	"jul": true, "aug": true, "sep": true, "sept": true, "oct": true,
	"nov": true, "dec": true,
	"god": true, "christ": true, "jesus": true,
	"stop": true, "wait": true, "run": true, "help": true, "look": true,
	"listen": true, "please": true, "thanks": true, "thank": true,
	"sorry": true, "goodbye": true, "farewell": true,
}
