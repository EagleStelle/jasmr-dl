package app

import (
	"fmt"

	"github.com/EagleStelle/jasmr-dl/internal/downloader"
)

// PostResult is what one post came to: the files it saved and where they went.
type PostResult struct {
	Label   string
	Dir     string
	Results []downloader.Result
}

// Files counts the files the post saved.
func (p PostResult) Files() int {
	n := 0
	for _, r := range p.Results {
		if r.Err == nil {
			n += len(r.Paths)
		}
	}
	return n
}

// Failed counts the downloads that did not land.
func (p PostResult) Failed() int {
	n := 0
	for _, r := range p.Results {
		if r.Err != nil {
			n++
		}
	}
	return n
}

// Summary is what a run came to.
type Summary struct {
	// Posts is how many the run set out to fetch.
	Posts int
	// FailedPosts saved nothing at all, whether or not they got as far as a download.
	FailedPosts int
	// Files saved across every post.
	Files int
	Results []PostResult
}

// summarize prints the failures and returns an error only if nothing at all was
// saved. broken counts the posts that never got as far as a download.
func (r *run) summarize(results []PostResult, broken int) (Summary, error) {
	posts := len(r.cfg.Targets)
	// A run of one post is named by its own output directory already.
	named := posts > 1

	s := Summary{Posts: posts, FailedPosts: broken, Results: results}
	for _, p := range results {
		files, failed := p.Files(), p.Failed()
		s.Files += files

		for _, res := range p.Results {
			if res.Err != nil {
				r.warnf("[error] %s%s: %v", tag(p, named), res.Job.Name, res.Err)
			}
		}

		switch {
		case failed == len(p.Results):
			s.FailedPosts++
		case failed > 0:
			r.infof("[done] %s saved to %s, %s failed",
				plural(files, "recording"), p.Dir, plural(failed, "download"))
		default:
			r.infof("[done] %s saved to %s", plural(files, "recording"), p.Dir)
		}
	}

	if s.FailedPosts == posts {
		if !named && len(results) == 1 {
			return s, fmt.Errorf("all %s failed", plural(len(results[0].Results), "download"))
		}
		return s, fmt.Errorf("all %s failed", plural(posts, "post"))
	}
	if named {
		line := fmt.Sprintf("[done] %s from %s",
			plural(s.Files, "recording"), plural(posts-s.FailedPosts, "post"))
		if s.FailedPosts > 0 {
			line += fmt.Sprintf(", %s failed", plural(s.FailedPosts, "post"))
		}
		r.infof("%s", line)
	}
	return s, nil
}

// tag names the post a failure belongs to, which only a run of several needs.
func tag(p PostResult, named bool) string {
	if !named {
		return ""
	}
	return p.Label + ": "
}
