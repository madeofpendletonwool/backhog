package books

import (
	"testing"

	"github.com/collinpendleton/backhog/api/internal/books/epub"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// extract canonicalizes one document — one list of blocks per doc — and
// runs the extractor over the real pieces, so every test asserts against
// the same shapes the ingester produces.
func extract(t *testing.T, docs ...[]string) (canonical string, occ map[string][]int) {
	t.Helper()
	docDocs := make([]epub.Doc, len(docs))
	for i, blocks := range docs {
		docDocs[i] = epub.Doc{Href: "d.xhtml", Blocks: blocks}
	}
	canonical, display, _, index, _ := Canonicalize(&epub.Document{Docs: docDocs})
	occ = make(map[string][]int)
	for _, o := range ExtractNames(index, []byte(display)) {
		occ[o.Name] = append(occ[o.Name], o.CharStart)
	}
	return canonical, occ
}

func occurrencesOf(t *testing.T, blocks ...string) []models.NameOccurrence {
	t.Helper()
	doc := &epub.Document{Docs: []epub.Doc{{Href: "d.xhtml", Blocks: blocks}}}
	_, display, _, index, _ := Canonicalize(doc)
	return ExtractNames(index, []byte(display))
}

func TestExtractNamesFindsMidSentenceNames(t *testing.T) {
	_, names := extract(t,
		[]string{`"Impossible," said Elizabeth. "Quite impossible."`},
		[]string{`Elizabeth only smiled. Darcy said nothing more.`},
		[]string{`Jane wrote to Elizabeth while Darcy watched.`},
	)

	// One confident mid-sentence sighting promotes the key book-wide: the
	// sentence-opening Elizabeths index too.
	if got := len(names["elizabeth"]); got != 3 {
		t.Errorf("elizabeth occurrences = %v, want 3 (one confident + two openers)", names["elizabeth"])
	}
	if got := len(names["darcy"]); got != 2 {
		t.Errorf("darcy occurrences = %v, want 2", names["darcy"])
	}
	// Jane's single sighting is under the frequency floor, confident or not.
	if names["jane"] != nil {
		t.Errorf("jane occurrences = %v, want none under the floor", names["jane"])
	}
}

func TestExtractNamesOffsetsAddressTheCanonicalText(t *testing.T) {
	canonical, occ := extract(t,
		[]string{`Tobias greeted Mirabel at the door, and Mirabel laughed.`},
	)
	// Mirabel's two mid-sentence sightings promote her; Tobias's single
	// sentence-opening sighting stays under the floor.
	if len(occ["mirabel"]) != 2 || occ["tobias"] != nil {
		t.Fatalf("names = %v", occ)
	}
	raw := occurrencesOf(t, `Tobias greeted Mirabel at the door, and Mirabel laughed.`)
	if len(raw) != 2 {
		t.Fatalf("occurrences = %d, want 2: %+v", len(raw), raw)
	}
	for _, o := range raw {
		if o.CharEnd <= o.CharStart || o.CharEnd > len(canonical) {
			t.Fatalf("occurrence %+v outside the text of %d", o, len(canonical))
		}
		if got := canonical[o.CharStart:o.CharEnd]; got != o.Name {
			t.Errorf("canonical[%d:%d] = %q, occurrence says %q", o.CharStart, o.CharEnd, got, o.Name)
		}
	}
}

func TestExtractNamesRunsAndHonorifics(t *testing.T) {
	_, names := extract(t,
		[]string{`Mrs. Norris wrote to Lady Catherine de Bourgh about it.`},
		[]string{`Norris answered by return of post.`},
		[]string{`Catherine de Bourgh replied at length.`},
		[]string{`Anne of Green Gables was mentioned, and Anne of Green Gables again.`},
	)

	// The honorific drops from the run's identity, so "Mrs. Norris" and
	// plain "Norris" are one entry.
	if got := len(names["norris"]); got != 2 {
		t.Errorf("norris = %v, want mrs norris + norris to merge into 2", names["norris"])
	}
	// A connective run keeps its full shape, both occurrences.
	if got := len(names["anne of green gables"]); got != 2 {
		t.Errorf("anne of green gables = %v, want 2", names["anne of green gables"])
	}
	// Lady Catherine de Bourgh: honorific dropped, connective kept.
	if got := len(names["catherine de bourgh"]); got != 2 {
		t.Errorf("catherine de bourgh = %v, want 2", names["catherine de bourgh"])
	}
}

func TestExtractNamesDropsTheObviousNoise(t *testing.T) {
	_, names := extract(t,
		[]string{`Chapter IV. The Journey`},
		[]string{`"STOP!" he shouted. "WAIT!"`},
		[]string{`Monday was quiet and Tuesday was too.`},
		[]string{`Someone called Romeo once, and only once.`},
	)

	for _, banned := range []string{"chapter", "iv", "stop", "wait", "monday", "tuesday", "romeo"} {
		if names[banned] != nil {
			t.Errorf("index kept %q: %v", banned, names[banned])
		}
	}
}

func TestExtractNamesFrequencyFloor(t *testing.T) {
	_, names := extract(t,
		[]string{`Once Beatrice waved from the window.`},
		[]string{`Cecilia sang, and Cecilia danced, and Cecilia slept.`},
	)
	if names["beatrice"] != nil {
		t.Errorf("single sighting became an entry: %v", names["beatrice"])
	}
	if got := len(names["cecilia"]); got != 3 {
		t.Errorf("cecilia = %v, want 3", names["cecilia"])
	}
}

func TestExtractNamesPossessivesJoinTheirName(t *testing.T) {
	raw := occurrencesOf(t, `Elizabeth's letter reached Elizabeth by morning.`)
	if len(raw) != 2 {
		t.Fatalf("occurrences = %d, want 2: %+v", len(raw), raw)
	}
	for _, o := range raw {
		if o.Name != "elizabeth" {
			t.Errorf("occurrence name = %q, want elizabeth (possessive must fold in)", o.Name)
		}
	}
}

func TestExtractNamesSentenceOpenersDoNotIndex(t *testing.T) {
	_, names := extract(t,
		[]string{`The morning was cold. The kettle had not boiled. She waited.`},
	)
	if len(names) != 0 {
		t.Errorf("index = %v, want empty when every capital opens a sentence", names)
	}
}

func TestExtractNamesCurlyPossessive(t *testing.T) {
	raw := occurrencesOf(t, `Tobias’s dog barked at Tobias twice.`)
	if len(raw) != 2 {
		t.Fatalf("occurrences = %d, want 2: %+v", len(raw), raw)
	}
	for _, o := range raw {
		if o.Name != "tobias" {
			t.Errorf("name = %q, want tobias", o.Name)
		}
	}
}

func TestExtractNamesAcronym(t *testing.T) {
	_, names := extract(t,
		[]string{`The FBI called the FBI back.`},
	)
	if got := len(names["fbi"]); got != 2 {
		t.Errorf("fbi = %v, want 2 (short all-caps runs are acronyms)", names["fbi"])
	}
}
