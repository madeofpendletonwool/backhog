package tesseract

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleTSV = `level	page_num	block_num	par_num	line_num	word_num	left	top	width	height	conf	text
1	1	0	0	0	0	0	0	800	600	-1	
5	1	1	1	1	1	10	10	50	20	96.25	WE
5	1	1	1	1	2	70	10	60	20	91.5	WERE
5	1	1	1	2	1	10	40	80	20	88.0	LEAN
2	1	0	0	0	0	0	0	0	0	-1	
5	1	2	1	1	1	10	100	60	20	75.0	HUNGRY
`

func TestParseTSVGroupsLinesAndAveragesConfidence(t *testing.T) {
	reading, err := ParseTSV(sampleTSV)
	if err != nil {
		t.Fatalf("ParseTSV: %v", err)
	}
	if reading.Text != "WE WERE\nLEAN\nHUNGRY" {
		t.Fatalf("text = %q", reading.Text)
	}
	// (96.25 + 91.5 + 88 + 75) / 4 / 100 = 0.876875
	if reading.MeanConfidence < 0.876 || reading.MeanConfidence > 0.878 {
		t.Fatalf("mean confidence = %v, want ~0.877", reading.MeanConfidence)
	}
}

func TestParseTSVNoWordsIsAnHonestEmpty(t *testing.T) {
	reading, err := ParseTSV("level\tpage_num\n1\t1\n")
	if err != nil {
		t.Fatalf("ParseTSV: %v", err)
	}
	if reading.Text != "" || reading.MeanConfidence != 0 {
		t.Fatalf("reading = %+v, want empty", reading)
	}
}

func TestParseTSVDropsPlaceholderRows(t *testing.T) {
	// Confidence -1 rows and empty words are structural, not lettering.
	tsv := strings.ReplaceAll(sampleTSV, "96.25", "-1")
	tsv = strings.ReplaceAll(tsv, "WE", "")
	reading, err := ParseTSV(tsv)
	if err != nil {
		t.Fatalf("ParseTSV: %v", err)
	}
	if strings.Contains(reading.Text, "WE") {
		t.Fatalf("placeholder leaked: %q", reading.Text)
	}
}

// stubBin writes a shell script masquerading as tesseract: it records the
// image paths it was handed and prints the sample TSV (or fails, on ask).
func stubBin(t *testing.T, output string, fail bool) (string, func() []string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	if err := os.WriteFile(log, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
[ "$1" = "--version" ] && { echo "tesseract 5.3.0"; exit 0; }
echo "$1" >> "` + log + `"
` + func() string {
		if fail {
			return `echo "boom" >&2; exit 1`
		}
		return `cat <<'EOF'
` + output + `EOF`
	}()
	path := filepath.Join(dir, "tesseract")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	calls := func() []string {
		data, _ := os.ReadFile(log)
		var out []string
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if line != "" {
				out = append(out, line)
			}
		}
		return out
	}
	return path, calls
}

func TestReadRunsTheBinaryAndParsesOutput(t *testing.T) {
	bin, calls := stubBin(t, sampleTSV, false)
	eng := Tesseract{Bin: bin, Language: "eng", PSM: 11}

	if _, err := eng.Check(); err != nil {
		t.Fatalf("check: %v", err)
	}
	reading, err := eng.Read(context.Background(), "/tmp/page.png")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if reading.Text == "" || reading.MeanConfidence <= 0 {
		t.Fatalf("reading = %+v", reading)
	}
	if got := calls(); len(got) != 1 {
		t.Fatalf("calls = %v", got)
	}
}

func TestArgsCarryTheKnobs(t *testing.T) {
	eng := Tesseract{Bin: "tesseract", Language: "eng", PSM: 11}
	got := strings.Join(eng.Args("/tmp/p.png"), " ")
	want := "/tmp/p.png stdout -l eng --psm 11 tsv"
	if got != want {
		t.Fatalf("args = %q, want %q", got, want)
	}
}

func TestReadFailureIsNamed(t *testing.T) {
	bin, _ := stubBin(t, "", true)
	eng := Tesseract{Bin: bin, Language: "eng", PSM: 11}
	if _, err := eng.Read(context.Background(), "/tmp/page.png"); err == nil ||
		!strings.Contains(err.Error(), "boom") {
		t.Fatalf("read error = %v, want the stderr detail", err)
	}
}

func TestCheckNamesTheVersion(t *testing.T) {
	bin, _ := stubBin(t, sampleTSV, false)
	eng := Tesseract{Bin: bin}
	version, err := eng.Check()
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if version != "tesseract 5.3.0" {
		t.Fatalf("version = %q", version)
	}
}
