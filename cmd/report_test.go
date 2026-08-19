package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/EagleStelle/jasmr-dl/internal/downloader"
)

// reporting runs report against a command whose two streams are captured.
func reporting(t *testing.T, outs []outcome, broken, posts int) (stdout, stderr string, err error) {
	t.Helper()

	var out, errBuf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)

	err = report(cmd, outs, broken, posts)
	return out.String(), errBuf.String(), err
}

func saved(paths ...string) downloader.Result {
	return downloader.Result{Job: downloader.Job{Name: "track"}, Paths: paths}
}

func lost(name string) downloader.Result {
	return downloader.Result{Job: downloader.Job{Name: name}, Err: errors.New("dead link")}
}

// A run of one post reports exactly as it always did: no post named, and the
// error naming the downloads rather than a count of one post.
func TestReportOnOnePostNamesNoPost(t *testing.T) {
	out, _, err := reporting(t, []outcome{{
		label:   "ある夏の日",
		dir:     "2024/RJ123456",
		results: []downloader.Result{saved("a.mp3"), saved("b.mp3")},
	}}, 0, 1)
	if err != nil {
		t.Fatalf("report: %v", err)
	}

	want := "[done] 2 recordings saved to 2024/RJ123456\n"
	if out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

func TestReportOnOnePostThatFailedEntirely(t *testing.T) {
	_, _, err := reporting(t, []outcome{{
		label:   "ある夏の日",
		dir:     "2024/RJ123456",
		results: []downloader.Result{lost("a"), lost("b")},
	}}, 0, 1)
	if err == nil {
		t.Fatal("report returned no error, want one")
	}
	if want := "all 2 downloads failed"; err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
}

// One dead post must not take the run down with it, and the posts that worked
// have to be identifiable once there is more than one.
func TestReportCarriesOnPastADeadPost(t *testing.T) {
	out, stderr, err := reporting(t, []outcome{
		{label: "ある夏の日", dir: "2024/RJ123456", results: []downloader.Result{saved("a.mp3"), saved("b.mp3")}},
		{label: "夜のひととき", dir: "2024/RJ123457", results: []downloader.Result{lost("a")}},
		{label: "朝の声", dir: "2024/RJ123458", results: []downloader.Result{saved("a.mp3")}},
	}, 0, 3)
	if err != nil {
		t.Fatalf("report: %v", err)
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
	// The dead post saved nothing, so it has no directory worth reporting.
	if strings.Contains(out, "2024/RJ123457") {
		t.Errorf("stdout reports a directory for a post that saved nothing\ngot:\n%s", out)
	}
}

// Nothing saved anywhere is the only thing that makes the run itself a failure.
func TestReportFailsOnlyWhenEveryPostDid(t *testing.T) {
	_, _, err := reporting(t, []outcome{
		{label: "ある夏の日", dir: "2024/RJ123456", results: []downloader.Result{lost("a")}},
	}, 2, 3) // two more never got as far as a download
	if err == nil {
		t.Fatal("report returned no error, want one")
	}
	if want := "all 3 posts failed"; err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
}

// A post that could not be read at all still counts, and the ones that worked
// still report.
func TestReportCountsAPostItCouldNotRead(t *testing.T) {
	out, _, err := reporting(t, []outcome{
		{label: "ある夏の日", dir: "2024/RJ123456", results: []downloader.Result{saved("a.mp3")}},
	}, 1, 2)
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if want := "[done] 1 recording from 1 post, 1 post failed\n"; !strings.Contains(out, want) {
		t.Errorf("stdout does not carry %q\ngot:\n%s", want, out)
	}
}
