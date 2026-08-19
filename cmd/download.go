package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

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
	if err := checkFlags(); err != nil {
		return err
	}
	given, err := givenTemplate(cmd)
	if err != nil {
		return err
	}
	targets, err := targetsFrom(cmd, args)
	if err != nil {
		return err
	}

	// One Ctrl+C cancels every in-flight request cleanly.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	sess, err := newSession(cmd)
	if err != nil {
		return err
	}

	plans, broken, err := planAll(ctx, cmd, &sess, targets, given)
	if err != nil {
		return err
	}
	if len(plans) == 0 {
		return report(cmd, nil, broken, len(targets))
	}

	debugf(cmd, "%s at once, %d requests in flight, %d retries each",
		plural(concurrency, "file"), connections, retries)

	// One budget for the whole run: the ceilings are what the host sees, so
	// they cannot be per post.
	budget := downloader.NewBudget(concurrency, connections)
	prog := downloader.NewProgress(cmd.OutOrStdout())

	outcomes := make([]outcome, len(plans))
	g := new(errgroup.Group)
	// -N bounds the posts as well as the files inside them: it is how much the
	// run does at once, not how much each post does. A post waiting here holds
	// nothing, since the budget is what the downloads actually draw on.
	g.SetLimit(concurrency)
	for i, p := range plans {
		g.Go(func() error {
			outcomes[i] = runPost(ctx, cmd, &sess, prog, budget, p)
			return nil
		})
	}
	_ = g.Wait() // a post never returns an error; failures live in its results

	// Drain the renderer first, or it fights the summary for the same lines.
	prog.Wait()

	return report(cmd, outcomes, broken, len(targets))
}

func checkFlags() error {
	switch {
	case concurrency < 1:
		return fmt.Errorf("--concurrency must be at least 1, got %d", concurrency)
	case connections < 1:
		return fmt.Errorf("--connections must be at least 1, got %d", connections)
	case retries < 0:
		return fmt.Errorf("--retries cannot be negative, got %d", retries)
	}
	return nil
}

// givenTemplate settles a bad -o before anything is fetched. Which shape a post
// takes is not known yet, so every shape is parsed.
func givenTemplate(cmd *cobra.Command) (string, error) {
	if !cmd.Flags().Changed("output") {
		return "", nil
	}
	given := strings.TrimSpace(outputTmpl)
	if given == "" {
		return "", errors.New("output template is empty")
	}
	for _, shape := range templateShapes {
		if _, err := templateFor(given, shape.split, shape.files); err != nil {
			return "", err
		}
	}
	return given, nil
}

// --- posts ------------------------------------------------------------------

// plan is one post, read and ready to download.
type plan struct {
	album    *scraper.Album
	dir      string
	tmpl     *naming.Template
	chapters []scraper.Chapter
	split    bool
	jobs     []downloader.Job
	lines    postLines
}

// planAll reads every post, one at a time on purpose: a challenge is cleared in
// one browser rather than several, and the session a solve replaces is written
// here and only read once the downloads start. It returns the posts it could
// read and a count of the ones it could not.
func planAll(ctx context.Context, cmd *cobra.Command, sess *session, targets []string, given string) ([]*plan, int, error) {
	var (
		plans  []*plan
		broken int
	)
	for _, target := range targets {
		// Ctrl+C here ends the run, rather than failing every post left over
		// and saying so one line at a time.
		if ctx.Err() != nil {
			return nil, 0, ctx.Err()
		}

		p, err := planFor(ctx, cmd, sess, target, given)
		if err != nil {
			// A lone post failing is the run failing, and its own error says
			// more than a count of one ever could.
			if len(targets) == 1 {
				return nil, 0, err
			}
			warnf(cmd, "[error] %s: %v", target, err)
			broken++
			continue
		}
		plans = append(plans, p)
	}
	return plans, broken, nil
}

