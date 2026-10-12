package books

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/collinpendleton/backhog/api/internal/books/epub"
)

// TestXPointerDeviceGoldens checks the Locator against XPointers a real
// KOReader produced, on the real books it produced them in. The books are
// not redistributable, so this runs only when BACKHOG_DEVICE_GOLDENS names a
// JSON manifest beside them:
//
//	[{"epub": "acho.epub",
//	  "xpointer": "/body/DocFragment[26]/body/div/section/p[3]/text()[2].227",
//	  "contains": "you’re one of the best"}]
//
// "contains" is text at the pointer as an XML parser (crengine's view of
// the document) resolves it; the block the Locator lands on must hold it.
// Collect pointers from a device's .sdr/metadata.epub.lua (last_xpointer)
// or from the progress a sync pushed. The synthetic fixtures in
// xpointer_test.go cover the same shapes in CI.
func TestXPointerDeviceGoldens(t *testing.T) {
	manifest := os.Getenv("BACKHOG_DEVICE_GOLDENS")
	if manifest == "" {
		t.Skip("BACKHOG_DEVICE_GOLDENS not set")
	}
	raw, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var goldens []struct {
		EPUB, XPointer, Contains string
	}
	if err := json.Unmarshal(raw, &goldens); err != nil {
		t.Fatal(err)
	}
	for _, g := range goldens {
		data, err := os.ReadFile(filepath.Join(filepath.Dir(manifest), g.EPUB))
		if err != nil {
			t.Fatal(err)
		}
		doc, err := epub.Parse(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("%s: %v", g.EPUB, err)
		}
		_, display, _, index, _ := Canonicalize(doc)
		zr, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		loc, err := NewLocator(zr, doc, index)
		if err != nil {
			t.Fatalf("%s: %v", g.EPUB, err)
		}
		offset, ok := loc.FromXPointer(g.XPointer)
		if !ok {
			t.Errorf("%s %s: not placed", g.EPUB, g.XPointer)
			continue
		}
		block := displayBlockAt(index, display, offset)
		if !strings.Contains(block, g.Contains) {
			t.Errorf("%s %s\n  landed in: %.120q\n  want text: %q", g.EPUB, g.XPointer, block, g.Contains)
		}
		// And back: the block's own pointer must name the same fragment.
		back, _ := loc.ToXPointer(offset)
		if frag := g.XPointer[:strings.Index(g.XPointer, "]")+1]; !strings.HasPrefix(back, frag) {
			t.Errorf("%s: %s came back as %s", g.EPUB, g.XPointer, back)
		}
		t.Logf("%s %s → %d → %s", g.EPUB, g.XPointer, offset, back)
	}
}

// displayBlockAt returns the display text of the block starting at offset.
func displayBlockAt(index *BlockIndex, display string, offset int) string {
	lines := strings.Split(display, "\n")
	g := 0
	for _, d := range index.Documents {
		for _, start := range d.Blocks {
			if start == offset && g < len(lines) {
				return lines[g]
			}
			g++
		}
	}
	return ""
}
