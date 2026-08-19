// Package app runs a download end to end. Everything a run depends on arrives
// in a Config, so one process can hold several at once.
package app

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/EagleStelle/jasmr-dl/internal/session"
)

// Host is the only site this reads.
const Host = "japaneseasmr.com"

const (
	DefaultConcurrency = 3
	DefaultConnections = 32
	DefaultRetries     = 4
)

// ChallengeOptions configures the browser an interstitial is cleared in. Its
// zero value clears nothing.
type ChallengeOptions struct {
	// Enabled allows a run to open a browser when a challenge appears.
	Enabled bool
	// BrowserPath names the browser to drive. Empty means find one.
	BrowserPath string
	// ProfileDir keeps a profile between runs. Empty means a throwaway one.
	ProfileDir string
	// Args are extra browser flags. See challenge.ContainerArgs.
	Args []string
	// Visible shows the browser instead of running it headless.
	Visible bool
	// Timeout bounds one solve. Zero means the challenge package's own.
	Timeout time.Duration
}

// Config is one run. DefaultConfig fills in what the command line would.
type Config struct {
	// Targets are post URLs, each already through ParseTarget.
	Targets []string

	// Template names each file and the directories above it. Empty is the
	// naming package's default.
	Template string

	// BasePath is the directory a relative template is written under.
	BasePath string

	// Concurrency is posts, and files within them, at once. Zero is the default.
	Concurrency int

	// Connections is ranged requests in flight across the run. Zero is the default.
	Connections int

	// Retries is retry attempts per ranged request. Zero means none.
	Retries int

	NoMetadata bool
	NoJacket   bool
	NoImages   bool
	NoChapters bool
	NoSplit    bool

	// Store keeps what a solve earns and hands back an earlier run's. Nil keeps
	// nothing.
	Store session.Store

	// StoreKey scopes the clearance in Store. Empty means Host; a service scopes
	// by account too, cf_clearance being bound to one IP and User-Agent.
	StoreKey string

	// Clearance is used ahead of anything in Store.
	Clearance *session.Clearance

	// UserAgent overrides the one to send. A clearance's own wins over it.
	UserAgent string

	Challenge ChallengeOptions

	// Stdout and Stderr may be nil, which discards them; Summary carries the
	// same facts.
	Stdout io.Writer
	Stderr io.Writer

	// Progress renders the live transfer lines. Nil draws none.
	Progress io.Writer

	// Log receives debug lines. Nil discards them.
	Log func(format string, args ...any)
}

// DefaultConfig carries the limits the command line defaults to.
func DefaultConfig() Config {
	return Config{
		Concurrency: DefaultConcurrency,
		Connections: DefaultConnections,
		Retries:     DefaultRetries,
	}
}

// prepared fills the blanks in and makes every stream safe to write to.
func (c Config) prepared() (Config, error) {
	if err := c.validate(); err != nil {
		return c, err
	}

	if c.Concurrency == 0 {
		c.Concurrency = DefaultConcurrency
	}
	if c.Connections == 0 {
		c.Connections = DefaultConnections
	}
	if c.StoreKey == "" {
		c.StoreKey = Host
	}
	if c.Stdout == nil {
		c.Stdout = io.Discard
	}
	if c.Stderr == nil {
		c.Stderr = io.Discard
	}
	if c.Progress == nil {
		c.Progress = io.Discard
	}
	if c.Log == nil {
		c.Log = func(string, ...any) {}
	}
	return c, nil
}

func (c Config) validate() error {
	switch {
	case len(c.Targets) == 0:
		return errors.New("no URL to download")
	case c.Concurrency < 0:
		return fmt.Errorf("concurrency must be at least 1, got %d", c.Concurrency)
	case c.Connections < 0:
		return fmt.Errorf("connections must be at least 1, got %d", c.Connections)
	case c.Retries < 0:
		return fmt.Errorf("retries cannot be negative, got %d", c.Retries)
	}

	// Which shape a post takes is not known yet, so every shape is parsed.
	if c.Template != "" {
		for _, shape := range templateShapes {
			if _, err := templateFor(c.Template, shape.split, shape.files); err != nil {
				return err
			}
		}
	}
	return nil
}
