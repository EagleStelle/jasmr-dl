package naming

import (
	"strings"
	"testing"
)

// defaultBranch splits Default by hand, so the expectation owes nothing to the
// code under test.
func defaultBranch(split bool) string {
	open := strings.IndexByte(Default, '<')
	shut := strings.IndexByte(Default, '>')
	branches := strings.Split(Default[open+1:shut], "|")
	if split {
		return Default[:open] + branches[1] + Default[shut+1:]
	}
	return Default[:open] + branches[0] + Default[shut+1:]
}

func TestSelectDividesOnTheGroup(t *testing.T) {
	const raw = "{year}/{circle}/<{rjcode}_{number}.{ext}|{number}_{chapter}.{ext}>"

	for _, tc := range []struct {
		split bool
		want  string
	}{
		{false, "{year}/{circle}/{rjcode}_{number}.{ext}"},
		{true, "{year}/{circle}/{number}_{chapter}.{ext}"},
	} {
		got, err := Select(raw, tc.split)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("Select(split=%v) = %q, want %q", tc.split, got, tc.want)
		}
	}
}

// The divider sits wherever the two shapes part, not at a fixed depth.
func TestSelectDividesAWholePath(t *testing.T) {
	const raw = "<{year}/{title}/{rjcode}_{number}.{ext}|{date}/{rjcode}/{number}_{chapter}.{ext}>"

	got, err := Select(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	if want := "{date}/{rjcode}/{number}_{chapter}.{ext}"; got != want {
		t.Errorf("Select = %q, want %q", got, want)
	}
}

func TestSelectKeepsTheDefaultBranch(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		split bool
		want  string
	}{
		{"<*|{number}_{chapter}.{ext}>", false, defaultBranch(false)},
		{"<*|{number}_{chapter}.{ext}>", true, "{number}_{chapter}.{ext}"},
		{"<{title}/{number}.{ext}|*>", false, "{title}/{number}.{ext}"},
		{"<{title}/{number}.{ext}|*>", true, defaultBranch(true)},
	} {
		got, err := Select(tc.raw, tc.split)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("Select(%q, split=%v) = %q, want %q", tc.raw, tc.split, got, tc.want)
		}
	}
}

func TestSelectLeavesATemplateWithNoGroup(t *testing.T) {
	const raw = `F:\Music\{rjcode}\{title}.{ext}`

	for _, split := range []bool{false, true} {
		got, err := Select(raw, split)
		if err != nil {
			t.Fatal(err)
		}
		if got != raw {
			t.Errorf("Select(split=%v) = %q, want it unchanged", split, got)
		}
	}
}

