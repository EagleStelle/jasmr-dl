package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/EagleStelle/jasmr-dl/internal/downloader"
)

func clearance() *Clearance {
	return &Clearance{
		Cookies: []downloader.Cookie{{
			Domain: ".japaneseasmr.com",
			Name:   "cf_clearance",
			Value:  "abc",
			Path:   "/",
			Secure: true,
		}},
		UserAgent: "Mozilla/5.0 Chrome/151.0.0.0",
	}
}

func TestFileStoreRoundTrip(t *testing.T) {
	s := FileStore{Dir: t.TempDir()}
	ctx := context.Background()

	if err := s.Save(ctx, "japaneseasmr.com", clearance()); err != nil {
		t.Fatal(err)
	}

	got, err := s.Load(ctx, "japaneseasmr.com")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("nothing loaded back")
	}
	if got.UserAgent != clearance().UserAgent {
		t.Errorf("user-agent = %q, want %q", got.UserAgent, clearance().UserAgent)
	}
	if len(got.Cookies) != 1 || got.Cookies[0].Name != "cf_clearance" {
		t.Errorf("cookies = %+v, want the one saved", got.Cookies)
	}
}

func TestFileStoreLoadsNothingWhenEmpty(t *testing.T) {
	got, err := FileStore{Dir: t.TempDir()}.Load(context.Background(), "japaneseasmr.com")
	if err != nil {
		t.Fatalf("an absent file is not an error: %v", err)
	}
	if got != nil {
		t.Errorf("clearance = %+v, want none", got)
	}
}

func TestFileStoreKeepsKeysApart(t *testing.T) {
	s := FileStore{Dir: t.TempDir()}
	ctx := context.Background()
	keys := []string{"user1/japaneseasmr.com", "user1_japaneseasmr.com"}

	for _, key := range keys {
		c := clearance()
		c.UserAgent = key
		if err := s.Save(ctx, key, c); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range keys {
		got, err := s.Load(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		if got == nil || got.UserAgent != key {
			t.Errorf("key %q loaded %+v, want its own", key, got)
		}
	}
}

func TestFileStorePathStaysInsideDir(t *testing.T) {
	dir := t.TempDir()
	s := FileStore{Dir: dir}

	for _, key := range []string{"../escape", `..\escape`, "a/b/c"} {
		if got := filepath.Dir(s.Path(key)); got != dir {
			t.Errorf("key %q writes to %q, want %q", key, got, dir)
		}
	}
}

func TestFileStoreReadsAFallbackWithoutWritingIt(t *testing.T) {
	legacy := filepath.Join(t.TempDir(), "cookies.txt")
	if err := downloader.SaveCookies(legacy, clearance().UserAgent, clearance().Cookies); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	got, err := FileStore{Dir: dir, Fallbacks: []string{legacy}}.Load(context.Background(), "japaneseasmr.com")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.UserAgent != clearance().UserAgent {
		t.Fatalf("clearance = %+v, want the fallback's", got)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("loading wrote %d files, want none", len(entries))
	}
}

func TestMemoryStoreRoundTripCopies(t *testing.T) {
	var s MemoryStore
	ctx := context.Background()

	if got, err := s.Load(ctx, "japaneseasmr.com"); err != nil || got != nil {
		t.Fatalf("empty store gave %+v, %v; want nothing", got, err)
	}
	if err := s.Save(ctx, "japaneseasmr.com", clearance()); err != nil {
		t.Fatal(err)
	}

	got, err := s.Load(ctx, "japaneseasmr.com")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got.Cookies) != 1 {
		t.Fatalf("clearance = %+v, want the one saved", got)
	}

	got.Cookies[0].Value = "changed"
	again, err := s.Load(ctx, "japaneseasmr.com")
	if err != nil {
		t.Fatal(err)
	}
	if again.Cookies[0].Value != "abc" {
		t.Errorf("stored value = %q, want it untouched", again.Cookies[0].Value)
	}
}
