package app

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/EagleStelle/jasmr-dl/internal/downloader"
)

// reporting runs summarize against a run whose two streams are captured.
func reporting(t *testing.T, results []PostResult, broken, posts int) (stdout, stderr string, err error) {
	t.Helper()

	var out, errBuf bytes.Buffer
	cfg, prepErr := Config{
		Targets: make([]string, posts),
		Stdout:  &out,
		Stderr:  &errBuf,
	}.prepared()
	if prepErr != nil {
		t.Fatal(prepErr)
	}

	_, err = (&run{cfg: cfg}).summarize(results, broken)
	return out.String(), errBuf.String(), err
}

func saved(paths ...string) downloader.Result {
	return downloader.Result{Job: downloader.Job{Name: "track"}, Paths: paths}
}

func lost(name string) downloader.Result {
	return downloader.Result{Job: downloader.Job{Name: name}, Err: errors.New("dead link")}
}

// A run of one post names no post, and the error names the downloads rather
// than a count of one post.
func TestSummarizeOnOnePostNamesNoPost(t *testing.T) {
	out, _, err := reporting(t, []PostResult{{
		Label:   "ある夏の日",
		Dir:     "2024/RJ123456",
		Results: []downloader.Result{saved("a.mp3"), saved("b.mp3")},
	}}, 0, 1)
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}

	want := "[done] 2 recordings saved to 2024/RJ123456\n"
	if out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

func TestSummarizeOnOnePostThatFailedEntirely(t *testing.T) {
	_, _, err := reporting(t, []PostResult{{
		Label:   "ある夏の日",
		Dir:     "2024/RJ123456",
		Results: []downloader.Result{lost("a"), lost("b")},
	}}, 0, 1)
	if err == nil {
		t.Fatal("summarize returned no error, want one")
	}
	if want := "all 2 downloads failed"; err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
}

// One dead post must not take the run down with it.
func TestSummarizeCarriesOnPastADeadPost(t *testing.T) {
	out, stderr, err := reporting(t, []PostResult{
		{Label: "ある夏の日", Dir: "2024/RJ123456", Results: []downloader.Result{saved("a.mp3"), saved("b.mp3")}},
		{Label: "夜のひととき", Dir: "2024/RJ123457", Results: []downloader.Result{lost("a")}},
		{Label: "朝の声", Dir: "2024/RJ123458", Results: []downloader.Result{saved("a.mp3")}},
	}, 0, 3)
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}

	for _, want := range []string{
		"[done] 2 recordings saved to 2024/RJ123456\n",
		"[done] 1 recording saved to 2024/RJ123458\n",
		"[done] 3 recordings from 2 posts, 1 post failed\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout does not carry %q\ngot:\n%s", want, out)
		}
	}
	if !strings.Contains(stderr, "夜のひととき: a: dead link") {
		t.Errorf("stderr does not name the failure against its post\ngot:\n%s", stderr)
	}
	if strings.Contains(out, "2024/RJ123457") {
		t.Errorf("stdout reports a directory for a post that saved nothing\ngot:\n%s", out)
	}
}

func TestSummarizeFailsOnlyWhenEveryPostDid(t *testing.T) {
	_, _, err := reporting(t, []PostResult{
		{Label: "ある夏の日", Dir: "2024/RJ123456", Results: []downloader.Result{lost("a")}},
	}, 2, 3)
	if err == nil {
		t.Fatal("summarize returned no error, want one")
	}
	if want := "all 3 posts failed"; err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
}

func TestSummarizeCountsAPostItCouldNotRead(t *testing.T) {
	out, _, err := reporting(t, []PostResult{
		{Label: "ある夏の日", Dir: "2024/RJ123456", Results: []downloader.Result{saved("a.mp3")}},
	}, 1, 2)
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if want := "[done] 1 recording from 1 post, 1 post failed\n"; !strings.Contains(out, want) {
		t.Errorf("stdout does not carry %q\ngot:\n%s", want, out)
	}
}

// The Summary carries the same facts as the printed lines, for a caller with no
// streams to read.
func TestSummarizeFillsTheSummary(t *testing.T) {
	var out bytes.Buffer
	cfg, err := Config{Targets: make([]string, 3), Stdout: &out}.prepared()
	if err != nil {
		t.Fatal(err)
	}

	s, err := (&run{cfg: cfg}).summarize([]PostResult{
		{Label: "a", Results: []downloader.Result{saved("a.mp3"), saved("b.mp3")}},
		{Label: "b", Results: []downloader.Result{lost("a")}},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if s.Posts != 3 || s.Files != 2 || s.FailedPosts != 1 {
		t.Errorf("summary = %+v, want 3 posts, 2 files, 1 failed", s)
	}
}
