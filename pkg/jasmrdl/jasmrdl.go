// Package jasmrdl downloads audio from japaneseasmr.com posts.
//
// It is the surface another program embeds. Everything a run needs arrives in a
// Config, and the Cloudflare clearance lives in a Store the caller supplies, so
// nothing is written beside the binary and no two callers share one identity.
package jasmrdl

import (
	"context"

	"github.com/EagleStelle/jasmr-dl/internal/app"
	"github.com/EagleStelle/jasmr-dl/internal/challenge"
	"github.com/EagleStelle/jasmr-dl/internal/downloader"
	"github.com/EagleStelle/jasmr-dl/internal/session"
)

// Host is the only site this reads.
const Host = app.Host

// StateDirEnv moves a FileStore.
const StateDirEnv = session.DirEnv

type (
	Config           = app.Config
	ChallengeOptions = app.ChallengeOptions
	Summary          = app.Summary
	PostResult       = app.PostResult
	Result           = downloader.Result
	OutputFile       = downloader.OutputFile

	// Info is a post as Config.WriteInfoJSON writes it, and as
	// Config.LoadInfoJSON reads it back.
	Info        = app.Info
	InfoTrack   = app.InfoTrack
	InfoChapter = app.InfoChapter

	Clearance   = session.Clearance
	Cookie      = downloader.Cookie
	Store       = session.Store
	FileStore   = session.FileStore
	MemoryStore = session.MemoryStore
)

// ContainerArgs are the browser flags Chrome needs inside a container.
var ContainerArgs = challenge.ContainerArgs

// DefaultConfig carries the limits the command line defaults to.
func DefaultConfig() Config { return app.DefaultConfig() }

// Run downloads every target in cfg.
func Run(ctx context.Context, cfg Config) (Summary, error) { return app.Run(ctx, cfg) }

// ParseTarget normalises one post URL and rejects anything off Host.
func ParseTarget(raw string) (string, error) { return app.ParseTarget(raw) }

// ParseTargets runs raw through ParseTarget, dropping repeats.
func ParseTargets(raw []string, onRepeat func(string)) ([]string, error) {
	return app.ParseTargets(raw, onRepeat)
}

// DefaultStateDir is the OS's per-user config directory for this program.
func DefaultStateDir() string { return session.DefaultDir() }

// ReadCookieFile reads one Netscape cookies.txt, whoever wrote it.
func ReadCookieFile(path string) (*Clearance, error) { return session.ReadFile(path) }
