package naming

import (
	"path/filepath"
	"strings"
	"testing"
)

func album() Fields {
	return Fields{
		Title:  "ある夏の日",
		RJCode: "RJ123456",
		Circle: "Some Circle",
		Artist: "Some CV",
		Date:   "2024-07-02",
		Year:   "2024",
		Month:  "07",
		Day:    "02",
	}
}

func TestParseRejectsFileFieldInDirectory(t *testing.T) {
	for _, tmpl := range []string{
		"{chapter}/{rjcode}.{ext}",
		"{number}/{title}.{ext}",
		"{track}/{title}.{ext}",
		"{ext}/{title}.{ext}",
	} {
		if _, err := Parse(tmpl); err == nil {
			t.Errorf("Parse(%q) succeeded, want an error", tmpl)
		}
	}
}

// A bare path is a filename, as it is to yt-dlp, and a filename that cannot
// carry an extension is refused before a run starts rather than after it.
func TestParseRejectsAPathWithNoFilename(t *testing.T) {
	for _, tmpl := range []string{
		"./out",
		"out",
		"./out/",
		"C:/Audio",
		"/srv/audio",
		"{year}/{rjcode}",
	} {
		if _, err := Parse(tmpl); err == nil {
			t.Errorf("Parse(%q) succeeded, want an error naming -P", tmpl)
		}
	}
}

func TestParseAcceptsALiteralExtension(t *testing.T) {
	tmpl, err := Parse("{year}/{title}.mp3")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := tmpl.File(Fields{Title: "a"}), "a.mp3"; got != want {
		t.Errorf("File = %q, want %q", got, want)
	}
}

// A trailing separator names no file, so the segment before it does.
func TestTrailingSeparatorNamesTheLastSegment(t *testing.T) {
	tmpl, err := Parse("{rjcode}/{title}.{ext}/")
	if err != nil {
		t.Fatal(err)
	}

	f := Fields{RJCode: "RJ1", Title: "a", Ext: "mp3"}
	if got, want := tmpl.Dir(f), "RJ1"; got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
	if got, want := tmpl.File(f), "a.mp3"; got != want {
		t.Errorf("File = %q, want %q", got, want)
	}
}

func TestParseRejectsBadTemplates(t *testing.T) {
	for _, tmpl := range []string{
		"",
		"   ",
		"{nope}.{ext}",
		"{rjcode.{ext}",
		// Gone: what a file is called is the template's to say.
		"{name}.{ext}",
		"{index}.{ext}",
	} {
		if _, err := Parse(tmpl); err == nil {
			t.Errorf("Parse(%q) succeeded, want an error", tmpl)
		}
	}
}

func TestDefaultLayout(t *testing.T) {
	tmpl, err := Parse("{year}/{rjcode}/{rjcode}_{number}.{ext}")
	if err != nil {
		t.Fatal(err)
	}

	f := album()
	f.Number, f.Width, f.Ext = 2, 1, "mp3"

	if got, want := tmpl.Dir(f), filepath.Join("2024", "RJ123456"); got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
	if got, want := tmpl.File(f), "RJ123456_2.mp3"; got != want {
		t.Errorf("File = %q, want %q", got, want)
	}
	if got, want := tmpl.Path(f), filepath.Join("2024", "RJ123456", "RJ123456_2.mp3"); got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}

func TestDefaultSplitLayout(t *testing.T) {
	tmpl, err := Parse("{year}/{rjcode}/{number}_{chapter}.{ext}")
	if err != nil {
		t.Fatal(err)
	}

	f := album()
	f.Number, f.Width, f.Chapter, f.Ext = 7, Width(12), "耳かき", "m4a"

	if got, want := tmpl.File(f), "07_耳かき.m4a"; got != want {
		t.Errorf("File = %q, want %q", got, want)
	}
}

// A post missing a field keeps the level the template asked for.
func TestEmptyFieldStandsIn(t *testing.T) {
	tmpl, err := Parse("{year}/{rjcode}/{title}.{ext}")
	if err != nil {
		t.Fatal(err)
	}

	f := Fields{RJCode: "RJ1", Title: "a", Ext: "mp3"}
	if got, want := tmpl.Dir(f), filepath.Join(Unknown, "RJ1"); got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
}

