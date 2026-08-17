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

var getCmd = &cobra.Command{
	Use:   "get <url>",
	Short: "Download every track from a japaneseasmr.com album page",
	Args:  cobra.ExactArgs(1),
	RunE:  runGet,
}

func init() {
	rootCmd.AddCommand(getCmd)
}

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

	// One Ctrl+C cancels every in-flight request cleanly.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	sc := scraper.New(downloader.NewClient(), userAgent)
	if verbose {
		cmd.Printf("fetching %s\n", target)
		cmd.Printf("(two requests, spaced by the %s robots.txt crawl delay)\n", scraper.PoliteDelay)
	}

	album, err := sc.Album(ctx, target.String())
	if err != nil {
		return err
	}

	printAlbum(cmd, album)

	tracks := album.Downloadable(combined)
	if len(tracks) == 0 {
		return fmt.Errorf("nothing to download")
	}

	dir := outputDirFor(album.Title)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	jobs := make([]downloader.Job, 0, len(tracks))
	for _, t := range tracks {
		jobs = append(jobs, downloader.Job{Name: t.Title, LinkURL: t.LinkURL})
	}

	bars := downloader.NewBarSet(cmd.OutOrStdout())
	d := &downloader.Downloader{
		Client:     downloader.NewClient(),
		UserAgent:  userAgent,
		OutputDir:  dir,
		Retries:    retries,
		OnStart:    bars.Start,
		OnProgress: bars.Update,
	}

	cmd.Printf("\ndownloading %d file(s) into %s with concurrency %d\n\n", len(jobs), dir, concurrency)
	results := d.Run(ctx, jobs, concurrency)

	// Drain the renderer before printing the summary, or the two fight over
	// the same lines.
	bars.Wait()

	return report(cmd, results)
}

// report prints a per-file summary and fails only if every file failed.
func report(cmd *cobra.Command, results []downloader.Result) error {
	var failed int
	cmd.Println()
	for _, r := range results {
		if r.Err != nil {
			failed++
			cmd.PrintErrf("  ✗ %s: %v\n", r.Job.Name, r.Err)
			continue
		}
		cmd.Printf("  ✓ %s\n", filepath.Base(r.Path))
	}

	switch {
	case failed == 0:
		cmd.Printf("\ndone: %d file(s)\n", len(results))
		return nil
	case failed == len(results):
		return fmt.Errorf("all %d download(s) failed", failed)
	default:
		cmd.Printf("\ndone: %d ok, %d failed\n", len(results)-failed, failed)
		return nil
	}
}

func humanSize(n int64) string {
	if n < 0 {
		return "unknown size"
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}

func printAlbum(cmd *cobra.Command, a *scraper.Album) {
	cmd.Printf("\ntitle:  %s\n", a.Title)
	cmd.Printf("rj:     %s\n", a.RJCode)
	cmd.Printf("cover:  %s\n", orNone(a.CoverURL))
	cmd.Printf("output: %s\n", outputDirFor(a.Title))

	tracks := a.Downloadable(combined)
	cmd.Printf("\ntracks: %d of %d listed\n", len(tracks), len(a.Tracks))
	for _, t := range tracks {
		label := fmt.Sprintf("%02d", t.Index)
		if t.Combined {
			label = "--"
		}
		cmd.Printf("  [%s] %s\n", label, t.Title)
		if verbose {
			cmd.Printf("       %s\n", t.LinkURL)
		}
	}
	if !combined {
		cmd.Printf("\n(combined whole-work file skipped; pass --combined to include it)\n")
	}
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