// planFor reads one post and works out everything the download needs from it.
func planFor(ctx context.Context, cmd *cobra.Command, sess *session, target, given string) (*plan, error) {
	cmd.Printf("[info] fetching %s\n", target)
	album, err := fetchAlbum(ctx, cmd, target, sess)
	if err != nil {
		return nil, err
	}
	// A page came back, so the cookies work and are worth keeping, once for the
	// run. A solve has already written its own, which a stale export must not
	// undo.
	if cookieFile != "" && !sess.solved && !sess.installed {
		sess.installed = true
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
		return nil, err
	}

	fields := fieldsFor(album)
	dir := underBasePath(tmpl.Dir(fields))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
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

	return &plan{
		album:    album,
		dir:      dir,
		tmpl:     tmpl,
		chapters: chapters,
		split:    split,
		jobs:     jobs,
		lines:    linesFor(album),
	}, nil
}

// runPost saves one post's pictures and then its recordings. budget is the
// run's, shared with every other post going down beside this one.
func runPost(ctx context.Context, cmd *cobra.Command, sess *session, prog *downloader.Progress, budget downloader.Budget, p *plan) outcome {
	d := &downloader.Downloader{
		Client:         sess.client,
		UserAgent:      sess.userAgent,
		OutputDir:      p.dir,
		Template:       p.tmpl,
		Budget:         budget,
		Retries:        retries,
		Chapters:       p.chapters,
		Split:          p.split,
		OnPictureError: func(err error) { warnf(cmd, "[warn] picture not saved: %v", err) },
		OnJacketError:  func(name string, err error) { warnf(cmd, "[warn] %s: %v", name, err) },
		OnChapterDropped: func(name string, start, total time.Duration) {
			warnf(cmd, "[warn] chapter %s begins at %s, past the end of a %s stream; skipped",
				name, start.Round(time.Second), total.Round(time.Second))
		},
		OnStart: func(name string, total int64) {
			prog.Start(p.lines.line(downloader.KindRecording, name), total)
		},
		OnProgress: func(name string, done, total int64) {
			prog.Update(p.lines.key(downloader.KindRecording, name), done, total)
		},
		OnSplitStart: func(total int) {
			prog.StartCount(p.lines.line(downloader.KindChapter, chaptersName), int64(total))
		},
		OnSplitProgress: func(done, total int) {
			prog.Update(p.lines.key(downloader.KindChapter, chaptersName), int64(done), int64(total))
		},
	}

	// The jacket has to be on disk before the audio it is embedded in.
	d.JacketPath = fetchPictures(ctx, cmd, d, prog, p)

	return outcome{label: p.lines.label, dir: p.dir, results: d.Run(ctx, p.jobs)}
}

