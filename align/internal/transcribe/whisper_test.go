package transcribe

import (
	"errors"
	"testing"
)

const sampleOutput = `{
  "systeminfo": "AVX = 1",
  "model": {"type": "base"},
  "params": {"model": "/models/model.bin", "language": "en", "translate": false},
  "result": {"language": "en"},
  "transcription": [
    {"timestamps": {"from": "00:00:00,000", "to": "00:00:04,120"},
     "offsets": {"from": 0, "to": 4120},
     "text": " Chapter one."},
    {"timestamps": {"from": "00:00:04,120", "to": "00:00:07,500"},
     "offsets": {"from": 4120, "to": 7500},
     "text": " [BLANK_AUDIO]"},
    {"timestamps": {"from": "00:00:07,500", "to": "00:00:11,940"},
     "offsets": {"from": 7500, "to": 11940},
     "text": " It was a bright cold day in April."}
  ]
}`

func TestParseJSONReadsOffsetsAsSeconds(t *testing.T) {
	segments, err := ParseJSON([]byte(sampleOutput))
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	if len(segments) != 2 {
		t.Fatalf("got %d segments, want 2 (the blank one is dropped): %+v", len(segments), segments)
	}
	if segments[0].Start != 0 || segments[0].End != 4.12 {
		t.Errorf("first span = [%v,%v]", segments[0].Start, segments[0].End)
	}
	if segments[0].Text != "Chapter one." {
		t.Errorf("first text = %q, want it trimmed", segments[0].Text)
	}
	if segments[1].Start != 7.5 || segments[1].End != 11.94 {
		t.Errorf("second span = [%v,%v]", segments[1].Start, segments[1].End)
	}
}

func TestParseJSONRejectsGarbage(t *testing.T) {
	if _, err := ParseJSON([]byte("not json")); err == nil {
		t.Fatal("want an error")
	}
}

func TestSpeechKeepsRealTextAndDropsPlaceholders(t *testing.T) {
	for _, text := range []string{"[BLANK_AUDIO]", " [blank_audio] ", "(silence)", "[MUSIC]", "", "   "} {
		if Speech(text) {
			t.Errorf("%q should not count as speech", text)
		}
	}
	for _, text := range []string{"Chapter one.", "[Sighs] he said", "(laughing) yes"} {
		if !Speech(text) {
			t.Errorf("%q should count as speech", text)
		}
	}
}

func TestClassifySeparatesAModelProblemFromABadChunk(t *testing.T) {
	runErr := errors.New("exit status 1")

	modelErr := classify("whisper_init_from_file_with_params_no_state: failed to load model\nerror: failed to initialize whisper context", "chunk.wav", runErr)
	if !errors.Is(modelErr, ErrModelLoad) {
		t.Errorf("want ErrModelLoad, got %v", modelErr)
	}

	audioErr := classify("error: failed to read audio data", "chunk.wav", runErr)
	if !errors.Is(audioErr, ErrTranscribe) {
		t.Errorf("want ErrTranscribe, got %v", audioErr)
	}
	if errors.Is(audioErr, ErrModelLoad) {
		t.Error("a bad chunk should not be reported as a model problem")
	}
}

func TestArgsAskForJSONOutput(t *testing.T) {
	w := Whisper{Bin: "whisper-cli", ModelPath: "/models/model.bin", Language: "en", Threads: 4, BeamSize: 2}
	args := w.Args("/scratch/chunk.wav", "/scratch/chunk")

	want := map[string]string{"-m": "/models/model.bin", "-f": "/scratch/chunk.wav", "-l": "en", "-t": "4", "-bs": "2", "-of": "/scratch/chunk"}
	for flag, value := range want {
		if got := argValue(args, flag); got != value {
			t.Errorf("%s = %q, want %q", flag, got, value)
		}
	}
	if !hasFlag(args, "-oj") {
		t.Error("-oj (JSON output) is required")
	}
}

func TestArgsClampNonsenseValues(t *testing.T) {
	w := Whisper{Threads: 0, BeamSize: 0}
	args := w.Args("a.wav", "a")
	if argValue(args, "-t") != "1" || argValue(args, "-bs") != "1" {
		t.Errorf("args = %v", args)
	}
}

func argValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}
