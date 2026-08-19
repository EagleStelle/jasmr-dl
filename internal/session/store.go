// Package session keeps a Cloudflare clearance — cookies plus the User-Agent
// they were earned under — behind a Store the caller supplies.
package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/EagleStelle/jasmr-dl/internal/downloader"
)

// DirEnv moves a FileStore, for a container pointing one at a volume.
const DirEnv = "JASMR_DL_STATE_DIR"

const dirName = "jasmr-dl"

// maxStem bounds the readable part of a filename.
const maxStem = 64

// Clearance is one key's cookies and the User-Agent that earned them.
type Clearance struct {
	Cookies   []downloader.Cookie
	UserAgent string
}

func (c *Clearance) Clone() *Clearance {
	if c == nil {
		return nil
	}
	return &Clearance{Cookies: slices.Clone(c.Cookies), UserAgent: c.UserAgent}
}

// Store keeps a Clearance between runs, scoped by key. Load returns nil, nil
// where the key holds none.
type Store interface {
	Load(ctx context.Context, key string) (*Clearance, error)
	Save(ctx context.Context, key string, c *Clearance) error
}

// DefaultDir is the OS's per-user config directory for this program.
func DefaultDir() string {
	if dir := os.Getenv(DirEnv); dir != "" {
		return dir
	}
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, dirName)
	}
	return "." + dirName
}

// FileStore keeps one Netscape cookies.txt per key under Dir.
type FileStore struct {
	// Dir holds the files; empty means DefaultDir.
	Dir string

	// Fallbacks are read, never written, when Dir holds nothing for the key.
	Fallbacks []string
}

// Path names the file a key is kept in.
func (s FileStore) Path(key string) string {
	return filepath.Join(s.dir(), fileName(key))
}

func (s FileStore) dir() string {
	if s.Dir != "" {
		return s.Dir
	}
	return DefaultDir()
}

func (s FileStore) Load(_ context.Context, key string) (*Clearance, error) {
	for _, path := range append([]string{s.Path(key)}, s.Fallbacks...) {
		c, err := ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return c, nil
	}
	return nil, nil
}

func (s FileStore) Save(_ context.Context, key string, c *Clearance) error {
	if c == nil || len(c.Cookies) == 0 {
		return nil
	}

	dir := s.dir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	return downloader.SaveCookies(filepath.Join(dir, fileName(key)), c.UserAgent, c.Cookies)
}

// ReadFile reads one cookies.txt, whoever wrote it.
func ReadFile(path string) (*Clearance, error) {
	cookies, userAgent, err := downloader.LoadCookies(path)
	if err != nil {
		return nil, err
	}
	return &Clearance{Cookies: cookies, UserAgent: userAgent}, nil
}

// MemoryStore keeps clearances for the life of the process. Zero value is ready.
type MemoryStore struct {
	mu sync.Mutex
	m  map[string]*Clearance
}

func (s *MemoryStore) Load(_ context.Context, key string) (*Clearance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[key].Clone(), nil
}

func (s *MemoryStore) Save(_ context.Context, key string, c *Clearance) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.m == nil {
		s.m = make(map[string]*Clearance, 1)
	}
	s.m[key] = c.Clone()
	return nil
}

// fileName reduces a key to one path element, digesting it where that changed
// or truncated it so two keys cannot collide.
func fileName(key string) string {
	if key == "" {
		key = "default"
	}

	stem := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '.', r == '-', r == '_':
			return r
		}
		return '_'
	}, key)

	if stem != key || len(stem) > maxStem {
		sum := sha256.Sum256([]byte(key))
		stem = stem[:min(len(stem), maxStem)] + "-" + hex.EncodeToString(sum[:4])
	}
	return stem + ".cookies.txt"
}