func TestEveryFieldCanStandIn(t *testing.T) {
	tmpl, err := Parse("{circle}/{artist}/{title}-{chapter}.{ext}")
	if err != nil {
		t.Fatal(err)
	}

	if got, want := tmpl.Dir(Fields{}), filepath.Join(Unknown, Unknown); got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
	if got, want := tmpl.File(Fields{}), Unknown+"-"+Unknown+"."+Unknown; got != want {
		t.Errorf("File = %q, want %q", got, want)
	}
}

// A literal level has nothing to stand in for.
func TestLiteralDirSegmentIsKept(t *testing.T) {
	tmpl, err := Parse("out/{title}.{ext}")
	if err != nil {
		t.Fatal(err)
	}

	f := Fields{Title: "a", Ext: "mp3"}
	if got, want := tmpl.Dir(f), "out"; got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
}

// Page content must never open a directory or climb out of one. The dots
// themselves are harmless once no separator survives beside them.
func TestFieldValuesCannotTraverse(t *testing.T) {
	tmpl, err := Parse("{title}/{chapter}.{ext}")
	if err != nil {
		t.Fatal(err)
	}

	f := Fields{
		Title:   "../../etc",
		Chapter: "../../../evil",
		Ext:     "mp3",
	}
	for _, got := range []string{tmpl.Dir(f), tmpl.File(f)} {
		if strings.ContainsAny(got, `/\`) {
			t.Errorf("%q crossed a path boundary", got)
		}
		if got == "." || got == ".." {
			t.Errorf("%q is a traversal", got)
		}
	}
}

// A value that sanitizes away still holds its level, so two such posts do not
// land on top of each other.
func TestDotOnlyValueKeepsItsLevel(t *testing.T) {
	tmpl, err := Parse("{title}/{rjcode}.{ext}")
	if err != nil {
		t.Fatal(err)
	}

	f := Fields{Title: "..", RJCode: "RJ1", Ext: "mp3"}
	if got, want := tmpl.Dir(f), "untitled"; got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
}

func TestRootedTemplateStaysRooted(t *testing.T) {
	tmpl, err := Parse("/srv/audio/{rjcode}/{title}.{ext}")
	if err != nil {
		t.Fatal(err)
	}

	f := Fields{RJCode: "RJ1", Title: "a", Ext: "mp3"}
	if got, want := filepath.ToSlash(tmpl.Dir(f)), "/srv/audio/RJ1"; got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
}

func TestWindowsSeparatorsAndVolume(t *testing.T) {
	tmpl, err := Parse(`F:\Music\{rjcode}\{title}.{ext}`)
	if err != nil {
		t.Fatal(err)
	}

	f := Fields{RJCode: "RJ1", Title: "a", Ext: "mp3"}
	if got, want := filepath.ToSlash(tmpl.Dir(f)), "F:/Music/RJ1"; got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
}

func TestWidthPadsToTotal(t *testing.T) {
	for _, tc := range []struct{ total, want int }{{0, 1}, {1, 1}, {9, 1}, {10, 2}, {120, 3}} {
		if got := Width(tc.total); got != tc.want {
			t.Errorf("Width(%d) = %d, want %d", tc.total, got, tc.want)
		}
	}
}

// A counter reads as a number, never as "0", and stands in like anything else.
func TestCounterRendering(t *testing.T) {
	tmpl, err := Parse("{track}of{tracktotal}_{title}.{ext}")
	if err != nil {
		t.Fatal(err)
	}

	set := Fields{Track: 3, TrackTotal: 12, Title: "a", Ext: "mp3"}
	if got, want := tmpl.File(set), "3of12_a.mp3"; got != want {
		t.Errorf("File = %q, want %q", got, want)
	}

	unset := Fields{Title: "a", Ext: "mp3"}
	if got, want := tmpl.File(unset), Unknown+"of"+Unknown+"_a.mp3"; got != want {
		t.Errorf("File = %q, want %q", got, want)
	}
}

func TestNilTemplateNamesNothing(t *testing.T) {
	var tmpl *Template

	if got := tmpl.File(album()); got != "" {
		t.Errorf("File = %q, want empty", got)
	}
	if got, want := tmpl.Dir(album()), "."; got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
}

func TestFieldNamesAreCaseInsensitive(t *testing.T) {
	tmpl, err := Parse("{RJCode}/{Title}.{EXT}")
	if err != nil {
		t.Fatal(err)
	}

	f := Fields{RJCode: "RJ1", Title: "a", Ext: "mp3"}
	if got, want := tmpl.Dir(f), "RJ1"; got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
	if got, want := tmpl.File(f), "a.mp3"; got != want {
		t.Errorf("File = %q, want %q", got, want)
	}
}
