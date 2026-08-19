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
	"time"

	"github.com/spf13/cobra"

	"github.com/EagleStelle/jasmr-dl/internal/challenge"
	"github.com/EagleStelle/jasmr-dl/internal/downloader"
	"github.com/EagleStelle/jasmr-dl/internal/naming"
	"github.com/EagleStelle/jasmr-dl/internal/scraper"
	"github.com/EagleStelle/jasmr-dl/internal/util"
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

	// The rows a post opens beside its recordings, one of each.
	jacketName   = "jacket"
	imagesName   = "images"
	chaptersName = "chapters"
)

// templateShapes is every branch templateFor can pick, so a bad template is
// refused before a run fetches anything. A split post holds one track, which is
// why no shape pairs a split with more.
var templateShapes = []struct {
	split bool
	files int
}{
	{split: false, files: 1}, // the whole post in one file, no counter
	{split: false, files: 2}, // a file per track, trailing counter
	{split: true, files: 1},  // a file per chapter, leading counter
}

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
	// Settle a bad template before anything is fetched. Which shape the post
	// takes is not known yet, so every shape is parsed.
	var given string
	if cmd.Flags().Changed("output") {
		if given = strings.TrimSpace(outputTmpl); given == "" {
			return errors.New("output template is empty")
		}
		for _, shape := range templateShapes {
			if _, err := templateFor(given, shape.split, shape.files); err != nil {
				return err
			}
		}
	}

	// One Ctrl+C cancels every in-flight request cleanly.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	sess, err := newSession(cmd)
	if err != nil {
		return err
	}

	cmd.Printf("[info] fetching %s\n", target)
	album, err := fetchAlbum(ctx, cmd, target, &sess)
	if err != nil {
		return err
	}
	// The page came back, so the cookies work and are worth keeping. A solve has
	// already written its own, which a stale export must not undo.
	if cookieFile != "" && !sess.solved {
		installCookies(cmd, cookieFile)
	}
	reportSource(cmd, album)

	debugf(cmd, "jacket: %s", orNone(album.JacketURL))
	debugf(cmd, "gallery: %d images", len(album.ImageURLs))
	debugf(cmd, "album: %s, circle: %s, date: %s", orNone(album.RJCode), orNone(album.Circle), orNone(album.Date))
	debugf(cmd, "chapters: %d", len(album.Chapters))
	for _, t := range album.Tracks {
		debugf(cmd, "%s: %s", t.Title, t.LinkURL)
	}

	chapters := chaptersFor(cmd, album)
	split := splitting(chapters)

	tmpl, err := templateFor(given, split, len(album.Tracks))
	if err != nil {
		return err
	}

	fields := fieldsFor(album)
	dir := underBasePath(tmpl.Dir(fields))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	jobs := make([]downloader.Job, 0, len(album.Tracks))
	for i, t := range album.Tracks {
		f := fields
		f.Number, f.Total = i+1, len(album.Tracks)
		jobs = append(jobs, downloader.Job{
			Name:       t.Title,
			LinkURL:    t.LinkURL,
			Source:     t.Source,
			Alternates: t.Alternates,
			Referer:    album.PageURL,
			Tags:       tagsFor(album, t, i+1),
			Fields:     f,
		})
	}

	prog := downloader.NewProgress(cmd.OutOrStdout())
	lines := linesFor(album)

	jacketPath := fetchJacket(ctx, cmd, &sess, album, dir, prog, lines)
	fetchImages(ctx, cmd, &sess, album, dir, prog, lines)

	d := &downloader.Downloader{
		Client:        sess.client,
		UserAgent:     sess.userAgent,
		OutputDir:     dir,
		Template:      tmpl,
		Connections:   connections,
		Retries:       retries,
		JacketPath:    jacketPath,
		Chapters:      chapters,
		Split:         split,
		OnJacketError: func(name string, err error) { cmd.PrintErrf("[warn] %s: %v\n", name, err) },
		OnChapterDropped: func(name string, start, total time.Duration) {
			cmd.PrintErrf("[warn] chapter %s begins at %s, past the end of a %s stream; skipped\n",
				name, start.Round(time.Second), total.Round(time.Second))
		},
		OnStart: func(name string, total int64) {
			prog.Start(lines.line(downloader.KindRecording, name), total)
		},
		OnProgress: func(name string, done, total int64) {
			prog.Update(lines.key(downloader.KindRecording, name), done, total)
		},
		OnSplitStart: func(total int) {
			prog.StartCount(lines.line(downloader.KindChapter, chaptersName), int64(total))
		},
		OnSplitProgress: func(done, total int) {
			prog.Update(lines.key(downloader.KindChapter, chaptersName), int64(done), int64(total))
		},
	}

	debugf(cmd, "%d files at once, %d requests in flight, %d retries each", concurrency, connections, retries)
	results := d.Run(ctx, jobs, concurrency)

	// Drain the renderer first, or it fights the summary for the same lines.
	prog.Wait()

	return report(cmd, dir, results)
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
// connection pool keeps sockets warm across the page fetch, the jacket and
// every track.
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
	for _, path := range []string{besideBinary(cookieFileName), cookieFileName} {
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
	dst := besideBinary(cookieFileName)
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

// underBasePath puts a relative template under --paths. A template that names
// its own root already says where it goes.
func underBasePath(dir string) string {
	if basePath == "" || rooted(dir) {
		return dir
	}
	return filepath.Join(basePath, dir)
}

// rooted reports whether dir says for itself where it starts. A leading
// separator counts: Windows calls that drive-relative rather than absolute, but
// it is still not somewhere --paths should move.
func rooted(dir string) bool {
	return filepath.IsAbs(dir) ||
		strings.HasPrefix(dir, "/") ||
		strings.HasPrefix(dir, `\`) ||
		hasDriveLetter(dir)
}

func hasDriveLetter(dir string) bool {
	if len(dir) < 2 || dir[1] != ':' {
		return false
	}
	c := dir[0]
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

// templateFor resolves the template a run writes from. files is how many names
// it has to keep apart.
func templateFor(given string, split bool, files int) (*naming.Template, error) {
	raw := naming.Default
	if given != "" {
		raw = given
	}

	chosen, err := naming.Select(raw, split)
	if err != nil {
		return nil, err
	}

	// A file per chapter always leads with its number, a file per track only
	// carries one where the post holds more than one, and a template that
	// places {number} itself is left to say where. Without this a custom -o
	// naming no counter would write every file of a post to one name.
	switch {
	case split:
		chosen = naming.WithNumber(chosen, true)
	case files > 1:
		chosen = naming.WithNumber(chosen, false)
	default:
		chosen = naming.WithoutNumber(chosen)
	}
	return naming.Parse(chosen)
}

// fieldsFor is the album half of the output template, the half a directory may
// name. An empty field is the template's own to stand in for.
func fieldsFor(album *scraper.Album) naming.Fields {
	year, month, day := splitDate(album.Date)
	return naming.Fields{
		Title:  album.Title,
		RJCode: album.RJCode,
		Circle: album.Circle,
		Artist: album.Artists,
		Date:   album.Date,
		Year:   year,
		Month:  month,
		Day:    day,
	}
}

// splitDate breaks the scraper's ISO date apart; it yields that form or nothing.
func splitDate(date string) (year, month, day string) {
	parts := strings.Split(date, "-")
	if len(parts) != 3 {
		return "", "", ""
	}
	return parts[0], parts[1], parts[2]
}

// splitting reports whether the run cuts a stream on its chapters. chapters is
// already empty where the post has none to use.
func splitting(chapters []scraper.Chapter) bool {
	return !noSplit && len(chapters) > 0
}

// tagsFor builds the metadata written into one file, n being its place in the
// post. A split run replaces the title and track number per chapter as it cuts.
func tagsFor(album *scraper.Album, track scraper.Track, n int) downloader.Tags {
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
		Track:       n,
		TrackTotal:  len(album.Tracks),
		Disc:        1,
		DiscTotal:   1,
	}
}

// titleFor names one file. album.Title reaches the metadata only here, so a
// multi-track post carries the work and the part one file holds.
func titleFor(album *scraper.Album, track scraper.Track) string {
	if len(album.Tracks) < 2 || track.Name == "" {
		return album.Title
	}
	return album.Title + " - " + track.Name
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

// fetchJacket is best-effort: missing art must not stop a download. Its line
// opens at an unknown total, which the first bytes read replace with the real
// one.
func fetchJacket(ctx context.Context, cmd *cobra.Command, s *session, album *scraper.Album, dir string, prog *downloader.Progress, lines postLines) string {
	if noJacket || album.JacketURL == "" {
		return ""
	}

	line := lines.line(downloader.KindJacket, jacketName)
	prog.Start(line, 0)

	path, err := downloader.FetchJacket(ctx, s.client, s.userAgent, album.JacketURL, album.PageURL, dir,
		func(done, total int64) { prog.Update(line.Key, done, total) })
	if err != nil {
		cmd.PrintErrf("[warn] no jacket art: %v\n", err)
		return ""
	}
	debugf(cmd, "jacket saved to %s", path)
	return path
}

// postLines names one post's progress rows, all of them titled alike and told
// apart by a key the post's address and the row's kind scope.
type postLines struct {
	scope string
	label string
}

func linesFor(album *scraper.Album) postLines {
	return postLines{scope: album.PageURL, label: progressLabel(album)}
}

// key scopes a row name by kind, so a track titled "images" opens a line of its
// own rather than driving the gallery's.
func (p postLines) key(kind, name string) string {
	return p.scope + "\x00" + kind + "\x00" + name
}

func (p postLines) line(kind, name string) downloader.Line {
	return downloader.Line{Key: p.key(kind, name), Kind: kind, Label: p.label}
}

// progressLabel titles a post's rows, falling back where it has no title.
func progressLabel(album *scraper.Album) string {
	switch {
	case album.Title != "":
		return album.Title
	case album.RJCode != "":
		return album.RJCode
	}
	return album.PageURL
}

// fetchImages saves the rest of the post's gallery beside the jacket. Best
// effort, like the jacket: pictures are not what the run is for.
func fetchImages(ctx context.Context, cmd *cobra.Command, s *session, album *scraper.Album, dir string, prog *downloader.Progress, lines postLines) {
	if noImages {
		return
	}

	var urls []string
	for _, u := range album.ImageURLs {
		// The jacket already sits beside the audio. Dropping it here rather
		// than counting on it being first keeps the numbering the same on a
		// post that opens its gallery with something else.
		if u != album.JacketURL {
			urls = append(urls, u)
		}
	}
	if len(urls) == 0 {
		return
	}

	key := lines.key(downloader.KindImage, imagesName)
	prog.StartCount(lines.line(downloader.KindImage, imagesName), int64(len(urls)))
	paths := downloader.FetchImages(ctx, s.client, s.userAgent, urls, album.PageURL, dir,
		func(done, total int) { prog.Update(key, int64(done), int64(total)) },
		func(err error) { cmd.PrintErrf("[warn] image not saved: %v\n", err) })
	debugf(cmd, "gallery: %d saved", len(paths))
}

// reportSource names what the post serves its audio as.
func reportSource(cmd *cobra.Command, album *scraper.Album) {
	kind := "file"
	if album.Source() == scraper.SourceHLS {
		kind = "stream"
	}
	cmd.Printf("[info] source: %s\n", kind)
}

// report lists the failures and returns an error only if every download failed.
// One download can leave several files behind, so the two are counted apart.
func report(cmd *cobra.Command, dir string, results []downloader.Result) error {
	var failed, files int
	for _, r := range results {
		if r.Err != nil {
			failed++
			cmd.PrintErrf("[error] %s: %v\n", r.Job.Name, r.Err)
			continue
		}
		files += len(r.Paths)
	}

	switch {
	case failed == len(results):
		return fmt.Errorf("all %s failed", plural(failed, "download"))
	case failed > 0:
		cmd.Printf("[done] %s saved to %s, %s failed\n", plural(files, "recording"), dir, plural(failed, "download"))
	default:
		cmd.Printf("[done] %s saved to %s\n", plural(files, "recording"), dir)
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
func parseAlbumURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("malformed URL %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("URL must be http or https, got %q", raw)
	}
	host := strings.ToLower(u.Hostname())
	if host != targetHost && host != "www."+targetHost {
		return "", fmt.Errorf("not a %s URL: %q", targetHost, raw)
	}
	return u.String(), nil
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}
