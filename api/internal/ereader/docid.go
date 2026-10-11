// Package ereader holds the protocol-level pieces an e-reader needs from
// Backhog (MAD-827): how a KOReader-protocol client names the book it is
// reading, and the exact author and title strings the catalog hands it.
// Nothing here touches the database or HTTP; the rules are the clients',
// ported byte for byte and golden-tested against them.
package ereader

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
)

// partialChunk is the sample size of KOReader's partial MD5.
const partialChunk = 1024

// PartialMD5 computes KOReader's binary document id: the MD5 of 1 KiB
// samples read at offsets 0, then 1024 << 2i for i = 0..10 (1 KiB, 4 KiB,
// 16 KiB … 1 GiB), stopping at the first offset past the end of the file.
// A sample cut short by the end of the file contributes what it has.
//
// The first offset is 0, not 256. KOReader's loop starts at i = -1 and
// computes lshift(1024, -2), which LuaJIT's 32-bit shift wraps to 0;
// CrossPoint hardcodes the same 0 to match (its header comment says 256,
// its code says 0 — the code is what talks to the server). Any change here
// silently breaks sync for every device, so the goldens in docid_test.go
// were produced by the clients, not by this function.
func PartialMD5(r io.ReaderAt, size int64) (string, error) {
	h := md5.New()
	buf := make([]byte, partialChunk)
	for i := -1; i <= 10; i++ {
		offset := int64(0)
		if i >= 0 {
			offset = int64(partialChunk) << (2 * i)
		}
		if offset >= size {
			break
		}
		n, err := r.ReadAt(buf, offset)
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		h.Write(buf[:n])
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// FilenameMD5 is the filename-mode document id: the MD5 of the bare
// filename the client saved the book under.
func FilenameMD5(filename string) string {
	sum := md5.Sum([]byte(filename))
	return hex.EncodeToString(sum[:])
}
