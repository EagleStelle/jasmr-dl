package cmd

import (
	"path/filepath"
	"testing"
)

func TestUnderBasePath(t *testing.T) {
	t.Cleanup(func() { basePath = "" })

	basePath = ""
	if got, want := underBasePath("2024"), "2024"; got != want {
		t.Errorf("without -P: %q, want %q", got, want)
	}

	basePath = filepath.Join("out", "audio")
	if got, want := underBasePath("2024"), filepath.Join("out", "audio", "2024"); got != want {
		t.Errorf("with -P: %q, want %q", got, want)
	}
}

// A template naming its own root already says where it goes.
func TestUnderBasePathLeavesARootedTemplate(t *testing.T) {
	t.Cleanup(func() { basePath = "" })
	basePath = "out"

	for _, dir := range []string{
		string(filepath.Separator) + filepath.Join("srv", "audio"),
		"C:" + string(filepath.Separator) + "Audio",
		"C:",
	} {
		if got := underBasePath(dir); got != dir {
			t.Errorf("underBasePath(%q) = %q, want it untouched", dir, got)
		}
	}
}
