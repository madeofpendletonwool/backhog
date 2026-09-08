package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// seedAudiobooks inventories audio files for one book — the shape a NAS holds
// when the same title was ripped twice — plus the user and library entry they
// attach to. Every file is given a duration, because a tape nobody measured
// is a different test.
func seedAudiobooks(t *testing.T, s *Store, files map[string]float64, order ...string) (userID, entryID string, ids map[string]int64) {
	t.Helper()
	ctx := context.Background()
	userID = newTestUser(t, s, "tapes@example.com", "tapes")
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO books (id, title) VALUES ('OL1W', 'Anathem')`); err != nil {
		t.Fatalf("seed book: %v", err)
	}
	rows := make([]models.MediaFile, 0, len(order))
	for _, p := range order {
		seconds := files[p]
		rows = append(rows, models.MediaFile{
			Root: "/nas", Path: p, Kind: models.MediaFileAudio, SizeBytes: 10, Mtime: 1,
			DurationSeconds: &seconds,
			ScannedAt:       time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		})
	}
	if err := s.InsertMediaFiles(ctx, rows); err != nil {
		t.Fatalf("insert: %v", err)
	}
	ids = map[string]int64{}
	for _, p := range order {
		var id int64
		if err := s.db.QueryRowContext(ctx,
			`SELECT id FROM media_files WHERE path = ?`, p).Scan(&id); err != nil {
			t.Fatalf("id of %s: %v", p, err)
		}
		ids[p] = id
	}
	return userID, entryFor(t, s, userID, "OL1W"), ids
}

// timelinePaths is what the player would actually be handed.
func timelinePaths(t *testing.T, s *Store) []string {
	t.Helper()
	files, err := s.AudioMediaFilesForBook(context.Background(), "OL1W")
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	return paths
}

func attach(t *testing.T, s *Store, userID, entryID string, ids ...int64) {
	t.Helper()
	if _, err := s.AttachMediaFiles(context.Background(), userID, entryID, ids, models.MediaFileAudio); err != nil {
		t.Fatalf("attach: %v", err)
	}
}

// A second recording is a fact about what the user owns, not a change to what
// plays. This is the audio half of the promise the text side makes when a
// .mobi lands beside the .epub — and here it is load-bearing twice over,
// because before editions existed the second rip's tracks interleaved with
// the first's.
func TestAttachingASecondAudiobookLeavesTheFirstPlaying(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID, entryID, ids := seedAudiobooks(t, s,
		map[string]float64{
			"Anathem [Dufris]/01.m4b":  100,
			"Anathem [Dufris]/02.m4b":  100,
			"Anathem [Guidall]/01.mp3": 60,
		},
		"Anathem [Dufris]/01.m4b", "Anathem [Dufris]/02.m4b", "Anathem [Guidall]/01.mp3")

	attach(t, s, userID, entryID, ids["Anathem [Dufris]/01.m4b"], ids["Anathem [Dufris]/02.m4b"])
	attach(t, s, userID, entryID, ids["Anathem [Guidall]/01.mp3"])

	editions, err := s.AudioEditionsForBook(ctx, "OL1W")
	if err != nil {
		t.Fatalf("editions: %v", err)
	}
	if len(editions) != 2 {
		t.Fatalf("editions = %d, want one per recording", len(editions))
	}
	if !editions[0].Primary || editions[1].Primary {
		t.Fatalf("designation moved to the newly attached recording: %+v", editions)
	}
	if editions[0].Label != "Anathem [Dufris]" || editions[0].TrackCount != 2 || editions[0].TotalDuration != 200 {
		t.Errorf("first edition = %q, %d tracks, %.0fs; want the Dufris directory, 2 tracks, 200s",
			editions[0].Label, editions[0].TrackCount, editions[0].TotalDuration)
	}
	// A lone file is named for itself: two rips often share one directory,
	// and "Anathem" twice tells the user nothing.
	if editions[1].Label != "01" {
		t.Errorf("second edition label = %q, want the file's own name", editions[1].Label)
	}

	want := []string{"Anathem [Dufris]/01.m4b", "Anathem [Dufris]/02.m4b"}
	if got := timelinePaths(t, s); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("timeline = %v, want only the designated recording %v", got, want)
	}
}

// The switch itself: the tape changes, and the listener keeps their place
// through the book rather than their place in a file that no longer exists.
func TestSetPrimaryAudioEditionCarriesThePositionByProportion(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID, entryID, ids := seedAudiobooks(t, s,
		map[string]float64{
			"Dufris/01.m4b":  100,
			"Dufris/02.m4b":  100,
			"Guidall/01.m4b": 40,
			"Guidall/02.m4b": 60,
		},
		"Dufris/01.m4b", "Dufris/02.m4b", "Guidall/01.m4b", "Guidall/02.m4b")

	attach(t, s, userID, entryID, ids["Dufris/01.m4b"], ids["Dufris/02.m4b"])
	attach(t, s, userID, entryID, ids["Guidall/01.m4b"], ids["Guidall/02.m4b"])

	// Half an hour into the second Dufris track: 150s of a 200s tape, so
	// three quarters of the way through the book.
	seconds := 50.0
	fileID := ids["Dufris/02.m4b"]
	if _, err := s.SaveBookProgress(ctx, userID, entryID, ProgressWrite{
		Source: models.PositionSourceListen, RawAudioSeconds: &seconds,
		RawAudioFileID: &fileID, PercentComplete: 75,
	}); err != nil {
		t.Fatalf("save progress: %v", err)
	}

	editions, err := s.AudioEditionsForBook(ctx, "OL1W")
	if err != nil {
		t.Fatalf("editions: %v", err)
	}
	target := editions[1].ID
	got, err := s.SetPrimaryAudioEdition(ctx, userID, entryID, target)
	if err != nil {
		t.Fatalf("switch: %v", err)
	}
	if !got.Primary || got.Label != "Guidall" {
		t.Fatalf("switched to %+v, want the Guidall recording designated", got)
	}
	if paths := timelinePaths(t, s); len(paths) != 2 || paths[0] != "Guidall/01.m4b" {
		t.Fatalf("timeline = %v, want the Guidall tracks", paths)
	}

	// Three quarters of a 100s tape is 75s: 40s of track one, so 35s into
	// track two.
	progress, err := s.BookProgress(ctx, userID, entryID)
	if err != nil {
		t.Fatalf("progress: %v", err)
	}
	if progress.RawAudioFileID == nil || *progress.RawAudioFileID != ids["Guidall/02.m4b"] {
		t.Fatalf("resume file = %v, want the second Guidall track %d",
			progress.RawAudioFileID, ids["Guidall/02.m4b"])
	}
	if progress.RawAudioSeconds == nil || *progress.RawAudioSeconds != 35 {
		t.Fatalf("resume at %v, want 35s into that track", progress.RawAudioSeconds)
	}
	if progress.PercentComplete != 75 {
		t.Errorf("percent = %v, want the 75 the listener saw", progress.PercentComplete)
	}
}

// An alignment maps this text onto the seconds of one specific reading. A
// different narrator does not say the same words at the same times, so the
// map goes rather than answering every query plausibly and wrongly.
func TestSetPrimaryAudioEditionDropsTheAlignment(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID, entryID, ids := seedAudiobooks(t, s,
		map[string]float64{"Dufris/01.m4b": 100, "Guidall/01.m4b": 100},
		"Dufris/01.m4b", "Guidall/01.m4b")
	attach(t, s, userID, entryID, ids["Dufris/01.m4b"])
	attach(t, s, userID, entryID, ids["Guidall/01.m4b"])
	seedAlignmentFor(t, s, entryID)

	editions, err := s.AudioEditionsForBook(ctx, "OL1W")
	if err != nil {
		t.Fatalf("editions: %v", err)
	}
	if _, err := s.SetPrimaryAudioEdition(ctx, userID, entryID, editions[1].ID); err != nil {
		t.Fatalf("switch: %v", err)
	}

	var alignments, jobs int
	if err := s.db.QueryRowContext(ctx,
		`SELECT (SELECT COUNT(*) FROM alignments WHERE entry_id = ?),
		        (SELECT COUNT(*) FROM alignment_jobs WHERE entry_id = ?)`,
		entryID, entryID).Scan(&alignments, &jobs); err != nil {
		t.Fatalf("count alignments: %v", err)
	}
	if alignments != 0 || jobs != 0 {
		t.Fatalf("after the switch: %d alignments, %d jobs; want both gone", alignments, jobs)
	}
}

// seedAlignmentFor gives an entry a finished alignment and a queued job, the
// two things a switch has to clear.
func seedAlignmentFor(t *testing.T, s *Store, entryID string) {
	t.Helper()
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	exec(`INSERT INTO media_files (id, root, path, kind, size_bytes, mtime, book_id, scanned_at)
	      VALUES (900, '/nas', 'Anathem.epub', 'epub', 10, 1, 'OL1W', CURRENT_TIMESTAMP)`)
	exec(`INSERT INTO epub_texts (id, media_file_id, char_count, word_count, normalized_sha256, parser_version)
	      VALUES ('text-1', 900, 400000, 70000, 'sha', 'v1')`)
	exec(`INSERT INTO alignments (id, entry_id, epub_text_id, state, coverage, mean_confidence)
	      VALUES ('al-1', ?, 'text-1', 'ready', 0.9, 0.9)`, entryID)
	exec(`INSERT INTO alignment_anchors (alignment_id, char_offset, audio_seconds)
	      VALUES ('al-1', 0, 0), ('al-1', 1000, 60)`)
	exec(`INSERT INTO alignment_jobs (id, entry_id, epub_text_id, audio_timeline_hash, state)
	      VALUES ('job-1', ?, 'text-1', 'hash', 'queued')`, entryID)
}

// Switching onto a recording whose files are all off the mount would leave
// the player with a timeline and no bytes. The switch is refused with the
// reason, and nothing moves.
func TestSetPrimaryAudioEditionRefusesAnUnmountedRecording(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID, entryID, ids := seedAudiobooks(t, s,
		map[string]float64{"Dufris/01.m4b": 100, "Guidall/01.m4b": 100},
		"Dufris/01.m4b", "Guidall/01.m4b")
	attach(t, s, userID, entryID, ids["Dufris/01.m4b"])
	attach(t, s, userID, entryID, ids["Guidall/01.m4b"])
	if _, err := s.db.ExecContext(ctx,
		`UPDATE media_files SET missing_at = CURRENT_TIMESTAMP WHERE id = ?`,
		ids["Guidall/01.m4b"]); err != nil {
		t.Fatalf("mark missing: %v", err)
	}

	editions, err := s.AudioEditionsForBook(ctx, "OL1W")
	if err != nil {
		t.Fatalf("editions: %v", err)
	}
	if _, err := s.SetPrimaryAudioEdition(ctx, userID, entryID, editions[1].ID); !errors.Is(err, ErrAttach) {
		t.Fatalf("switch onto a missing recording = %v, want ErrAttach", err)
	}
	if paths := timelinePaths(t, s); len(paths) != 1 || paths[0] != "Dufris/01.m4b" {
		t.Fatalf("timeline = %v, want the refused switch to have changed nothing", paths)
	}
}

// Re-sending a tape's files is how track order is corrected. It has to
// renumber that recording, not clone it into a second one.
func TestReattachingTheSameFilesReordersOneRecording(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID, entryID, ids := seedAudiobooks(t, s,
		map[string]float64{"Dufris/01.m4b": 100, "Dufris/02.m4b": 100},
		"Dufris/01.m4b", "Dufris/02.m4b")
	attach(t, s, userID, entryID, ids["Dufris/01.m4b"], ids["Dufris/02.m4b"])
	attach(t, s, userID, entryID, ids["Dufris/02.m4b"], ids["Dufris/01.m4b"])

	editions, err := s.AudioEditionsForBook(ctx, "OL1W")
	if err != nil {
		t.Fatalf("editions: %v", err)
	}
	if len(editions) != 1 {
		t.Fatalf("editions = %d, want the reorder to have stayed one recording", len(editions))
	}
	if paths := timelinePaths(t, s); paths[0] != "Dufris/02.m4b" {
		t.Fatalf("timeline = %v, want the new order", paths)
	}
}

// Detaching the last file of the designated recording leaves a book that
// still has an audiobook attached. Another one takes over, carrying the
// position with it — the same handover the text side does.
func TestDetachingTheLastTrackHandsOverToTheOtherRecording(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID, entryID, ids := seedAudiobooks(t, s,
		map[string]float64{"Dufris/01.m4b": 100, "Guidall/01.m4b": 200},
		"Dufris/01.m4b", "Guidall/01.m4b")
	attach(t, s, userID, entryID, ids["Dufris/01.m4b"])
	attach(t, s, userID, entryID, ids["Guidall/01.m4b"])

	seconds, fileID := 25.0, ids["Dufris/01.m4b"]
	if _, err := s.SaveBookProgress(ctx, userID, entryID, ProgressWrite{
		Source: models.PositionSourceListen, RawAudioSeconds: &seconds,
		RawAudioFileID: &fileID, PercentComplete: 25,
	}); err != nil {
		t.Fatalf("save progress: %v", err)
	}

	if err := s.DetachMediaFile(ctx, userID, entryID, ids["Dufris/01.m4b"]); err != nil {
		t.Fatalf("detach: %v", err)
	}

	editions, err := s.AudioEditionsForBook(ctx, "OL1W")
	if err != nil {
		t.Fatalf("editions: %v", err)
	}
	if len(editions) != 1 || !editions[0].Primary || editions[0].Label != "01" {
		t.Fatalf("editions = %+v, want the surviving recording designated", editions)
	}
	progress, err := s.BookProgress(ctx, userID, entryID)
	if err != nil {
		t.Fatalf("progress: %v", err)
	}
	if progress.RawAudioFileID == nil || *progress.RawAudioFileID != ids["Guidall/01.m4b"] {
		t.Fatalf("resume file = %v, want the surviving recording", progress.RawAudioFileID)
	}
	if progress.RawAudioSeconds == nil || *progress.RawAudioSeconds != 50 {
		t.Fatalf("resume at %v, want a quarter of the way into the 200s tape", progress.RawAudioSeconds)
	}
}

// The narrator is a hint for telling two recordings apart, and it is only
// offered when the tags are unambiguous: album artist for the author, artist
// for whoever read it. One name on its own means "whoever is responsible",
// which is the author as often as the reader.
func TestEditionNarratorOnlyWhenTheTagsAreUnambiguous(t *testing.T) {
	tags := func(artist, albumArtist string) editionFile {
		raw, err := json.Marshal(map[string]string{"artist": artist, "album_artist": albumArtist})
		if err != nil {
			t.Fatalf("marshal tags: %v", err)
		}
		return editionFile{path: "x/01.m4b", metadata: raw}
	}
	for _, tc := range []struct {
		name  string
		files []editionFile
		want  string
	}{
		{"both, different", []editionFile{tags("William Dufris", "Neal Stephenson")}, "William Dufris"},
		{"artist alone", []editionFile{tags("Neal Stephenson", "")}, ""},
		{"the same name twice", []editionFile{tags("Neal Stephenson", "neal stephenson")}, ""},
		{"no tags at all", []editionFile{{path: "x/01.m4b"}}, ""},
	} {
		if got := editionNarrator(tc.files); got != tc.want {
			t.Errorf("%s: narrator = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// A rip split across platters is one recording, named for the directory the
// platters share rather than for "Disc 1".
func TestEditionLabelUsesTheSharedDirectory(t *testing.T) {
	files := []editionFile{
		{path: "Anathem (Unabridged)/Disc 1/01.mp3"},
		{path: "Anathem (Unabridged)/Disc 1/02.mp3"},
		{path: "Anathem (Unabridged)/Disc 2/01.mp3"},
	}
	if got := editionLabel(files); got != "Anathem (Unabridged)" {
		t.Errorf("label = %q, want the directory the discs share", got)
	}
}
