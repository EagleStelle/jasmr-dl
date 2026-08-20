package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/EagleStelle/jasmr-dl/internal/session"
)

// configured parses args against a fresh command, then applies the config, in
// a working directory and state directory of its own.
func configured(t *testing.T, global, local string, args ...string) (*options, error) {
	t.Helper()

	dir := t.TempDir()
	t.Setenv(session.DirEnv, dir)
	if global != "" {
		write(t, filepath.Join(dir, configName), global)
	}

	work := t.TempDir()
	t.Chdir(work)
	if local != "" {
		write(t, filepath.Join(work, localName), local)
	}

	var o options
	cmd := &cobra.Command{Use: "jasmr-dl"}
	registerFlags(cmd.PersistentFlags(), &o)
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatal(err)
	}
	return &o, applyConfig(cmd)
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestConfigFileSetsFlags(t *testing.T) {
	o, err := configured(t, "", "# a config\n\nconcurrency = 8\nno-cover\noutput = {year}/{title}.{ext}\n")
	if err != nil {
		t.Fatal(err)
	}
	if o.concurrency != 8 {
		t.Errorf("concurrency = %d, want 8", o.concurrency)
	}
	if !o.noCover {
		t.Error("no-cover was not set by a bare key")
	}
	if want := "{year}/{title}.{ext}"; o.outputTmpl != want {
		t.Errorf("output = %q, want %q", o.outputTmpl, want)
	}
}

func TestCommandLineBeatsConfigFile(t *testing.T) {
	o, err := configured(t, "", "concurrency = 8\n", "--concurrency", "2")
	if err != nil {
		t.Fatal(err)
	}
	if o.concurrency != 2 {
		t.Errorf("concurrency = %d, want the 2 typed on the command line", o.concurrency)
	}
}

func TestEnvironmentBeatsConfigFile(t *testing.T) {
	t.Setenv("JASMR_DL_CONCURRENCY", "5")

	o, err := configured(t, "", "concurrency = 8\n")
	if err != nil {
		t.Fatal(err)
	}
	if o.concurrency != 5 {
		t.Errorf("concurrency = %d, want the environment's 5", o.concurrency)
	}
}

func TestCommandLineBeatsEnvironment(t *testing.T) {
	t.Setenv("JASMR_DL_CONCURRENCY", "5")

	o, err := configured(t, "", "", "--concurrency", "2")
	if err != nil {
		t.Fatal(err)
	}
	if o.concurrency != 2 {
		t.Errorf("concurrency = %d, want 2", o.concurrency)
	}
}

func TestWorkingDirectoryBeatsStateDirectory(t *testing.T) {
	o, err := configured(t, "concurrency = 8\nretries = 9\n", "concurrency = 4\n")
	if err != nil {
		t.Fatal(err)
	}
	if o.concurrency != 4 {
		t.Errorf("concurrency = %d, want the local file's 4", o.concurrency)
	}
	if o.retries != 9 {
		t.Errorf("retries = %d, want the state directory's 9", o.retries)
	}
}

func TestConfigAcceptsDashesAndQuotes(t *testing.T) {
	o, err := configured(t, "", "--paths = \" out \"\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := " out "; o.basePath != want {
		t.Errorf("paths = %q, want %q", o.basePath, want)
	}
}

func TestConfigRejectsAnUnknownSetting(t *testing.T) {
	if _, err := configured(t, "", "concurrancy = 8\n"); err == nil {
		t.Error("an unknown setting was accepted, want an error")
	}
}

func TestConfigRejectsAValuelessNonBool(t *testing.T) {
	if _, err := configured(t, "", "output\n"); err == nil {
		t.Error("a valueless string setting was accepted, want an error")
	}
}

func TestNoConfigEnvSkipsTheFiles(t *testing.T) {
	t.Setenv(noConfigEnv, "1")

	o, err := configured(t, "", "concurrency = 8\n")
	if err != nil {
		t.Fatal(err)
	}
	if o.concurrency != 3 {
		t.Errorf("concurrency = %d, want the default 3", o.concurrency)
	}
}
