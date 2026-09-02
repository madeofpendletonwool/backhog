// Package epub parses EPUB files into their spine structure: the ordered
// spine documents, each with its block-level text runs and its TOC title.
// It is pure parsing — no normalization, no storage, no filesystem. The
// caller (internal/books) owns canonicalization: it applies books.Normalize
// to the extracted blocks and computes the char offsets everything in the
// arena addresses.
//
// Supported: EPUB 2 (NCX TOC) and EPUB 3 (nav document). DRM-protected files
// are refused with ErrDRM — this project does not handle DRM by decision.
package epub

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"

	"golang.org/x/net/html"
)

// ErrDRM reports an EPUB carrying META-INF/encryption.xml, which is how DRM
// (Adobe ADEPT, etc.) marks an encrypted package. The file is skipped and
// reported, never worked around.
var ErrDRM = errors.New("epub: file is DRM-protected (META-INF/encryption.xml present)")

// Doc is one spine document in reading order.
type Doc struct {
	// SpineIndex is the document's position in the OPF spine.
	SpineIndex int
	// Href is the document's path inside the zip, e.g. "OEBPS/Text/ch1.xhtml".
	Href string
	// Title and Depth come from the first TOC entry that targets this
	// document; both are zero values when the TOC never mentions it.
	Title string
	Depth int
	// Blocks holds the raw text of each block-level element (paragraph,
	// heading, list item, ...) in document order. Raw means pre-normalized;
	// blank entries are possible and are dropped by the canonicalizer.
	Blocks []string
	// Images holds the document's internal illustrations in document
	// order. Only references that resolve to a file actually inside this
	// EPUB survive; see extractor.resolveAsset.
	Images []Image
}

// Image is one illustration a spine document references. It carries no
// bytes: the reader fetches those from the asset endpoint, which reopens
// the EPUB and re-checks who is asking.
//
// Only *internal* references become an Image. A remote URL, a
// protocol-relative one, a data: URI and a reference that climbs out of the
// zip are all dropped during extraction, so nothing downstream — the
// sidecar, the chapters payload, the reader — ever holds an address that
// could reach a third party.
type Image struct {
	// Href is the image's path inside the zip, resolved against the
	// document that referenced it, e.g. "OEBPS/Images/plate1.png".
	Href string
	Alt  string
	// BeforeBlock is the index into Blocks of the block that follows this
	// image; len(Blocks) means it trails every block in the document.
	BeforeBlock int
}

// Document is the parse result: the spine in reading order.
type Document struct {
	Docs []Doc
}

// Parse reads an EPUB held in r (an io.ReaderAt of the whole file, as
// archive/zip requires) and returns its spine structure.
func Parse(r io.ReaderAt, size int64) (*Document, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("epub: open zip: %w", err)
	}

	if f := zipLookup(zr, "META-INF/encryption.xml"); f != nil {
		return nil, ErrDRM
	}

	container, err := readContainer(zr)
	if err != nil {
		return nil, err
	}

	pkg, err := readOPF(zr, container.RootfilePath)
	if err != nil {
		return nil, err
	}

	entries, err := readTOC(zr, container.RootfilePath, pkg)
	if err != nil {
		// A broken TOC is not a broken book: spine order still defines the
		// canonical text, only titles are lost.
		entries = nil
	}

	doc := &Document{Docs: make([]Doc, 0, len(pkg.Spine.ItemRefs))}
	for _, ref := range pkg.Spine.ItemRefs {
		if ref.Linear == "no" {
			continue
		}
		item, ok := pkg.Manifest[ref.IDRef]
		if !ok {
			continue
		}
		href := joinZipPath(path.Dir(container.RootfilePath), item.Href)
		blocks, images, err := extractBlocks(zr, href)
		if err != nil {
			return nil, err
		}
		doc.Docs = append(doc.Docs, Doc{
			SpineIndex: len(doc.Docs),
			Href:       href,
			Title:      matchingTitle(entries, href),
			Depth:      matchingDepth(entries, href),
			Blocks:     blocks,
			Images:     images,
		})
	}
	return doc, nil
}

// container is the parsed META-INF/container.xml.
type container struct {
	RootfilePath string
}

func readContainer(zr *zip.Reader) (*container, error) {
	f := zipLookup(zr, "META-INF/container.xml")
	if f == nil {
		return nil, errors.New("epub: missing META-INF/container.xml")
	}
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("epub: open container.xml: %w", err)
	}
	defer rc.Close()

	var raw struct {
		Rootfiles []struct {
			FullPath  string `xml:"full-path,attr"`
			MediaType string `xml:"media-type,attr"`
		} `xml:"rootfiles>rootfile"`
	}
	if err := xml.NewDecoder(rc).Decode(&raw); err != nil {
		return nil, fmt.Errorf("epub: parse container.xml: %w", err)
	}
	for _, rf := range raw.Rootfiles {
		if rf.MediaType == "" || rf.MediaType == "application/oebps-package+xml" {
			if rf.FullPath != "" {
				return &container{RootfilePath: rf.FullPath}, nil
			}
		}
	}
	return nil, errors.New("epub: container.xml has no OPF rootfile")
}

// manifestItem is one OPF <item>.
type manifestItem struct {
	Href       string
	MediaType  string
	Properties string
}

// packageXML is the parsed OPF package.
type packageXML struct {
	Manifest map[string]manifestItem
	// spineToc is the spine@toc idref naming the NCX, when present.
	spineToc string
	Spine    struct {
		ItemRefs []struct {
			IDRef  string `xml:"idref,attr"`
			Linear string `xml:"linear,attr"`
		} `xml:"itemref"`
	}
}

