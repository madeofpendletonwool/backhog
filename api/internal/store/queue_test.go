package store

import (
	"errors"
	"testing"
)

func ptr(f float64) *float64 { return &f }

func TestMidpoint(t *testing.T) {
	tests := []struct {
		name          string
		before, after *float64
		want          float64
		wantErr       error
	}{
		{"empty list", nil, nil, positionGap, nil},
		{"insert at front", nil, ptr(1024), 0, nil},
		{"append to end", ptr(4096), nil, 5120, nil},
		{"between neighbours", ptr(1024), ptr(2048), 1536, nil},
		{"tight but valid gap", ptr(1), ptr(1.00001), 1.000005, nil},
		{"converged gap", ptr(1), ptr(1 + 1e-9), 0, errNeedsRenormalize},
		{"identical neighbours", ptr(5), ptr(5), 0, errNeedsRenormalize},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := midpoint(tt.before, tt.after)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("midpoint() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if got != tt.want {
				t.Errorf("midpoint() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestMidpointStaysOrdered checks the invariant that matters: a midpoint always
// lands strictly between its neighbours, so repeated inserts never reorder the
// queue. The worst case is inserting into the same slot over and over; that
// must eventually ask for a renormalize rather than silently collapse two
// entries onto the same position.
func TestMidpointStaysOrdered(t *testing.T) {
	lo, hi := 0.0, positionGap
	insertions := 0

	for {
		mid, err := midpoint(&lo, &hi)
		if errors.Is(err, errNeedsRenormalize) {
			break
		}
		if err != nil {
			t.Fatalf("midpoint: %v", err)
		}
		if mid <= lo || mid >= hi {
			t.Fatalf("insertion %d: %v is not strictly between %v and %v", insertions, mid, lo, hi)
		}
		hi = mid
		insertions++

		if insertions > 1000 {
			t.Fatal("never converged; the renormalize guard is unreachable")
		}
	}

	// Halving a gap of positionGap down to minGap takes log2(1024/1e-6) ≈ 30
	// steps. Far fewer would mean the guard is firing early and renormalizing
	// more often than necessary.
	if insertions < 25 {
		t.Errorf("only %d insertions before renormalize; expected ~30", insertions)
	}
	t.Logf("worst-case insertions before renormalize: %d", insertions)
}
