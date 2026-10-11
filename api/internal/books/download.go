package books

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// EpubFile is the original EPUB behind a user's book entry, opened for
// serving as-is (MAD-829).
type EpubFile struct {
	File   *os.File
	Info   fs.FileInfo
	Media  models.MediaFile
	BookID string
}

// OpenEpubFile opens the exact file a reader's EPUB text was parsed from,
// for the e-reader download. The bytes leave untouched: a KOReader-protocol
// client identifies the book by a hash of what it stored, so any repacking
// on the way out would silently break sync.
//
// The rules are OpenAsset's. Ownership (or a share) is re-checked on every
// request — range follow-ups included — because the URL carries no
// authority; the file resolves inside its media root with symlinks
// followed; and only a .epub is served, since a MOBI- or PDF-sourced text
// is something no EPUB reader can open. Every refusal is store.ErrNotFound
// or ErrNoEpub, which the endpoint answers alike.
//
// The caller closes File.
func (ing *Ingester) OpenEpubFile(ctx context.Context, userID, entryID string) (EpubFile, error) {
	bookID, err := ing.store.BookFilesForEntry(ctx, userID, entryID)
	if err != nil {
		return EpubFile{}, err
	}
	f, err := ing.store.EpubMediaFileForBook(ctx, bookID)
	if err != nil {
		return EpubFile{}, ErrNoEpub
	}
	if strings.ToLower(filepath.Ext(f.Path)) != ".epub" {
		return EpubFile{}, ErrNoEpub
	}
	p, err := resolveWithinRoot(f.Root, f.Path)
	if err != nil {
		return EpubFile{}, ErrNoEpub
	}
	file, err := os.Open(p)
	if err != nil {
		return EpubFile{}, ErrNoEpub
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		if err == nil {
			err = fmt.Errorf("books: %s is not a regular file", f.Path)
		}
		return EpubFile{}, fmt.Errorf("%w: %v", ErrNoEpub, err)
	}
	return EpubFile{File: file, Info: info, Media: f, BookID: bookID}, nil
}
