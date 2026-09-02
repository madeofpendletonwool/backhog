package audio

import (
	"errors"
	"strconv"
	"testing"
)

func TestParseDuration(t *testing.T) {
	got, err := ParseDuration("42.512000\n")
	if err != nil || got != 42.512 {
		t.Fatalf("ParseDuration = %v, %v", got, err)
	}

	// ffprobe prints N/A for a stream it cannot measure. That is a
	// decode problem, not a zero-length file.
	for _, raw := range []string{"N/A", "", "0", "-1"} {
		if _, err := ParseDuration(raw); !errors.Is(err, ErrDecode) {
			t.Errorf("ParseDuration(%q) error = %v, want ErrDecode", raw, err)
		}
	}
}

func TestExtractArgsSeeksBeforeInput(t *testing.T) {
	args := ExtractArgs("/media/book.m4b", "/scratch/chunk.wav", 3600, 605)

	ss, input := indexOf(args, "-ss"), indexOf(args, "-i")
	if ss < 0 || input < 0 || ss > input {
		t.Fatalf("-ss must come before -i for index seeking: %v", args)
	}
	if args[ss+1] != "3600.000" || args[indexOf(args, "-t")+1] != "605.000" {
		t.Errorf("window = %v", args)
	}
	if got := args[indexOf(args, "-ar")+1]; got != strconv.Itoa(SampleRate) {
		t.Errorf("sample rate = %s, want %d", got, SampleRate)
	}
	if args[indexOf(args, "-ac")+1] != "1" {
		t.Errorf("want mono: %v", args)
	}
	if args[len(args)-1] != "/scratch/chunk.wav" {
		t.Errorf("output must be last: %v", args)
	}
}

func TestExtractArgsClampsNegativeSeek(t *testing.T) {
	args := ExtractArgs("in.mp3", "out.wav", -5, 10)
	if args[indexOf(args, "-ss")+1] != "0.000" {
		t.Errorf("args = %v", args)
	}
}

func TestReadableDistinguishesAMissingMount(t *testing.T) {
	err := readable("/definitely/not/mounted/book.m4b")
	if !errors.Is(err, ErrUnreadable) {
		t.Fatalf("error = %v, want ErrUnreadable", err)
	}
}

func indexOf(args []string, flag string) int {
	for i, a := range args {
		if a == flag {
			return i
		}
	}
	return -1
}
