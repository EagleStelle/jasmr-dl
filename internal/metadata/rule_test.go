package metadata

import (
	"strings"
	"testing"
)

func sample() Fields {
	return Fields{
		Title:  "CV.鈴木 - ある夏の日",
		RJCode: "RJ123456",
		Circle: "サークル",
		Artist: "声優",
		Date:   "2024-05-01",
		Genre:  "ASMR",
	}
}

func apply(t *testing.T, f Fields, raws ...string) Fields {
	t.Helper()

	rules, err := ParseRules(raws)
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range rules {
		f, _ = rule.Apply(f)
	}
	return f
}

func TestRuleSplitsOneFieldIntoTwo(t *testing.T) {
	f := apply(t, sample(), "{title}:{artist} - {title}")

	if f.Artist != "CV.鈴木" || f.Title != "ある夏の日" {
		t.Errorf("artist = %q, title = %q, want the two halves of the post title", f.Artist, f.Title)
	}
}

// The rules run in the order they were given, each reading what the one before
// it left.
func TestRulesRunInOrder(t *testing.T) {
	f := apply(t, sample(),
		"{title}:{artist} - {title}",
		"{artist}:CV.{artist}",
	)

	if f.Artist != "鈴木" {
		t.Errorf("artist = %q, want the second rule applied over the first", f.Artist)
	}
}

// A rule written for the posts it fits leaves the rest as they stand.
func TestRuleThatMatchesNothingChangesNothing(t *testing.T) {
	rules, err := ParseRules([]string{`{title}:^Vol\. (?P<title>.+)$`})
	if err != nil {
		t.Fatal(err)
	}

	got, ok := rules[0].Apply(sample())
	if ok {
		t.Error("a rule matching nothing reported a match")
	}
	if got != sample() {
		t.Errorf("fields = %+v, want them untouched", got)
	}
}

// The album is the title with its RJ code, so a rule that renames the title
// renames the album with it. One that names the album itself wins.
func TestAlbumFollowsTheTitleUnlessARuleNamesIt(t *testing.T) {
	f := apply(t, sample(), "{title}:{artist} - {title}")
	if got, want := f.AlbumName(), "ある夏の日 [RJ123456]"; got != want {
		t.Errorf("album = %q, want %q", got, want)
	}

	f = apply(t, f, "{title}:(?P<album>.+)")
	if got, want := f.AlbumName(), "ある夏の日"; got != want {
		t.Errorf("album = %q, want the one the rule named, %q", got, want)
	}
}

// A post carrying only one half of the album name is named for that half.
func TestAlbumNameFallsBackToWhicheverHalfIsThere(t *testing.T) {
	for _, tc := range []struct {
		f    Fields
		want string
	}{
		{Fields{Title: "ある夏の日", RJCode: "RJ123456"}, "ある夏の日 [RJ123456]"},
		{Fields{Title: "ある夏の日"}, "ある夏の日"},
		{Fields{RJCode: "RJ123456"}, "RJ123456"},
		{Fields{}, ""},
	} {
		if got := tc.f.AlbumName(); got != tc.want {
			t.Errorf("AlbumName(%+v) = %q, want %q", tc.f, got, tc.want)
		}
	}
}

// A TO naming no field is the expression it looks like, so a group named the
// long way round sets its field too.
func TestRuleTakesARawExpression(t *testing.T) {
	f := apply(t, Fields{Title: "RJ654321 ある夏の日"}, `{title}:^(?P<rjcode>RJ\d+) (?P<title>.+)$`)

	if f.RJCode != "RJ654321" || f.Title != "ある夏の日" {
		t.Errorf("rjcode = %q, title = %q, want them read apart", f.RJCode, f.Title)
	}
}

// A brace holding a repeat count is the expression's own, not a field.
func TestRuleKeepsAQuantifier(t *testing.T) {
	f := apply(t, Fields{Title: "2024ある夏の日"}, `{title}:^\d{4}(?P<title>.+)$`)

	if f.Title != "ある夏の日" {
		t.Errorf("title = %q, want the year dropped", f.Title)
	}
}

// A colon of the rule's own is written \:, the first bare one dividing it.
func TestRuleKeepsAnEscapedColon(t *testing.T) {
	f := apply(t, Fields{Title: "Vol.1: ある夏の日"}, `{title}:^Vol\.1\: (?P<title>.+)$`)

	if f.Title != "ある夏の日" {
		t.Errorf("title = %q, want the label dropped", f.Title)
	}
}

// A group that took no part in the match says nothing about its field. One
// that matched an empty string does, and empties it.
func TestRuleWritesOnlyWhatTookPartInTheMatch(t *testing.T) {
	f := apply(t, Fields{Title: "ある夏の日", Circle: "サークル", Artist: "声優"},
		`{title}:^(?P<title>.+?)(?P<circle>\d*)(?:x(?P<artist>.+))?$`)

	if f.Circle != "" {
		t.Errorf("circle = %q, want the empty capture to have emptied it", f.Circle)
	}
	if f.Artist != "声優" {
		t.Errorf("artist = %q, want the group that took no part to have left it", f.Artist)
	}
}

// A bare field name is that field, on either side. On the right it keeps the
// whole of what was matched.
func TestRuleTakesBareFieldNames(t *testing.T) {
	f := apply(t, Fields{Title: "ある夏の日\n二日目"}, "title:album")

	if f.Album != "ある夏の日\n二日目" {
		t.Errorf("album = %q, want the whole title, newline and all", f.Album)
	}
}

// A template's literal text is literal: a rule reaching for a regular
// expression writes the whole half as one, which is how yt-dlp divides them.
func TestRuleEscapesTheLiteralsAroundAField(t *testing.T) {
	f := apply(t, Fields{Title: "(ある夏の日)"}, "{title}:({title})")

	if f.Title != "ある夏の日" {
		t.Errorf("title = %q, want the brackets read as brackets", f.Title)
	}
}

func TestParseRuleRejectsABadRule(t *testing.T) {
	for _, tc := range []struct{ raw, wants string }{
		{"{title}", "FROM:TO"},
		{"{nope}:{title}", "unknown field {nope}"},
		{"{title}:{nope}", "unknown field {nope}"},
		{"{title}:{year}", "unknown field {year}"},
		{"{title}:(?P<year>.+)", "unknown field {year}"},
		{"{title:{title}", "unclosed {"},
		{"nothing to read:{title}", "reads no field"},
		{"{title}:(", "is not a pattern"},
	} {
		_, err := parseRule(tc.raw)
		if err == nil {
			t.Errorf("parseRule(%q) succeeded, want an error", tc.raw)
			continue
		}
		if !strings.Contains(err.Error(), tc.wants) {
			t.Errorf("parseRule(%q) said %q, want it to mention %q", tc.raw, err, tc.wants)
		}
	}
}

// A bad rule names itself, so a run with several says which one it means.
func TestParseRulesNamesTheRuleThatFailed(t *testing.T) {
	_, err := ParseRules([]string{"{title}:{title}", "{nope}:{title}"})
	if err == nil {
		t.Fatal("ParseRules accepted an unknown field, want an error")
	}
	if !strings.Contains(err.Error(), `"{nope}:{title}"`) {
		t.Errorf("error = %q, want it to quote the rule that failed", err)
	}
}
