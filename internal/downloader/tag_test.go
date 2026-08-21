package downloader

import (
	"slices"
	"strings"
	"testing"
)

func metaValue(args []string, key string) (string, bool) {
	for i := 0; i+1 < len(args); i++ {
		if args[i] != "-metadata" {
			continue
		}
		if k, v, ok := strings.Cut(args[i+1], "="); ok && k == key {
			return v, true
		}
	}
	return "", false
}

// mp3 and m4a take one "n/total" value.
func TestCountersPairOnMostMuxers(t *testing.T) {
	tags := Tags{Track: 3, TrackTotal: 12, Disc: 1, DiscTotal: 1}

	for _, muxer := range []string{"mp3", "ipod"} {
		args := tags.args(muxer)
		if got, _ := metaValue(args, "track"); got != "3/12" {
			t.Errorf("%s: track = %q, want %q", muxer, got, "3/12")
		}
		if got, _ := metaValue(args, "disc"); got != "1/1" {
			t.Errorf("%s: disc = %q, want %q", muxer, got, "1/1")
		}
	}
}

// FLAC carries the number and the total as separate Vorbis comments.
func TestCountersSplitOnFLAC(t *testing.T) {
	args := Tags{Track: 3, TrackTotal: 12, Disc: 1, DiscTotal: 1}.args("flac")

	for key, want := range map[string]string{
		"TRACKNUMBER": "3",
		"TRACKTOTAL":  "12",
		"DISCNUMBER":  "1",
		"DISCTOTAL":   "1",
	} {
		if got, ok := metaValue(args, key); !ok || got != want {
			t.Errorf("%s = %q (present %v), want %q", key, got, ok, want)
		}
	}
	if _, ok := metaValue(args, "track"); ok {
		t.Error("flac got a track=n/total value it cannot read")
	}
}

func TestUnsetCountersAreNotWritten(t *testing.T) {
	args := Tags{Title: "x"}.args("mp3")
	if slices.Contains(args, "-metadata") && len(args) != 2 {
		t.Errorf("args = %v, want the title alone", args)
	}
	for _, key := range []string{"track", "disc", "TRACKNUMBER", "DISCNUMBER"} {
		if _, ok := metaValue(args, key); ok {
			t.Errorf("%s written with nothing to say", key)
		}
	}
}

// A total of zero is unknown, not "of nothing".
func TestCounterWithoutTotal(t *testing.T) {
	if got, _ := metaValue(Tags{Track: 2}.args("mp3"), "track"); got != "2" {
		t.Errorf("track = %q, want %q", got, "2")
	}
	if got, _ := metaValue(Tags{Track: 2}.args("flac"), "TRACKNUMBER"); got != "2" {
		t.Errorf("TRACKNUMBER = %q, want %q", got, "2")
	}
	if _, ok := metaValue(Tags{Track: 2}.args("flac"), "TRACKTOTAL"); ok {
		t.Error("TRACKTOTAL written with no total")
	}
}

func TestTaggingPadsCoverToSquareWithBlack(t *testing.T) {
	d := Downloader{CoverPath: "cover.png"}
	_, opts := d.tagging(Tags{}, 1, "mp3", "")

	for i := 0; i+1 < len(opts); i++ {
		if opts[i] == "-vf" {
			if got, want := opts[i+1], `pad=max(iw\,ih):max(iw\,ih):(ow-iw)/2:(oh-ih)/2:color=black`; got != want {
				t.Errorf("cover filter = %q, want %q", got, want)
			}
			return
		}
	}
	t.Fatal("cover tagging has no square padding filter")
}

// A run with nothing to write must not rewrite the file through ffmpeg.
func TestNothingToTagSurvivesTheCounters(t *testing.T) {
	var d Downloader
	if !d.nothingToTag(Tags{}, nil) {
		t.Error("an empty Tags no longer reads as nothing to write")
	}
	if d.nothingToTag(Tags{Disc: 1, DiscTotal: 1}, nil) {
		t.Error("a disc number alone should still be written")
	}
}

// A piece is titled for its chapter, plainly: the track number beside it
// already carries the order, unlike the filename or an embedded chapter label.
func TestPieceTagsTitleIsTheChapter(t *testing.T) {
	base := Tags{Title: "ある夏の日", Album: "RJ123456", Track: 1, TrackTotal: 1, Disc: 1, DiscTotal: 1}
	got := PieceTags(base, "耳かき", 3, 12)

	if got.Title != "耳かき" {
		t.Errorf("Title = %q, want %q", got.Title, "耳かき")
	}
	if got.Track != 3 || got.TrackTotal != 12 {
		t.Errorf("track = %d/%d, want 3/12", got.Track, got.TrackTotal)
	}
	if got.Disc != 1 || got.DiscTotal != 1 {
		t.Errorf("disc = %d/%d, want 1/1", got.Disc, got.DiscTotal)
	}
	if got.Album != base.Album {
		t.Errorf("Album = %q, want the work's own %q", got.Album, base.Album)
	}
}

// A run without --embed-metadata must stay that way, not acquire a title
// through the split.
func TestPieceTagsStayEmptyWhenTaggingIsOff(t *testing.T) {
	if got := PieceTags(Tags{}, "耳かき", 3, 12); got != (Tags{}) {
		t.Errorf("pieceTags = %+v, want an empty Tags", got)
	}
}

// The remux writes .m4a whatever extension the template asked for.
func TestStreamNameForcesTheRemuxExtension(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"work.m4a", "work.m4a"},
		{"work.mp3", "work.m4a"},
		{"work.flac", "work.m4a"},
		{"work", "work.m4a"},
		{"01_Track 1", "01_Track 1.m4a"},
		{"work.part2", "work.part2.m4a"},
	} {
		if got := streamName(tc.in); got != tc.want {
			t.Errorf("streamName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
