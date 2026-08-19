package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/EagleStelle/jasmr-dl/internal/challenge"
	"github.com/EagleStelle/jasmr-dl/internal/downloader"
	"github.com/EagleStelle/jasmr-dl/internal/scraper"
	"github.com/EagleStelle/jasmr-dl/internal/session"
	"github.com/EagleStelle/jasmr-dl/internal/util"
)

// manualClearance is the way out when no browser can do it.
const manualClearance = "       Open the page in a browser, export its cookies as cookies.txt,\n" +
	"       and point --cookies at the file."

// state is the client and the User-Agent it presents. cf_clearance is bound to
// the User-Agent that earned it, so the two are only ever replaced together.
//
// Written while the posts are read, one at a time, and only read once they are
// downloading. Nothing here is safe to write from a post.
type state struct {
	client    *http.Client
	userAgent string

	// pending is a clearance worth storing once a page proves it works.
	pending *session.Clearance
}

func newState(ctx context.Context, cfg Config) (*state, error) {
	s := &state{userAgent: cfg.UserAgent}
	if s.userAgent == "" {
		s.userAgent = util.UserAgent()
	}

	clearance := cfg.Clearance
	if clearance != nil {
		s.pending = clearance
	} else if cfg.Store != nil {
		loaded, err := cfg.Store.Load(ctx, cfg.StoreKey)
		if err != nil {
			return nil, fmt.Errorf("read cookies: %w", err)
		}
		clearance = loaded
	}

	if clearance != nil {
		cfg.Log("holding %s", plural(len(clearance.Cookies), "cookie"))
		if clearance.UserAgent != "" {
			s.userAgent = clearance.UserAgent
			cfg.Log("user-agent recorded alongside them")
		}
	}

	if err := s.use(clearanceCookies(clearance)); err != nil {
		return nil, err
	}
	cfg.Log("user-agent: %s", s.userAgent)
	return s, nil
}

// transport is what one post downloads through.
func (s *state) transport() (*http.Client, string) { return s.client, s.userAgent }

// use rebuilds the client around a set of cookies, keeping one connection pool
// across the page fetch, the jacket and every track.
func (s *state) use(cookies []downloader.Cookie) error {
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

// keep stores a clearance the run was handed, now that a page has proved it
// works. Failing is not worth ending a run over.
func (s *state) keep(ctx context.Context, r *run) {
	if s.pending == nil || r.cfg.Store == nil {
		s.pending = nil
		return
	}

	c := s.pending
	s.pending = nil
	if err := r.cfg.Store.Save(ctx, r.cfg.StoreKey, c); err != nil {
		r.warnf("[warn] cookies not saved for next time: %v", err)
		return
	}
	r.infof("[info] cookies kept under %q, so they need not be given again", r.cfg.StoreKey)
}

// fetchAlbum reads the post page, clearing a challenge and trying once more
// when one stands in the way.
func (r *run) fetchAlbum(ctx context.Context, target string) (*scraper.Album, error) {
	client, userAgent := r.state.transport()
	album, err := scraper.New(client, userAgent).Album(ctx, target)
	if err == nil || !errors.Is(err, util.ErrChallenge) {
		return album, err
	}
	if !r.cfg.Challenge.Enabled {
		return nil, fmt.Errorf("%w\n%s", err, manualClearance)
	}

	r.warnf("[warn] Cloudflare is challenging this request, opening a browser to clear it")
	if err := r.solveChallenge(ctx, target); err != nil {
		return nil, fmt.Errorf("clear the challenge: %w\n%s", err, manualClearance)
	}

	// A second refusal is not worth a third browser: what the site objects to is
	// something no cookie fixes, the IP most likely.
	client, userAgent = r.state.transport()
	album, err = scraper.New(client, userAgent).Album(ctx, target)
	if errors.Is(err, util.ErrChallenge) {
		return nil, fmt.Errorf("%w even after clearing it\n%s", err, manualClearance)
	}
	return album, err
}

// solveChallenge clears the challenge and moves the run onto what it earned.
func (r *run) solveChallenge(ctx context.Context, target string) error {
	c := r.cfg.Challenge
	res, err := challenge.Solve(ctx, target, challenge.Options{
		BrowserPath: c.BrowserPath,
		ProfileDir:  c.ProfileDir,
		Args:        c.Args,
		Visible:     c.Visible,
		Timeout:     c.Timeout,
		Log:         r.debugf,
	})
	if err != nil {
		return err
	}

	earned := &session.Clearance{Cookies: res.Cookies, UserAgent: res.UserAgent}
	r.state.userAgent = earned.UserAgent
	if err := r.state.use(earned.Cookies); err != nil {
		return err
	}
	r.debugf("cleared, holding %s", plural(len(earned.Cookies), "cookie"))

	// Whatever the run was handed is now stale.
	r.state.pending = nil
	if r.cfg.Store == nil {
		return nil
	}
	if err := r.cfg.Store.Save(ctx, r.cfg.StoreKey, earned); err != nil {
		r.warnf("[warn] clearance not saved for next time: %v", err)
		return nil
	}
	r.infof("[info] clearance saved under %q", r.cfg.StoreKey)
	return nil
}

func clearanceCookies(c *session.Clearance) []downloader.Cookie {
	if c == nil {
		return nil
	}
	return c.Cookies
}