func readOPF(zr *zip.Reader, opfPath string) (*packageXML, error) {
	f := zipLookup(zr, opfPath)
	if f == nil {
		return nil, fmt.Errorf("epub: OPF not found at %s", opfPath)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("epub: open %s: %w", opfPath, err)
	}
	defer rc.Close()

	var raw struct {
		Manifest struct {
			Items []struct {
				ID         string `xml:"id,attr"`
				Href       string `xml:"href,attr"`
				MediaType  string `xml:"media-type,attr"`
				Properties string `xml:"properties,attr"`
			} `xml:"item"`
		} `xml:"manifest"`
		Spine struct {
			Toc      string `xml:"toc,attr"`
			ItemRefs []struct {
				IDRef  string `xml:"idref,attr"`
				Linear string `xml:"linear,attr"`
			} `xml:"itemref"`
		} `xml:"spine"`
	}
	if err := xml.NewDecoder(rc).Decode(&raw); err != nil {
		return nil, fmt.Errorf("epub: parse %s: %w", opfPath, err)
	}

	pkg := &packageXML{Manifest: make(map[string]manifestItem, len(raw.Manifest.Items))}
	for _, it := range raw.Manifest.Items {
		pkg.Manifest[it.ID] = manifestItem{Href: it.Href, MediaType: it.MediaType, Properties: it.Properties}
	}
	pkg.spineToc = raw.Spine.Toc
	pkg.Spine.ItemRefs = raw.Spine.ItemRefs
	return pkg, nil
}

// tocEntry is one TOC record from the NCX or nav document, in document order.
type tocEntry struct {
	href     string // zip path, fragment stripped
	fragment string
	title    string
	depth    int
}

// readTOC finds the EPUB 3 nav document (manifest item with a "nav"
// property) or the EPUB 2 NCX (spine@toc idref, else any NCX media-type
// item) and parses its entries. Hrefs are resolved to zip paths.
func readTOC(zr *zip.Reader, opfPath string, pkg *packageXML) ([]tocEntry, error) {
	opfDir := path.Dir(opfPath)

	for _, item := range pkg.Manifest {
		if hasProperty(item.Properties, "nav") {
			return parseNav(zr, joinZipPath(opfDir, item.Href))
		}
	}

	if it, ok := pkg.Manifest[pkg.ncxID()]; ok {
		return parseNCX(zr, joinZipPath(opfDir, it.Href))
	}
	for _, it := range pkg.Manifest {
		if it.MediaType == "application/x-dtbncx+xml" {
			return parseNCX(zr, joinZipPath(opfDir, it.Href))
		}
	}
	return nil, errors.New("epub: no nav document and no NCX in manifest")
}

// ncxID returns the spine@toc idref, if any.
func (p *packageXML) ncxID() string { return p.spineToc }

// hasProperty reports whether a space-separated EPUB property list carries
// the given token.
func hasProperty(props, token string) bool {
	for _, f := range strings.Fields(props) {
		if f == token {
			return true
		}
	}
	return false
}

// matchingTitle returns the title of the first TOC entry targeting href.
func matchingTitle(entries []tocEntry, href string) string {
	for _, e := range entries {
		if e.href == href && e.title != "" {
			return e.title
		}
	}
	return ""
}

// matchingDepth returns the depth of the first TOC entry targeting href.
func matchingDepth(entries []tocEntry, href string) int {
	for _, e := range entries {
		if e.href == href {
			return e.depth
		}
	}
	return 0
}

// joinZipPath resolves rel against the directory of base inside the zip,
// producing a clean forward-slash path. Absolute hrefs (leading '/') are
// treated as root-relative, and fragments/queries are stripped.
func joinZipPath(baseDir, rel string) string {
	rel = strings.TrimSuffix(rel, "#") // bare "#" hrefs
	if i := strings.IndexAny(rel, "#?"); i >= 0 {
		rel = rel[:i]
	}
	if rel == "" {
		return path.Clean(baseDir)
	}
	if strings.HasPrefix(rel, "/") {
		return path.Clean(rel)[1:]
	}
	if decoded, err := url.PathUnescape(rel); err == nil {
		rel = decoded
	}
	return path.Clean(path.Join(baseDir, rel))
}

// zipLookup finds a zip entry by exact name, falling back to the
// percent-decoded name for manifests that store encoded hrefs.
func zipLookup(zr *zip.Reader, name string) *zip.File {
	if f := lookup(zr, name); f != nil {
		return f
	}
	if decoded, err := url.PathUnescape(name); err == nil && decoded != name {
		return lookup(zr, decoded)
	}
	return nil
}

func lookup(zr *zip.Reader, name string) *zip.File {
	for _, f := range zr.File {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// readAll reads a zip entry fully.
func readAll(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("epub: open %s: %w", f.Name, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("epub: read %s: %w", f.Name, err)
	}
	return data, nil
}

// parseHTML reads a zip entry into an HTML node tree.
func parseHTML(zr *zip.Reader, href string) (*html.Node, error) {
	f := zipLookup(zr, href)
	if f == nil {
		return nil, fmt.Errorf("epub: document not found in zip: %s", href)
	}
	data, err := readAll(f)
	if err != nil {
		return nil, err
	}
	root, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("epub: parse %s: %w", href, err)
	}
	return root, nil
}