// | divides the branches, so a drive letter in either one is no longer the
// ambiguity a : would have been.
func TestSelectBranchesMayBeRooted(t *testing.T) {
	const raw = "<C:/Audio/{rjcode}_{number}.{ext}|D:/Audio/{number}_{chapter}.{ext}>"

	got, err := Select(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	if want := "D:/Audio/{number}_{chapter}.{ext}"; got != want {
		t.Errorf("Select = %q, want %q", got, want)
	}
}

func TestSelectResolvesEveryGroup(t *testing.T) {
	const raw = "{year}/<{rjcode}|{date}>/<{number}.{ext}|{number}_{chapter}.{ext}>"

	got, err := Select(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	if want := "{year}/{date}/{number}_{chapter}.{ext}"; got != want {
		t.Errorf("Select = %q, want %q", got, want)
	}
}

func TestSelectTrimsAroundBranches(t *testing.T) {
	got, err := Select("< {title}.{ext} | {chapter}.{ext} >", false)
	if err != nil {
		t.Fatal(err)
	}
	if want := "{title}.{ext}"; got != want {
		t.Errorf("Select = %q, want %q", got, want)
	}
}

func TestSelectRejectsBadGroups(t *testing.T) {
	for _, raw := range []string{
		"<{a}.{ext}|{b}.{ext}|{c}.{ext}>",  // three branches
		"<{a}.{ext}<{b}.{ext}|{c}.{ext}>>", // nested
		"<{a}.{ext}|{b}.{ext}",             // unclosed <
		"{a}.{ext}|{b}.{ext}",              // | outside a group
		"{a}.{ext}>",                       // > closing nothing
		"<*|*>",                            // both sides default
		"<|{b}.{ext}>",                     // empty branch
		"<{a}.{ext}|>",                     // empty branch
		"<{a}.{ext}| >",                    // whitespace branch
		"{year}/<*|{chapter}.{ext}>",       // * inside a longer template
		"{title} * {number}.{ext}",         // stray *
		"<{title}*.{ext}|{chapter}.{ext}>", // stray * in a branch
	} {
		for _, split := range []bool{false, true} {
			if _, err := Select(raw, split); err == nil {
				t.Errorf("Select(%q, split=%v) succeeded, want an error", raw, split)
			}
		}
	}
}

// A group resolves to a template Parse reads like any other.
func TestSelectedBranchParses(t *testing.T) {
	raw, err := Select("{year}/<{rjcode}_{number}.{ext}|{number}_{chapter}.{ext}>", true)
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}

	f := album()
	f.Number, f.Total, f.Chapter, f.Ext = 3, 12, "耳かき", "m4a"
	if got, want := tmpl.File(f), "03_耳かき.m4a"; got != want {
		t.Errorf("File = %q, want %q", got, want)
	}
}

func TestWithNumberLeavesAnExplicitNumber(t *testing.T) {
	for _, raw := range []string{
		"{year}/{rjcode}_{number}.{ext}",
		"{year}/{chapter}_{NUMBER}.{ext}",
		"{year}/{ number }.{ext}",
	} {
		for _, prefix := range []bool{false, true} {
			if got := WithNumber(raw, prefix); got != raw {
				t.Errorf("WithNumber(%q, %v) = %q, want it unchanged", raw, prefix, got)
			}
		}
	}
}

func TestWithNumberLeadsAChapterFile(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"{year}/{title}/{chapter}.{ext}", "{year}/{title}/{number}_{chapter}.{ext}"},
		{"{title} - {chapter}.{ext}", "{number}_{title} - {chapter}.{ext}"},
		{`F:\Music\{chapter}.mp3`, `F:\Music\{number}_{chapter}.mp3`},
	} {
		if got := WithNumber(tc.raw, true); got != tc.want {
			t.Errorf("WithNumber(%q, true) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestWithNumberTrailsATrackFile(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"{year}/{title}/{rjcode}.{ext}", "{year}/{title}/{rjcode}_{number}.{ext}"},
		{"{title}.mp3", "{title}_{number}.mp3"},
		{"{rjcode}. {title}.{ext}", "{rjcode}. {title}_{number}.{ext}"},
		{"{title}{ext}", "{title}_{number}{ext}"},
		{"{rjcode}/{title}.{ext}/", "{rjcode}/{title}_{number}.{ext}/"},
	} {
		if got := WithNumber(tc.raw, false); got != tc.want {
			t.Errorf("WithNumber(%q, false) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

// A counter never opens the filename with a separator, however little precedes
// the extension.
func TestWithNumberNeverLeadsWithASeparator(t *testing.T) {
	for _, raw := range []string{"{ext}", ".mp3", "{year}/{ext}"} {
		got := WithNumber(raw, false)
		if start, _ := filenameBounds(got); got[start] == '_' {
			t.Errorf("WithNumber(%q, false) = %q, which opens with %q", raw, got, "_")
		}
	}
}

// Every file of a post has to land on a name of its own, whatever -o said.
func TestNumberedTemplateNamesEveryFileApart(t *testing.T) {
	for _, raw := range []string{
		"{title}.{ext}",
		"{year}/{rjcode}/{circle}.{ext}",
		"{chapter}.{ext}",
	} {
		for _, prefix := range []bool{false, true} {
			tmpl, err := Parse(WithNumber(raw, prefix))
			if err != nil {
				t.Fatal(err)
			}

			seen := map[string]bool{}
			for i := 1; i <= 12; i++ {
				f := album()
				f.Number, f.Total, f.Ext = i, 12, "mp3"
				f.Chapter = "same title every time"

				name := tmpl.File(f)
				if seen[name] {
					t.Fatalf("%q (prefix=%v) wrote %q twice", raw, prefix, name)
				}
				seen[name] = true
			}
		}
	}
}
