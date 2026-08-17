package cmd

import (
	"context"
	"fmt"
	"net/http"
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

func runDownload(cmd *cobra.Command, args []string) error {
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

	// One client for the run: its connection pool is what keeps sockets warm
	// across the page fetch, the cover and every track.
	client := downloader.NewClient()
	sc := scraper.New(client, userAgent)
	cmd.Printf("[info] fetching %s\n", target)

	album, err := sc.Album(ctx, target.String())
	if err != nil {
		return err
	}
	reportSource(cmd, album)

	cmd.Printf("[info] %s to download\n", plural(len(album.Tracks), "file"))
	debugf(cmd, "cover: %s", orNone(album.CoverURL))
	debugf(cmd, "chapters: %d", len(album.Chapters))
	for _, t := range album.Tracks {
		debugf(cmd, "%s: %s", t.Title, t.LinkURL)
	}

	dir := outputDirFor(album.Title)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	cmd.Printf("[info] saving to %s\n", dir)

	jobs := make([]downloader.Job, 0, len(album.Tracks))
	for _, t := range album.Tracks {
		jobs = append(jobs, downloader.Job{
			Name:       t.Title,
			LinkURL:    t.LinkURL,
			Source:     t.Source,
			Alternates: t.Alternates,
			Referer:    album.PageURL,
		})
	}

	prog := downloader.NewProgress(cmd.OutOrStdout())
	d := &downloader.Downloader{
		Client:       client,
		UserAgent:    userAgent,
		OutputDir:    dir,
		Retries:      retries,
		FFmpeg:       ffmpegPath,
		CoverPath:    fetchCover(ctx, cmd, client, album, dir),
		Chapters:     chaptersFor(cmd, album),
		Tags:         tagsFor(album),
		OnCoverError: func(name string, err error) { cmd.PrintErrf("[warn] %s: %v\n", name, err) },
		OnStart:      prog.Start,
		OnProgress:   prog.Update,
		OnStartCount: prog.StartCount,
	}

	debugf(cmd, "%d files at once, %d retries per file", concurrency, retries)
	results := d.Run(ctx, jobs, concurrency)

	// Drain the renderer first, or it fights the summary for the same lines.
	prog.Wait()

	return report(cmd, results)
}

// tagsFor builds the metadata written into every file.
func tagsFor(album *scraper.Album) downloader.Tags {
	if noTags {
		return downloader.Tags{}
	}
	return downloader.Tags{
		Title:   album.Title,
		Artist:  album.Artists,
		Album:   album.Title,
		Comment: album.PageURL,
	}
}

// chaptersFor returns chapters only when one file holds the whole work.
func chaptersFor(cmd *cobra.Command, album *scraper.Album) []scraper.Chapter {
	if noChapters || len(album.Chapters) == 0 {
		return nil
	}
	if len(album.Tracks) != 1 {
		debugf(cmd, "%d chapters ignored: the work is already split across files", len(album.Chapters))
		return nil
	}
	return album.Chapters
}

// fetchCover is best-effort: missing art must not stop a download.
func fetchCover(ctx context.Context, cmd *cobra.Command, client *http.Client, album *scraper.Album, dir string) string {
	if noCover || album.CoverURL == "" {
		return ""
	}
	path, err := downloader.FetchCover(ctx, client, userAgent, album.CoverURL, album.PageURL, dir)
	if err != nil {
		cmd.PrintErrf("[warn] no cover art: %v\n", err)
		return ""
	}
	debugf(cmd, "cover saved to %s", path)
	return path
}

// reportSource warns when a post offers only its stream.
func reportSource(cmd *cobra.Command, album *scraper.Album) {
	if album.Source() != scraper.SourceHLS {
		return
	}
	cmd.PrintErrln("[warn] this post serves no audio file, only the site's stream, so the")
	cmd.PrintErrln("[warn] audio is reassembled from it and ffmpeg is required")
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

// debugf writes to stderr under --verbose, keeping piped stdout clean.
func debugf(cmd *cobra.Command, format string, args ...any) {
	if verbose {
		cmd.PrintErrf("[debug] "+format+"\n", args...)
	}
}

// plural avoids having to write "file(s)".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// parseAlbumURL matches the hostname, not a substring, so evil.com cannot pass.
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

// outputDirFor sanitizes the title, untrusted page content, into one component.
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
