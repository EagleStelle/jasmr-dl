package cmd

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// batch writes a --batch-file and points the flag at it for one test.
func batch(t *testing.T, body string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "urls.txt")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	batchFile = path
	t.Cleanup(func() { batchFile = "" })
}

func TestTargetsFromDropsRepeats(t *testing.T) {
	got, err := targetsFrom(&cobra.Command{}, []string{
		"https://japaneseasmr.com/12345/",
		"https://japaneseasmr.com/12346/",
		"https://japaneseasmr.com/12345/",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"https://japaneseasmr.com/12345/", "https://japaneseasmr.com/12346/"}
	if !slices.Equal(got, want) {
		t.Errorf("targets = %v, want %v: a repeat is fetched once, in the order given", got, want)
	}
}

// A typo must end the run before anything is fetched, not nine downloads in.
func TestTargetsFromRefusesABadURL(t *testing.T) {
	for _, raw := range []string{
		"https://evil.com/12345/",
		"japaneseasmr.com/12345/",
		"ftp://japaneseasmr.com/12345/",
	} {
		if _, err := targetsFrom(&cobra.Command{}, []string{"https://japaneseasmr.com/1/", raw}); err == nil {
			t.Errorf("targetsFrom accepted %q, want an error", raw)
		}
	}
}

func TestTargetsFromReadsABatchFile(t *testing.T) {
	batch(t, "# a list\n\nhttps://japaneseasmr.com/12346/\n  https://japaneseasmr.com/12347/  \n\n")

	got, err := targetsFrom(&cobra.Command{}, []string{"https://japaneseasmr.com/12345/"})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"https://japaneseasmr.com/12345/",
		"https://japaneseasmr.com/12346/",
		"https://japaneseasmr.com/12347/",
	}
	if !slices.Equal(got, want) {
		t.Errorf("targets = %v, want %v: the arguments, then the file", got, want)
	}
}

func TestTargetsFromReadsTheBatchFileFromStdin(t *testing.T) {
	batchFile = "-"
	t.Cleanup(func() { batchFile = "" })

	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("https://japaneseasmr.com/12345/\n"))

	got, err := targetsFrom(cmd, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"https://japaneseasmr.com/12345/"}; !slices.Equal(got, want) {
		t.Errorf("targets = %v, want %v", got, want)
	}
}

// A list of nothing but remarks names no post, which is worth saying rather
// than downloading nothing and calling it a success.
func TestTargetsFromRefusesAnEmptyBatchFile(t *testing.T) {
	batch(t, "# nothing here\n\n")

	if _, err := targetsFrom(&cobra.Command{}, nil); err == nil {
		t.Error("an empty batch file was accepted, want an error")
	}
}
