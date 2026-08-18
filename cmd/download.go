package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"jasmr-dl/internal/challenge"
	"jasmr-dl/internal/downloader"
	"jasmr-dl/internal/scraper"
	"jasmr-dl/internal/util"
)

const (
	targetHost     = "japaneseasmr.com"
	cookieFileName = "cookies.txt"

	// profileDirName is kept between runs so a cleared challenge counts next time.
	profileDirName = "browser-profile"

	// manualClearance is the way out when no browser can do it.
	manualClearance = "       Open the page in a browser, export its cookies as cookies.txt,\n" +
		"       and leave the file beside this binary."

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
	if connections < 1 {
		return fmt.Errorf("--connections must be at least 1, got %d", connections)
	}
	if retries < 0 {
		return fmt.Errorf("--retries cannot be negative, got %d", retries)
	}

	// One Ctrl+C cancels every in-flight request cleanly.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	sess, err := newSession(cmd)
	if err != nil {
		return err
	}

	cmd.Printf("[info] fetching %s\n", target)
	album, err := fetchAlbum(ctx, cmd, target.String(), &sess)
	if err != nil {
		return err
	}
	// The page came back, so the cookies work and are worth keeping. A solve has
	// already written its own, which a stale export must not undo.
	if cookieFile != "" && !sess.solved {
		installCookies(cmd, cookieFile)
	}
	reportSource(cmd, album)

	cmd.Printf("[info] %s to download\n", plural(len(album.Tracks), "file"))
	debugf(cmd, "cover: %s", orNone(album.CoverURL))
	debugf(cmd, "gallery: %d images", len(album.ImageURLs))
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

	coverPath := fetchCover(ctx, cmd, sess, album, dir)
	fetchImages(ctx, cmd, sess, album, dir)

	prog := downloader.NewProgress(cmd.OutOrStdout())
	d := &downloader.Downloader{
		Client:       sess.client,
		UserAgent:    sess.userAgent,
		OutputDir:    dir,
		Connections:  connections,
		Retries:      retries,
		CoverPath:    coverPath,
		Chapters:     chaptersFor(cmd, album),
		OnCoverError: func(name string, err error) { cmd.PrintErrf("[warn] %s: %v\n", name, err) },
		OnStart:      prog.Start,
		OnProgress:   prog.Update,
	}

	debugf(cmd, "%d files at once, %d requests in flight, %d retries each", concurrency, connections, retries)
	results := d.Run(ctx, jobs, concurrency)

	// Drain the renderer first, or it fights the summary for the same lines.
	prog.Wait()

	return report(cmd, results)
}

// session is the client and the User-Agent it presents. cf_clearance is bound to
// the User-Agent that earned it, so the two are only ever replaced together.
type session struct {
	client    *http.Client
	userAgent string
	solved    bool // cookies.txt is already current
}

// newSession loads whatever clearance is already on disk. A run with none still
// starts: a challenge is cleared when it appears.
func newSession(cmd *cobra.Command) (session, error) {
	s := session{userAgent: util.UserAgent()}

	path := cookieFile
	if path == "" {
		path = foundCookieFile()
	}

	var cookies []downloader.Cookie
	if path != "" {
		loaded, userAgent, err := downloader.LoadCookies(path)
		if err != nil {
			return s, fmt.Errorf("read cookies: %w", err)
		}
		debugf(cmd, "cookies from %s", path)

		cookies = loaded
		// The recorded UA is the one the cookies were earned under.
		if userAgent != "" {
			s.userAgent = userAgent
			debugf(cmd, "user-agent recorded alongside them")
		}
	}

	if err := s.use(cookies); err != nil {
		return s, err
	}
	debugf(cmd, "user-agent: %s", s.userAgent)
	return s, nil
}

// use rebuilds the client around a set of cookies. One client per set: its
// connection pool keeps sockets warm across the page fetch, the cover and every
// track.
func (s *session) use(cookies []downloader.Cookie) error {
	if len(cookies) == 0 {
		s.client = downloader.NewClient(nil)
		return nil
	}

	jar, err := downloader.NewJar(cookies)
	if err != nil {
		return err
	}
	s.client = downloader.NewClient(jar)
	return nil
}

