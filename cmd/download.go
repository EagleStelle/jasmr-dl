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

const (
	targetHost     = "japaneseasmr.com"
	cookieFileName = "cookies.txt"

	genre = "ASMR"

	// partSeparator joins a work to the part one file holds.
	partSeparator = " - "
)

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

	jar, err := cookieJar(cmd)
	if err != nil {
		return err
	}

	// The UA is read off this machine's browser, because a cookie only clears
	// the challenge when it accompanies the UA that solved it.
	userAgent := util.UserAgent()
	debugf(cmd, "user-agent: %s", userAgent)

	// One client for the run: its connection pool is what keeps sockets warm
	// across the page fetch, the cover and every track.
	client := downloader.NewClient(jar)
	sc := scraper.New(client, userAgent)
	cmd.Printf("[info] fetching %s\n", target)

	album, err := sc.Album(ctx, target.String())
	if err != nil {
		return err
	}
	// The page came back, so whatever cleared the challenge works. Only now is
	// it worth keeping, and worth overwriting whatever was kept before.
	if cookieFile != "" {
		installCookies(cmd, cookieFile)
	}
	reportSource(cmd, album)

	cmd.Printf("[info] %s to download\n", plural(len(album.Tracks), "file"))
	debugf(cmd, "cover: %s", orNone(album.CoverURL))
	debugf(cmd, "album: %s, circle: %s, date: %s", orNone(album.RJCode), orNone(album.Circle), orNone(album.Date))
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
			Tags:       tagsFor(album, t),
		})
	}

	prog := downloader.NewProgress(cmd.OutOrStdout())
	d := &downloader.Downloader{
		Client:       client,
		UserAgent:    userAgent,
		OutputDir:    dir,
		Retries:      retries,
		CoverPath:    fetchCover(ctx, cmd, client, album, dir),
		Chapters:     chaptersFor(cmd, album),
		OnCoverError: func(name string, err error) { cmd.PrintErrf("[warn] %s: %v\n", name, err) },
		OnStart:      prog.Start,
		OnProgress:   prog.Update,
	}

	debugf(cmd, "%d files at once, %d retries per file", concurrency, retries)
	results := d.Run(ctx, jobs, concurrency)

	// Drain the renderer first, or it fights the summary for the same lines.
	prog.Wait()

	return report(cmd, results)
}

// cookieJar loads --cookies, or a cookies.txt sitting beside the binary. A nil
// jar means neither was there.
func cookieJar(cmd *cobra.Command) (http.CookieJar, error) {
	path := cookieFile
	if path == "" {
		path = foundCookieFile()
	}
	if path == "" {
		return nil, nil
	}

	jar, err := downloader.LoadCookieJar(path)
	if err != nil {
		return nil, fmt.Errorf("read cookies: %w", err)
	}
	debugf(cmd, "cookies from %s", path)
	return jar, nil
}

// foundCookieFile looks beside the binary and then in the working directory, so
// the file only has to exist to be used.
func foundCookieFile() string {
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	dirs = append(dirs, ".")

	for _, dir := range dirs {
		path := filepath.Join(dir, cookieFileName)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

// installCookies keeps a working --cookies file beside the binary under the name
// picked up automatically, so the flag is only ever needed once per export.
//
// Failing to save is not worth ending a run over: the cookies work, they just
// will not be there next time.
func installCookies(cmd *cobra.Command, src string) {
	exe, err := os.Executable()
	if err != nil {
		debugf(cmd, "cookies not saved: %v", err)
		return
	}

	dst := filepath.Join(filepath.Dir(exe), cookieFileName)
	if sameFile(src, dst) {
		return
	}

	data, err := os.ReadFile(src)
	if err == nil {
		err = os.WriteFile(dst, data, 0o600)
	}
	if err != nil {
		cmd.PrintErrf("[warn] cookies not saved for next time: %v\n", err)
		return
	}
	cmd.Printf("[info] cookies saved to %s, so -c is not needed again\n", dst)
}

// sameFile compares the files themselves, since two paths to one file differ
// freely in case and separators on Windows.
func sameFile(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

// tagsFor builds the metadata written into one file.
func tagsFor(album *scraper.Album, track scraper.Track) downloader.Tags {
	if noTags {
		return downloader.Tags{}
	}
	return downloader.Tags{
		Title:       titleFor(album, track),
		Artist:      album.Artists,
		AlbumArtist: album.Circle,
		Album:       album.RJCode,
		Date:        album.Date,
		Genre:       genre,
		Comment:     album.PageURL,
	}
}

// titleFor names one file, so a split post does not tag every part alike.
func titleFor(album *scraper.Album, track scraper.Track) string {
	if len(album.Tracks) < 2 || track.Name == "" {
		return album.Title
	}
	return album.Title + partSeparator + track.Name
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
	path, err := downloader.FetchCover(ctx, client, util.UserAgent(), album.CoverURL, album.PageURL, dir)
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
