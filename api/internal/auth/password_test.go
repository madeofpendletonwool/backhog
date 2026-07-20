package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestHashAndVerify(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if err := VerifyPassword("correct horse battery staple", hash); err != nil {
		t.Errorf("VerifyPassword with correct password: %v", err)
	}
	if err := VerifyPassword("wrong password", hash); !errors.Is(err, ErrMismatch) {
		t.Errorf("VerifyPassword with wrong password = %v, want ErrMismatch", err)
	}
}

// The same password must produce different hashes, or the salt is not doing
// its job and identical passwords would be visible in the database.
func TestHashIsSalted(t *testing.T) {
	a, err := HashPassword("same password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	b, err := HashPassword("same password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if a == b {
		t.Error("two hashes of the same password are identical; salt is not random")
	}
}

func TestVerifyRejectsMalformedHashes(t *testing.T) {
	valid, err := HashPassword("password123")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	tests := map[string]string{
		"empty":            "",
		"not a hash":       "hunter2",
		"wrong algorithm":  strings.Replace(valid, "argon2id", "argon2i", 1),
		"truncated":        valid[:len(valid)/2],
		"missing sections": "$argon2id$v=19$m=65536,t=1,p=4",
	}

	for name, hash := range tests {
		t.Run(name, func(t *testing.T) {
			if err := VerifyPassword("password123", hash); err == nil {
				t.Error("expected an error for a malformed hash, got nil")
			}
		})
	}
}

// A hash stored with different cost parameters must still verify, so the cost
// can be raised later without locking existing users out.
func TestVerifyReadsParametersFromHash(t *testing.T) {
	hash, err := HashPassword("password123")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.Contains(hash, "m=65536,t=1,p=4") {
		t.Fatalf("unexpected parameter encoding in %q", hash)
	}
	if err := VerifyPassword("password123", hash); err != nil {
		t.Errorf("VerifyPassword: %v", err)
	}
}
