package cmd

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func batchFile(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "urls.txt")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadBatchFile(t *testing.T) {
	path := batchFile(t, "# a list\n\nhttps://japaneseasmr.com/12346/\n  https://japaneseasmr.com/12347/  \n\n")

	got, err := readBatchFile(&cobra.Command{}, path)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"https://japaneseasmr.com/12346/", "https://japaneseasmr.com/12347/"}
	if !slices.Equal(got, want) {
		t.Errorf("urls = %v, want %v", got, want)
	}
}

func TestReadBatchFileFromStdin(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("https://japaneseasmr.com/12345/\n"))

	got, err := readBatchFile(cmd, "-")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"https://japaneseasmr.com/12345/"}; !slices.Equal(got, want) {
		t.Errorf("urls = %v, want %v", got, want)
	}
}

// A list of nothing but remarks names no post, which the run has to say rather
// than downloading nothing and calling it a success.
func TestRunDownloadRefusesAnEmptyBatchFile(t *testing.T) {
	opts.batchFile = batchFile(t, "# nothing here\n\n")
	t.Cleanup(func() { opts.batchFile = "" })

	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	if err := runDownload(cmd, nil); err == nil {
		t.Error("an empty batch file was accepted, want an error")
	}
}
