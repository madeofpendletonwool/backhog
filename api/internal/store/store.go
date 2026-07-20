package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
)

// ErrNotFound is returned when a row does not exist, or exists but belongs to
// another user. Callers must not distinguish the two: leaking "exists but not
// yours" would let one user enumerate another's library.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned on a uniqueness violation the caller can act on,
// e.g. adding a game already in the library.
var ErrConflict = errors.New("already exists")

// Store is the data access layer. Every user-scoped method takes userID as its
// first argument after ctx so that ownership filtering cannot be forgotten.
type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store { return &Store{db: db} }

// DB exposes the underlying handle for health checks.
func (s *Store) DB() *sql.DB { return s.db }

// newID returns a random 128-bit identifier as hex.
func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