// session is the client and the User-Agent it presents. cf_clearance is bound to
// the User-Agent that earned it, so the two are only ever replaced together.
//
// It is written while the posts are being read, one at a time, and only read
// once they are downloading. Nothing here is safe to write from a post.
type session struct {
	client    *http.Client
	userAgent string
	solved    bool // cookies.txt is already current
	installed bool // the --cookies export is already beside the binary
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

// fetchPictures saves the jacket and the gallery together, on one line and in
// parallel, and returns the jacket the audio embeds. Best-effort: missing art
// must not stop a download.
func fetchPictures(ctx context.Context, cmd *cobra.Command, d *downloader.Downloader, prog *downloader.Progress, p *plan) string {
	jacketURL, gallery := pictureURLs(p.album)
	count := len(gallery)
	if jacketURL != "" {
		count++
	}
	if count == 0 {
		return ""
	}

	line := p.lines.line(downloader.KindImage, imagesName)
	prog.StartUnits(line, int64(count))

	pics := d.FetchPictures(ctx, jacketURL, gallery, p.album.PageURL,
		func(done int, bytes, total int64) {
			prog.SetUnits(line.Key, int64(done))
			prog.Update(line.Key, bytes, total)
		})

	debugf(cmd, "jacket: %s", orNone(pics.Jacket))
	debugf(cmd, "gallery: %d saved", len(pics.Gallery))
	return pics.Jacket
}

// pictureURLs splits a post's pictures into the jacket the audio embeds and the
// gallery behind it, dropping whatever the flags turn off.
func pictureURLs(album *scraper.Album) (jacket string, gallery []string) {
	if !noJacket {
		jacket = album.JacketURL
	}
	if noImages {
		return jacket, nil
	}

	for _, u := range album.ImageURLs {
		// The jacket already sits beside the audio. Dropping it here rather
		// than counting on it being first keeps the numbering the same on a
		// post that opens its gallery with something else.
		if u != album.JacketURL {
			gallery = append(gallery, u)
		}
	}
	return jacket, gallery
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

// reportSource names what the post serves its audio as.
func reportSource(cmd *cobra.Command, album *scraper.Album) {
	kind := "file"
	if album.Source() == scraper.SourceHLS {
		kind = "stream"
	}
	cmd.Printf("[info] source: %s\n", kind)
}

// outcome is what one post came to: the files it saved and where they went.
type outcome struct {
	label   string
	dir     string
	results []downloader.Result
}

// tag names the post a failure belongs to, which only a run of several needs.
func (o outcome) tag(named bool) string {
	if !named {
		return ""
	}
	return o.label + ": "
}

// report lists the failures and returns an error only if nothing at all was
// saved. broken counts the posts that never got as far as a download, posts how
// many the run set out to fetch. One download can leave several files behind, so
// files and downloads are counted apart.
func report(cmd *cobra.Command, outs []outcome, broken, posts int) error {
	// A run of one is named by its own output directory already.
	named := posts > 1

	saved, failedPosts := 0, broken
	for _, o := range outs {
		var failed, files int
		for _, r := range o.results {
			if r.Err != nil {
				failed++
				warnf(cmd, "[error] %s%s: %v", o.tag(named), r.Job.Name, r.Err)
				continue
			}
			files += len(r.Paths)
		}
		saved += files

		switch {
		case failed == len(o.results):
			failedPosts++
		case failed > 0:
			cmd.Printf("[done] %s saved to %s, %s failed\n",
				plural(files, "recording"), o.dir, plural(failed, "download"))
		default:
			cmd.Printf("[done] %s saved to %s\n", plural(files, "recording"), o.dir)
		}
	}

	if failedPosts == posts {
		if !named && len(outs) == 1 {
			return fmt.Errorf("all %s failed", plural(len(outs[0].results), "download"))
		}
		return fmt.Errorf("all %s failed", plural(posts, "post"))
	}
	if named {
		line := fmt.Sprintf("[done] %s from %s", plural(saved, "recording"), plural(posts-failedPosts, "post"))
		if failedPosts > 0 {
			line += fmt.Sprintf(", %s failed", plural(failedPosts, "post"))
		}
		cmd.Println(line)
	}
	return nil
}

// stderrMu hands stderr over one line at a time: every post warns down the same
// pipe, and half a line inside another is worse than waiting.
var stderrMu sync.Mutex

// warnf writes one line to stderr, keeping piped stdout clean.
func warnf(cmd *cobra.Command, format string, args ...any) {
	stderrMu.Lock()
	defer stderrMu.Unlock()
	cmd.PrintErrf(format+"\n", args...)
}

// debugf writes to stderr under --verbose.
func debugf(cmd *cobra.Command, format string, args ...any) {
	if verbose {
		warnf(cmd, "[debug] "+format, args...)
	}
}

// plural avoids having to write "file(s)".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// targetsFrom is every post the run covers: the arguments, then whatever
// --batch-file lists. Repeats are dropped, so a post named twice is fetched
// once. A malformed URL ends the run before anything is fetched, since it is
// almost always a typo, and finding out after nine downloads helps nobody.
func targetsFrom(cmd *cobra.Command, args []string) ([]string, error) {
	raw := args
	if batchFile != "" {
		listed, err := readBatchFile(cmd, batchFile)
		if err != nil {
			return nil, err
		}
		raw = slices.Concat(args, listed)
	}

	seen := make(map[string]bool, len(raw))
	targets := make([]string, 0, len(raw))
	for _, r := range raw {
		target, err := parseAlbumURL(r)
		if err != nil {
			return nil, err
		}
		if seen[target] {
			debugf(cmd, "%s is listed more than once, fetching it once", target)
			continue
		}
		seen[target] = true
		targets = append(targets, target)
	}

	if len(targets) == 0 {
		return nil, errors.New("no URL to download")
	}
	return targets, nil
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
