package cmd

import (
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"jasmr-dl/internal/downloader"
	"jasmr-dl/internal/scraper"
	"jasmr-dl/internal/util"
)

const targetHost = "japaneseasmr.com"

func runGet(cmd *cobra.Command, args []string) error {
	target, err := parseAlbumURL(args[0])
	if err != nil {
		return err
	}
	if concurrency < 1 {
		return fmt.Errorf("--concurrency must be at least 1, got %d", concurrency)
	}
	if retries < 0 {
		return fmt.Errorf("--retries cannot be negative, got %d", retries)
	}
	m := scraper.Mode(mode)
	if !m.Valid() {
		return fmt.Errorf("--mode must be 0 (combined), 1 (split) or 2 (both), got %d", mode)
	}

	// One Ctrl+C cancels every in-flight request cleanly.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	sc := scraper.New(downloader.NewClient(), userAgent)
	cmd.Printf("[info] fetching %s\n", target)
	debugf(cmd, "two requests, spaced by the %s robots.txt crawl delay", scraper.PoliteDelay)

	album, err := sc.Album(ctx, target.String())
	if err != nil {
		return err
	}

	tracks := album.Select(m)

	// Not every listing carries a whole-work file. Falling back beats
	// exiting with nothing when the default mode finds none.
	if len(tracks) == 0 && m == scraper.ModeCombined {
		cmd.PrintErrln("[warn] no combined file in this listing, falling back to split tracks")
		m = scraper.ModeSplit
		tracks = album.Select(m)
	}

	cmd.Printf("[info] %d of %s selected\n", len(tracks), plural(len(album.Tracks), "file"))
	debugf(cmd, "cover: %s", orNone(album.CoverURL))
	for _, t := range tracks {
		debugf(cmd, "%s: %s", t.Title, t.LinkURL)
	}

	if len(tracks) == 0 {
		return fmt.Errorf("no files to download in mode %d (%s)", mode, m)
	}

	dir := outputDirFor(album.Title)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	cmd.Printf("[info] saving to %s\n", dir)

	jobs := make([]downloader.Job, 0, len(tracks))
	for _, t := range tracks {
		jobs = append(jobs, downloader.Job{Name: t.Title, LinkURL: t.LinkURL})
	}

	prog := downloader.NewProgress(cmd.OutOrStdout())
	d := &downloader.Downloader{
		Client:     downloader.NewClient(),
		UserAgent:  userAgent,
		OutputDir:  dir,
		Retries:    retries,
		OnStart:    prog.Start,
		OnProgress: prog.Update,
	}

	debugf(cmd, "concurrency %d, %d retries per file", concurrency, retries)
	results := d.Run(ctx, jobs, concurrency)

	// Drain the renderer before printing the summary, or the two fight over
	// the same lines.
	prog.Wait()

	return report(cmd, results)
}

// report lists the failures and returns an error only if every file failed.
func report(cmd *cobra.Command, results []downloader.Result) error {
	var failed int
	for _, r := range results {
		if r.Err != nil {
			failed++
			cmd.PrintErrf("[error] %s: %v\n", r.Job.Name, r.Err)
		}
	}

	switch {
	case failed == len(results):
		return fmt.Errorf("all %s failed", plural(failed, "download"))
	case failed > 0:
		cmd.Printf("[done] %d of %s\n", len(results)-failed, plural(len(results), "file"))
	default:
		cmd.Printf("[done] %s\n", plural(len(results), "file"))
	}
	return nil
}

// debugf writes a line only under --verbose. It goes to stderr so that piping
// stdout stays clean.
func debugf(cmd *cobra.Command, format string, args ...any) {
	if verbose {
		cmd.PrintErrf("[debug] "+format+"\n", args...)
	}
}

// plural renders a count with its noun, so no line has to say "file(s)".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// parseAlbumURL rejects anything that is not an http(s) URL on the target host.
// Matching on the parsed hostname, not a substring, keeps
// japaneseasmr.com.evil.com from passing.
func parseAlbumURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("malformed URL %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("URL must be http or https, got %q", raw)
	}
	host := strings.ToLower(u.Hostname())
	if host != targetHost && host != "www."+targetHost {
		return nil, fmt.Errorf("not a %s URL: %q", targetHost, raw)
	}
	return u, nil
}

// outputDirFor builds the destination directory. The album title is untrusted
// page content, so it is sanitized into a single path component rather than
// pasted into a path directly.
func outputDirFor(title string) string {
	if outputDir != "" {
		return outputDir
	}
	return filepath.Join(".", util.Sanitize(title))
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}