// fetchAlbum reads the post page, clearing a Cloudflare challenge in a browser
// and trying once more when one stands in the way.
func fetchAlbum(ctx context.Context, cmd *cobra.Command, target string, s *session) (*scraper.Album, error) {
	album, err := scraper.New(s.client, s.userAgent).Album(ctx, target)
	if err == nil || !errors.Is(err, util.ErrChallenge) {
		return album, err
	}

	cmd.PrintErrln("[warn] Cloudflare is challenging this request, opening a browser to clear it")
	if err := solveChallenge(ctx, cmd, target, s); err != nil {
		return nil, fmt.Errorf("clear the challenge: %w\n%s", err, manualClearance)
	}

	// A second refusal is not worth a third browser: what the site objects to is
	// something no cookie fixes, the IP most likely.
	album, err = scraper.New(s.client, s.userAgent).Album(ctx, target)
	if errors.Is(err, util.ErrChallenge) {
		return nil, fmt.Errorf("%w even after clearing it\n%s", err, manualClearance)
	}
	return album, err
}

// solveChallenge clears the challenge and moves the session onto what it earned.
func solveChallenge(ctx context.Context, cmd *cobra.Command, target string, s *session) error {
	res, err := challenge.Solve(ctx, target, challenge.Options{
		BrowserPath: browserPath,
		ProfileDir:  besideBinary(profileDirName),
		Visible:     showBrowser,
		Log:         func(format string, args ...any) { debugf(cmd, format, args...) },
	})
	if err != nil {
		return err
	}

	s.userAgent = res.UserAgent
	s.solved = true
	if err := s.use(res.Cookies); err != nil {
		return err
	}
	debugf(cmd, "cleared, holding %s", plural(len(res.Cookies), "cookie"))

	// Keeping them is not worth failing a run that is already cleared.
	path := besideBinary(cookieFileName)
	if err := downloader.SaveCookies(path, res.UserAgent, res.Cookies); err != nil {
		cmd.PrintErrf("[warn] clearance not saved for next time: %v\n", err)
		return nil
	}
	cmd.Printf("[info] clearance saved to %s\n", path)
	return nil
}

// besideBinary is where state that outlives a run goes.
func besideBinary(name string) string {
	exe, err := os.Executable()
	if err != nil {
		return name
	}
	return filepath.Join(filepath.Dir(exe), name)
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
func fetchCover(ctx context.Context, cmd *cobra.Command, s session, album *scraper.Album, dir string) string {
	if noCover || album.CoverURL == "" {
		return ""
	}
	path, err := downloader.FetchCover(ctx, s.client, s.userAgent, album.CoverURL, album.PageURL, dir)
	if err != nil {
		cmd.PrintErrf("[warn] no cover art: %v\n", err)
		return ""
	}
	debugf(cmd, "cover saved to %s", path)
	return path
}

// fetchImages saves the rest of the post's gallery beside the jacket. Best
// effort, like the cover: pictures are not what the run is for.
func fetchImages(ctx context.Context, cmd *cobra.Command, s session, album *scraper.Album, dir string) {
	if noImages {
		return
	}

	var urls []string
	for _, u := range album.ImageURLs {
		// The cover is the jacket. Dropping it here rather than counting on
		// it being first keeps the numbering the same on a post that opens
		// its gallery with something else.
		if u != album.CoverURL {
			urls = append(urls, u)
		}
	}
	if len(urls) == 0 {
		return
	}

	paths := downloader.FetchImages(ctx, s.client, s.userAgent, urls, album.PageURL, dir,
		func(err error) { cmd.PrintErrf("[warn] image not saved: %v\n", err) })
	if len(paths) > 0 {
		cmd.Printf("[info] %s saved to %s\n", plural(len(paths), "image"), filepath.Dir(paths[0]))
	}
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
