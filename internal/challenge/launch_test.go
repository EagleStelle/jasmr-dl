package challenge

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// A caller's flag comes after the defaults, so it wins where the two disagree.
func TestBrowserArgsPutsCallerFlagsLast(t *testing.T) {
	args := browserArgs(Options{Args: ContainerArgs}, "profile", "")

	sandbox := slices.Index(args, "--no-sandbox")
	if sandbox < 0 {
		t.Fatalf("args = %v, want --no-sandbox", args)
	}
	if last := len(args) - 1; args[last] != "about:blank" {
		t.Errorf("args end with %q, want about:blank", args[last])
	}
	if headless := slices.Index(args, "--headless=new"); headless > sandbox {
		t.Errorf("--headless=new comes after a caller's flag in %v", args)
	}
}

func TestProfileMakesAThrowawayWhenUnnamed(t *testing.T) {
	dir, cleanup, err := profile("")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("profile dir %q is relative; Chrome exits on one", dir)
	}

	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("%q survived cleanup", dir)
	}
}

func TestProfileKeepsANamedDirectory(t *testing.T) {
	want := t.TempDir()
	dir, cleanup, err := profile(want)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()

	if _, err := os.Stat(dir); err != nil {
		t.Errorf("a named profile directory was removed: %v", err)
	}
}
