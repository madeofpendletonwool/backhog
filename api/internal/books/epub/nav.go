package epub

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"path"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// parseNav reads an EPUB 3 nav document and returns its TOC entries in
// document order. The preferred listing is the <nav epub:type="toc">; if
// absent (or unmarked) the first <nav> with nested list links is used.
func parseNav(zr *zip.Reader, navHref string) ([]tocEntry, error) {
	root, err := parseHTML(zr, navHref)
	if err != nil {
		return nil, err
	}
	navDir := path.Dir(navHref)

	navs := findAll(root, func(n *html.Node) bool {
		return n.DataAtom == atom.Nav
	})
	var listNode *html.Node
	for _, nav := range navs {
		if epubType(nav) == "toc" {
			listNode = nav
			break
		}
	}
	if listNode == nil {
		for _, nav := range navs {
			listNode = nav
			break
		}
	}
	if listNode == nil {
		return nil, fmt.Errorf("epub: nav document %s has no nav element", navHref)
	}

	var entries []tocEntry
	walkNavList(listNode, navDir, -1, &entries)
	return entries, nil
}

// walkNavList descends the nested <ol> structure of a nav element,
// recording each <li><a> as a TOC entry at its nesting depth (top level 0).
// depth starts at -1 so the outer <ol> brings it to 0.
func walkNavList(n *html.Node, navDir string, depth int, entries *[]tocEntry) {
	if n.Type == html.ElementNode {
		switch n.DataAtom {
		case atom.Ol:
			depth++
		case atom.Li:
			if rawHref, title, ok := navItemLink(n); ok {
				href := joinZipPath(navDir, rawHref)
				fragment := ""
				if i := strings.Index(rawHref, "#"); i >= 0 {
					fragment = rawHref[i+1:]
				}
				*entries = append(*entries, tocEntry{
					href:     href,
					fragment: fragment,
					title:    title,
					depth:    depth,
				})
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkNavList(c, navDir, depth, entries)
	}
}

// navItemLink extracts the target href and link text of an <li>'s direct
// <a> child. Items without a link (span-only group headers) are skipped.
func navItemLink(li *html.Node) (href, title string, ok bool) {
	for c := li.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.DataAtom == atom.A {
			for _, attr := range c.Attr {
				if attr.Key == "href" {
					return attr.Val, strings.TrimSpace(textContent(c)), true
				}
			}
		}
	}
	return "", "", false
}

// epubType returns the epub:type attribute of an element, if any.
func epubType(n *html.Node) string {
	for _, attr := range n.Attr {
		if attr.Key == "epub:type" {
			return strings.TrimSpace(attr.Val)
		}
	}
	return ""
}

// parseNCX reads an EPUB 2 NCX file and returns its navMap entries in
// document order, preserving nesting depth.
func parseNCX(zr *zip.Reader, ncxHref string) ([]tocEntry, error) {
	f := zipLookup(zr, ncxHref)
	if f == nil {
		return nil, fmt.Errorf("epub: NCX not found in zip: %s", ncxHref)
	}
	data, err := readAll(f)
	if err != nil {
		return nil, err
	}

	var raw struct {
		NavMap struct {
			Points []ncxPoint `xml:"navPoint"`
		} `xml:"navMap"`
	}
	if err := xml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("epub: parse NCX %s: %w", ncxHref, err)
	}

	ncxDir := path.Dir(ncxHref)
	var entries []tocEntry
	for _, p := range raw.NavMap.Points {
		p.collect(ncxDir, 0, &entries)
	}
	return entries, nil
}

// ncxPoint is one <navPoint>; children nest recursively.
type ncxPoint struct {
	PlayOrder string   `xml:"playOrder,attr"`
	Labels    []string `xml:"navLabel>text"`
	Content   []struct {
		Src string `xml:"src,attr"`
	} `xml:"content"`
	Points []ncxPoint `xml:"navPoint"`
}

// collect appends this point and its descendants to entries in document
// order. NCX nesting is the TOC depth.
func (p ncxPoint) collect(ncxDir string, depth int, entries *[]tocEntry) {
	title := ""
	if len(p.Labels) > 0 {
		title = p.Labels[0]
	}
	if len(p.Content) > 0 && p.Content[0].Src != "" {
		href := joinZipPath(ncxDir, p.Content[0].Src)
		src := p.Content[0].Src
		fragment := ""
		if i := strings.Index(src, "#"); i >= 0 {
			fragment = src[i+1:]
		}
		*entries = append(*entries, tocEntry{href: href, fragment: fragment, title: title, depth: depth})
	}
	for _, child := range p.Points {
		child.collect(ncxDir, depth+1, entries)
	}
}

// findAll collects nodes matching pred in tree order.
func findAll(n *html.Node, pred func(*html.Node) bool) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if pred(n) {
			out = append(out, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return out
}

// textContent returns the concatenated text of an element's descendants.
func textContent(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}
