package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/EagleStelle/jasmr-dl/internal/app"
	"github.com/EagleStelle/jasmr-dl/internal/session"
)

const cookieFileName = "cookies.txt"

// profileDirName is kept in the state directory so a cleared challenge counts
// next time.
const profileDirName = "browser-profile"

func runDownload(cmd *cobra.Command, args []string) error {
	raw := args
	if opts.batchFile != "" {
		listed, err := readBatchFile(cmd, opts.batchFile)
		if err != nil {
			return err
		}
		raw = slices.Concat(args, listed)
	}

	targets, err := app.ParseTargets(raw, func(target string) {
		debugf(cmd, "%s is listed more than once, fetching it once", target)
	})
	if err != nil {
		return err
	}

	cfg, err := config(cmd, targets)
	if err != nil {
		return err
	}

	// One Ctrl+C cancels every in-flight request cleanly.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	_, err = app.Run(ctx, cfg)
	return err
}

// config turns the flags into one run.
func config(cmd *cobra.Command, targets []string) (app.Config, error) {
	dir := session.DefaultDir()
	store := session.FileStore{Dir: dir, Fallbacks: legacyCookieFiles()}

	cfg := app.Config{
		Targets:     targets,
		BasePath:    opts.basePath,
		Concurrency: opts.concurrency,
		Connections: opts.connections,
		Retries:     opts.retries,
		NoMetadata:  opts.noMetadata,
		NoCover:    opts.noCover,
		NoImages:    opts.noImages,
		NoChapters:  opts.noChapters,
		NoSplit:     opts.noSplit,
		Store:       store,
		Challenge: app.ChallengeOptions{
			Enabled:     true,
			BrowserPath: opts.browserPath,
			ProfileDir:  filepath.Join(dir, profileDirName),
			Visible:     opts.showBrowser,
		},
		Stdout:   cmd.OutOrStdout(),
		Stderr:   cmd.ErrOrStderr(),
		Progress: cmd.OutOrStdout(),
		Log:      func(format string, args ...any) { debugf(cmd, format, args...) },
	}

	if cmd.Flags().Changed("output") {
		cfg.Template = strings.TrimSpace(opts.outputTmpl)
		if cfg.Template == "" {
			return cfg, errors.New("output template is empty")
		}
	}

	if opts.cookieFile != "" {
		clearance, err := session.ReadFile(opts.cookieFile)
		if err != nil {
			return cfg, fmt.Errorf("read cookies: %w", err)
		}
		debugf(cmd, "cookies from %s", opts.cookieFile)
		cfg.Clearance = clearance
	}
	return cfg, nil
}

// legacyCookieFiles are where earlier releases left a cookies.txt, read but
// never written.
func legacyCookieFiles() []string {
	paths := []string{cookieFileName}
	if exe, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(exe), cookieFileName))
	}
	return paths
}

// readBatchFile reads one URL per line, ignoring blank lines and any line that
// opens with a #. A path of - reads the list from standard input.
func readBatchFile(cmd *cobra.Command, path string) ([]string, error) {
	var (
		data []byte
		err  error
	)
	if path == "-" {
		data, err = io.ReadAll(cmd.InOrStdin())
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("read batch file: %w", err)
	}

	var urls []string
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		// At the start of a line only: a # inside a URL is a fragment, not a
		// remark.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		urls = append(urls, line)
	}
	return urls, nil
}

// stderrMu hands stderr over one line at a time.
var stderrMu sync.Mutex

func debugf(cmd *cobra.Command, format string, args ...any) {
	if !opts.verbose {
		return
	}
	stderrMu.Lock()
	defer stderrMu.Unlock()
	cmd.PrintErrf("[debug] "+format+"\n", args...)
}
