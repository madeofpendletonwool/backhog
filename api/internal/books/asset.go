package books

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"time"
)

// ErrNotAsset reports an href the reader may not have: not a relative
// reference, not an image, or not present in this EPUB at all. Every one of
// those answers the same way at the endpoint — a 404 — because telling a
// caller *which* of them it was is telling them what is in someone else's
// library.
var ErrNotAsset = errors.New("books: not a readable image in this epub")

// assetSizeLimit caps what one asset request will decompress. Book
// illustrations are tens of kilobytes; a zip entry claiming a gigabyte is
// either a bomb or a mistake, and reading it would trade one request for the
// server's memory.
const assetSizeLimit = 16 << 20

// Asset is one image read out of an EPUB for the reader.
type Asset struct {
	// Href is the cleaned zip path the bytes came from.
	Href        string
	ContentType string
	Data        []byte
	// ModTime is the zip entry's own timestamp, which is stable across
	// re-reads and is what the endpoint validates against.
	ModTime time.Time
}

// OpenAsset reads one internal image out of the EPUB attached to a user's
// book entry.
//
// The containment rules are the audio endpoint's, applied twice over. The
// EPUB *file* is resolved through resolveWithinRoot, so a hand-edited
// media_files row or a symlink planted in the library cannot turn this into
// an arbitrary-file read. The href *inside* the zip is cleaned and refused
// if it is absolute, carries a scheme, or climbs out with "..". And like
// OpenTrack, ownership is re-checked here on every request rather than
// trusted from the URL: the URL is not a capability.
//
// Only raster image types are served. An EPUB's XHTML, its OPF and its
// stylesheets are not things the reader needs, and SVG is deliberately
// excluded: an SVG served from our own origin is a document that can carry
// script if it is ever navigated to directly, and there is no version of
// "render the book" that is worth that.
func (ing *Ingester) OpenAsset(ctx context.Context, userID, entryID, href string) (Asset, error) {
	bookID, err := ing.store.BookIDForEntry(ctx, userID, entryID)
	if err != nil {
		return Asset{}, err
	}
	f, err := ing.store.EpubMediaFileForBook(ctx, bookID)
	if err != nil {
		return Asset{}, ErrNoEpub
	}

	name, ok := cleanZipPath(href)
	if !ok {
		return Asset{}, ErrNotAsset
	}
	contentType, ok := imageContentType(name)
	if !ok {
		return Asset{}, ErrNotAsset
	}

	epubPath, err := resolveWithinRoot(f.Root, f.Path)
	if err != nil {
		return Asset{}, err
	}
	zr, err := zip.OpenReader(epubPath)
	if err != nil {
		return Asset{}, fmt.Errorf("books: open epub: %w", err)
	}
	defer zr.Close()

	entry := zipEntry(&zr.Reader, name)
	if entry == nil || entry.FileInfo().IsDir() {
		return Asset{}, ErrNotAsset
	}
	if entry.UncompressedSize64 > assetSizeLimit {
		return Asset{}, ErrNotAsset
	}
	rc, err := entry.Open()
	if err != nil {
		return Asset{}, fmt.Errorf("books: open epub asset: %w", err)
	}
	defer rc.Close()
	// LimitReader as well as the header check: the declared size is the
	// archive's claim about itself, not a measurement.
	data, err := io.ReadAll(io.LimitReader(rc, assetSizeLimit+1))
	if err != nil {
		return Asset{}, fmt.Errorf("books: read epub asset: %w", err)
	}
	if len(data) > assetSizeLimit {
		return Asset{}, ErrNotAsset
	}
	return Asset{Href: name, ContentType: contentType, Data: data, ModTime: entry.Modified}, nil
}

// cleanZipPath normalizes a requested href into the zip entry name it may
// address, or refuses it. A zip entry name is not a filesystem path, so
// lookup alone cannot escape anywhere — but the same shapes are refused
// anyway, because an href that says "/etc/passwd" or "https://elsewhere" is
// a caller asking for something this endpoint does not do, and answering it
// at all is how the next parser bug becomes a read primitive.
func cleanZipPath(href string) (string, bool) {
	href = strings.TrimSpace(href)
	if href == "" || strings.ContainsRune(href, 0) {
		return "", false
	}
	if i := strings.IndexAny(href, "#?"); i >= 0 {
		href = href[:i]
	}
	// Percent-decode before the checks, so "..%2Fsecret" is judged as the
	// path it becomes rather than the string it arrived as.
	if decoded, err := url.PathUnescape(href); err == nil {
		href = decoded
	}
	if strings.HasPrefix(href, "//") {
		return "", false
	}
	if i := strings.Index(href, ":"); i >= 0 && i < strings.IndexAny(href+"/", "/") {
		return "", false // a scheme: http:, data:, file:
	}
	clean := path.Clean(href)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", false
	}
	return clean, true
}

// zipEntry finds an entry by name, falling back to the percent-decoded name
// for archives that store encoded paths (the same fallback the parser uses,
// so what the parser indexed is what this can serve).
func zipEntry(zr *zip.Reader, name string) *zip.File {
	for _, f := range zr.File {
		if f.Name == name {
			return f
		}
		if decoded, err := url.PathUnescape(f.Name); err == nil && decoded == name {
			return f
		}
	}
	return nil
}

// imageContentType maps the image extensions an EPUB may legitimately
// illustrate with onto the type a browser needs. Anything else — XHTML,
// CSS, fonts, SVG — is not an asset this endpoint serves.
func imageContentType(p string) (string, bool) {
	switch strings.ToLower(path.Ext(p)) {
	case ".png":
		return "image/png", true
	case ".jpg", ".jpeg":
		return "image/jpeg", true
	case ".gif":
		return "image/gif", true
	case ".webp":
		return "image/webp", true
	}
	return "", false
}
