package cmd

import (
	"testing"

	"github.com/EagleStelle/jasmr-dl/internal/naming"
)

func fields(n, total int) naming.Fields {
	return naming.Fields{
		RJCode:  "RJ123456",
		Title:   "ある夏の日",
		Year:    "2024",
		Chapter: "耳かき",
		Ext:     "mp3",
		Number:  n,
		Width:   naming.Width(total),
	}
}

func TestTemplateForKeepsBothDefaults(t *testing.T) {
	for _, tc := range []struct {
		split bool
		want  string
	}{
		{false, "RJ123456_2.mp3"},
		{true, "2_耳かき.mp3"},
	} {
		tmpl, err := templateFor("", tc.split, 3)
		if err != nil {
			t.Fatal(err)
		}
		if got := tmpl.File(fields(2, 3)); got != tc.want {
			t.Errorf("split=%v: File = %q, want %q", tc.split, got, tc.want)
		}
	}
}

func TestTemplateForDividesOnTheGroup(t *testing.T) {
	const given = "{year}/<{rjcode}_{number}.{ext}|{number}_{chapter}.{ext}>"

	for _, tc := range []struct {
		split bool
		want  string
	}{
		{false, "RJ123456_2.mp3"},
		{true, "2_耳かき.mp3"},
	} {
		tmpl, err := templateFor(given, tc.split, 3)
		if err != nil {
			t.Fatal(err)
		}
		if got := tmpl.File(fields(2, 3)); got != tc.want {
			t.Errorf("split=%v: File = %q, want %q", tc.split, got, tc.want)
		}
	}
}

// A branch of * leaves that side alone, so one custom template does not force
// the other to be written out.
func TestTemplateForKeepsAStarBranchDefault(t *testing.T) {
	tmpl, err := templateFor("<*|{number}. {chapter}.{ext}>", false, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := tmpl.File(fields(2, 3)), "RJ123456_2.mp3"; got != want {
		t.Errorf("File = %q, want %q", got, want)
	}
}

// A -o naming no counter still writes every file of a post to its own name:
// leading where each file is a chapter, trailing where each is a track.
func TestTemplateForNumbersWhatTheTemplateDoesNot(t *testing.T) {
	for _, tc := range []struct {
		split bool
		files int
		want  string
	}{
		{false, 1, "ある夏の日.mp3"},
		{false, 3, "ある夏の日_2.mp3"},
		{true, 3, "2_ある夏の日.mp3"},
	} {
		tmpl, err := templateFor("{year}/{title}.{ext}", tc.split, tc.files)
		if err != nil {
			t.Fatal(err)
		}
		if got := tmpl.File(fields(2, tc.files)); got != tc.want {
			t.Errorf("split=%v files=%d: File = %q, want %q", tc.split, tc.files, got, tc.want)
		}
	}
}

// A post of one file has nothing to count, so a template naming a counter
// writes without it.
func TestTemplateForDropsTheNumberOnASingleFile(t *testing.T) {
	for _, tc := range []struct {
		given string
		want  string
	}{
		{"", "RJ123456.mp3"},
		{"{year}/{title} - {number}.{ext}", "ある夏の日.mp3"},
		{"{year}/{number}. {title}.{ext}", "ある夏の日.mp3"},
		{"{year}/{title} ({number}).{ext}", "ある夏の日.mp3"},
		{"{year}/{number}.{ext}", "1.mp3"},
	} {
		tmpl, err := templateFor(tc.given, false, 1)
		if err != nil {
			t.Fatal(err)
		}
		if got := tmpl.File(fields(1, 1)); got != tc.want {
			t.Errorf("templateFor(%q): File = %q, want %q", tc.given, got, tc.want)
		}
	}
}

func TestTemplateForRejectsABadTemplate(t *testing.T) {
	for _, given := range []string{
		"{nope}.{ext}",
		"{year}/{rjcode}",
		"<{a}.{ext}|{b}.{ext}",
		"<*|*>",
	} {
		for _, split := range []bool{false, true} {
			if _, err := templateFor(given, split, 3); err == nil {
				t.Errorf("templateFor(%q, split=%v) succeeded, want an error", given, split)
			}
		}
	}
}
