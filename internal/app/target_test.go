package app

import (
	"slices"
	"testing"
)

func TestParseTargetsDropsRepeats(t *testing.T) {
	var repeats []string
	got, err := ParseTargets([]string{
		"https://japaneseasmr.com/12345/",
		"https://japaneseasmr.com/12346/",
		"https://japaneseasmr.com/12345/",
	}, func(target string) { repeats = append(repeats, target) })
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"https://japaneseasmr.com/12345/", "https://japaneseasmr.com/12346/"}
	if !slices.Equal(got, want) {
		t.Errorf("targets = %v, want %v: a repeat is fetched once, in the order given", got, want)
	}
	if len(repeats) != 1 {
		t.Errorf("reported %d repeats, want 1", len(repeats))
	}
}

// A typo must end the run before anything is fetched, not nine downloads in.
func TestParseTargetsRefusesABadURL(t *testing.T) {
	for _, raw := range []string{
		"https://evil.com/12345/",
		"japaneseasmr.com/12345/",
		"ftp://japaneseasmr.com/12345/",
	} {
		if _, err := ParseTargets([]string{"https://japaneseasmr.com/1/", raw}, nil); err == nil {
			t.Errorf("ParseTargets accepted %q, want an error", raw)
		}
	}
}

func TestParseTargetAcceptsWWW(t *testing.T) {
	if _, err := ParseTarget("https://www.japaneseasmr.com/12345/"); err != nil {
		t.Errorf("www was refused: %v", err)
	}
}
