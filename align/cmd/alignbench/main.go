// Command alignbench measures the aligner instead of guessing at it.
//
// With no flags it runs the synthetic scenarios in internal/bench — a clean
// reading, two noisy ones, an abridgement and an outright wrong book — and
// prints coverage, mean confidence and the distance between where the
// aligner puts a moment and where it really is. Those are the numbers the
// worker's ready/low_confidence thresholds were chosen from, and re-running
// this is how to find out whether a change to the aligner moved them.
//
//	go run ./cmd/alignbench
//
// Given a real pair it runs that instead. There is no ground truth for a
// real book, so it reports coverage, confidence and the anchor map's shape,
// which is enough to spot-check a handful of anchors by hand:
//
//	go run ./cmd/alignbench -book /data/epub_text/<id>.txt -segments dump.json
//
// where dump.json is a JSON array of {"audio_start","audio_end","text"} —
// the same shape the worker posts to /internal/align/{id}/segments.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/collinpendleton/backhog/align/internal/align"
	"github.com/collinpendleton/backhog/align/internal/bench"
)

func main() {
	book := flag.String("book", "", "path to a canonical text file to align against")
	segs := flag.String("segments", "", "path to a JSON array of transcript segments")
	only := flag.String("case", "", "run only the named synthetic case")
	verbose := flag.Bool("v", false, "print the first and last few anchors")
	flag.Parse()

	if err := run(*book, *segs, *only, *verbose); err != nil {
		fmt.Fprintln(os.Stderr, "alignbench:", err)
		os.Exit(1)
	}
}

func run(book, segs, only string, verbose bool) error {
	opts := align.DefaultOptions()
	if book != "" || segs != "" {
		if book == "" || segs == "" {
			return fmt.Errorf("-book and -segments go together")
		}
		return runReal(book, segs, opts, verbose)
	}

	fmt.Printf("%-12s %-52s %s\n", "case", "scenario", "result")
	for _, p := range bench.Cases() {
		if only != "" && p.Name != only {
			continue
		}
		started := time.Now()
		report := bench.Measure(bench.Generate(p), opts)
		fmt.Printf("%-12s %-52s\n  %s  [%s]\n", p.Name, p.Description,
			report.String(), time.Since(started).Round(time.Millisecond))
		if verbose {
			printAnchors(report.Result)
		}
	}
	return nil
}

func runReal(book, segs string, opts align.Options, verbose bool) error {
	text, err := os.ReadFile(book)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(segs)
	if err != nil {
		return err
	}
	var wire []struct {
		AudioStart float64 `json:"audio_start"`
		AudioEnd   float64 `json:"audio_end"`
		Text       string  `json:"text"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return fmt.Errorf("decode %s: %w", segs, err)
	}
	segments := make([]align.Segment, len(wire))
	for i, s := range wire {
		segments[i] = align.Segment{AudioStart: s.AudioStart, AudioEnd: s.AudioEnd, Text: s.Text}
	}

	started := time.Now()
	res := align.Align(string(text), segments, opts)
	fmt.Printf("chars=%d segments=%d\n", len(text), len(segments))
	fmt.Printf("coverage=%.3f mean_confidence=%.3f anchors=%d windows=%d located=%d [%s]\n",
		res.Coverage, res.MeanConfidence, len(res.Anchors),
		res.Stats.Windows, res.Stats.LocatedWindows, time.Since(started).Round(time.Millisecond))
	if len(res.Anchors) > 0 {
		fmt.Printf("first anchor: char %d at %.1fs; last anchor: char %d at %.1fs\n",
			res.Anchors[0].CharOffset, res.Anchors[0].AudioSeconds,
			res.Anchors[len(res.Anchors)-1].CharOffset,
			res.Anchors[len(res.Anchors)-1].AudioSeconds)
	}
	if verbose {
		printAnchors(res)
	}
	return nil
}

// printAnchors shows the head, middle and tail of the map — the three
// places worth spot-checking against the book by hand.
func printAnchors(res align.Result) {
	if len(res.Anchors) == 0 {
		return
	}
	show := func(label string, from, to int) {
		fmt.Printf("  %s:\n", label)
		for i := from; i < to && i < len(res.Anchors); i++ {
			a := res.Anchors[i]
			fmt.Printf("    char %-8d %8.1fs  conf %.2f\n", a.CharOffset, a.AudioSeconds, a.Confidence)
		}
	}
	show("start", 0, 5)
	show("middle", len(res.Anchors)/2, len(res.Anchors)/2+5)
	show("end", max(len(res.Anchors)-5, 0), len(res.Anchors))
}
