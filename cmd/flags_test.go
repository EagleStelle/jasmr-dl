package cmd

import (
	"testing"

	"github.com/spf13/cobra"

	"github.com/EagleStelle/jasmr-dl/internal/app"
	"github.com/EagleStelle/jasmr-dl/internal/session"
)

// runConfig parses args against a fresh command in a state directory of its
// own, then builds the run they describe.
func runConfig(t *testing.T, targets []string, args ...string) app.Config {
	t.Helper()

	t.Setenv(session.DirEnv, t.TempDir())
	t.Setenv(noConfigEnv, "1")
	t.Chdir(t.TempDir())

	var o options
	cmd := &cobra.Command{Use: "jasmr-dl"}
	registerFlags(cmd.PersistentFlags(), &o)
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatal(err)
	}
	if err := applyConfig(cmd); err != nil {
		t.Fatal(err)
	}

	cfg, err := config(cmd, &o, targets)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// Nothing is written into a file, or beside it, unless the run asks.
func TestNothingIsWrittenByDefault(t *testing.T) {
	cfg := runConfig(t, []string{"https://japaneseasmr.com/12345/"})

	for _, tc := range []struct {
		name string
		on   bool
	}{
		{"write-info-json", cfg.WriteInfoJSON},
		{"write-cover", cfg.WriteCover},
		{"write-images", cfg.WriteImages},
		{"write-nfo", cfg.WriteNFO},
		{"embed-metadata", cfg.EmbedMetadata},
		{"embed-cover", cfg.EmbedCover},
		{"embed-chapters", cfg.EmbedChapters},
		{"split-chapters", cfg.SplitChapters},
	} {
		if tc.on {
			t.Errorf("--%s is on with nothing asking for it", tc.name)
		}
	}
}

func TestFlagsReachTheRun(t *testing.T) {
	cfg := runConfig(t, []string{"https://japaneseasmr.com/12345/"},
		"--write-info-json", "--write-cover", "--write-images", "--write-nfo",
		"--embed-metadata", "--embed-cover", "--embed-chapters", "--split-chapters")

	if !cfg.WriteInfoJSON || !cfg.WriteCover || !cfg.WriteImages || !cfg.WriteNFO {
		t.Errorf("the write flags did not reach the run: %+v", cfg)
	}
	if !cfg.EmbedMetadata || !cfg.EmbedCover || !cfg.EmbedChapters || !cfg.SplitChapters {
		t.Errorf("the embed flags did not reach the run: %+v", cfg)
	}
}

// A boolean set outside the command line is turned off again with =false.
func TestAFlagIsTurnedOffAgain(t *testing.T) {
	t.Setenv("JASMR_DL_EMBED_METADATA", "true")

	if cfg := runConfig(t, []string{"https://japaneseasmr.com/12345/"}); !cfg.EmbedMetadata {
		t.Error("the environment did not turn embed-metadata on")
	}

	t.Setenv("JASMR_DL_EMBED_METADATA", "true")
	cfg := runConfig(t, []string{"https://japaneseasmr.com/12345/"}, "--embed-metadata=false")
	if cfg.EmbedMetadata {
		t.Error("--embed-metadata=false did not beat the environment")
	}
}

// A record names everything it carries, so applying one writes all of it. The
// run narrows that only by naming what it wants.
func TestLoadInfoJSONWritesEverythingUnlessNarrowed(t *testing.T) {
	const path = "RJ123456/RJ123456.info.json"

	for _, tc := range []struct {
		name                      string
		args                      []string
		metadata, cover, chapters bool
	}{
		{"on its own", nil, true, true, true},
		{"narrowed to chapters", []string{"--embed-chapters"}, false, false, true},
		{"narrowed to metadata and art", []string{"--embed-metadata", "--embed-cover"}, true, true, false},
	} {
		args := append([]string{"--load-info-json", path}, tc.args...)
		cfg := runConfig(t, nil, args...)

		if cfg.LoadInfoJSON != path {
			t.Errorf("%s: record = %q, want %q", tc.name, cfg.LoadInfoJSON, path)
		}
		if cfg.EmbedMetadata != tc.metadata || cfg.EmbedCover != tc.cover || cfg.EmbedChapters != tc.chapters {
			t.Errorf("%s: metadata=%v cover=%v chapters=%v, want %v %v %v",
				tc.name, cfg.EmbedMetadata, cfg.EmbedCover, cfg.EmbedChapters,
				tc.metadata, tc.cover, tc.chapters)
		}
	}
}

// A config file naming an embed flag is the run saying what it wants, as much
// as typing it is.
func TestAConfigFileNarrowsARecordToo(t *testing.T) {
	t.Setenv("JASMR_DL_EMBED_CHAPTERS", "true")

	cfg := runConfig(t, nil, "--load-info-json", "RJ123456.info.json")
	if cfg.EmbedMetadata || cfg.EmbedCover {
		t.Errorf("metadata=%v cover=%v, want only the chapters the environment named",
			cfg.EmbedMetadata, cfg.EmbedCover)
	}
	if !cfg.EmbedChapters {
		t.Error("the environment's embed-chapters did not reach the run")
	}
}
