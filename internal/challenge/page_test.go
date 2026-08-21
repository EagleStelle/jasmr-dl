package challenge

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	postURL     = "https://japaneseasmr.com/12345/"
	clearedPage = `<html><body><video><source src="https://v.example.xyz/RJ123456.mp3"/></video></body></html>`
)

func TestReadPageWaitsForTheDocumentToSettle(t *testing.T) {
	var (
		asked    atomic.Int32
		sessions atomic.Value
	)
	sessions.Store("")

	c := answer(t, func(cmd command) any {
		result := map[string]any{}
		switch cmd.Method {
		case "Target.attachToTarget":
			result["sessionId"] = "5e11"
		case "Runtime.evaluate":
			sessions.Store(cmd.SessionID)
			html := ""
			if asked.Add(1) >= 3 {
				html = clearedPage
			}
			result["result"] = map[string]any{"value": html}
		}
		return map[string]any{"id": cmd.ID, "result": result}
	})

	html, err := readPage(t.Context(), &browser{conn: c}, "9f0a", postURL)
	if err != nil {
		t.Fatal(err)
	}
	if html != clearedPage {
		t.Errorf("html = %q, want the cleared page", html)
	}
	if asked.Load() < 3 {
		t.Errorf("asked %d times, want it to have waited out the interstitial", asked.Load())
	}
	if got := sessions.Load(); got != "5e11" {
		t.Errorf("evaluated on session %q, want the attached one", got)
	}
}

func TestReadPageReportsAnException(t *testing.T) {
	c := answer(t, func(cmd command) any {
		result := map[string]any{}
		if cmd.Method == "Target.attachToTarget" {
			result["sessionId"] = "5e11"
		} else {
			result["exceptionDetails"] = map[string]any{"text": "Uncaught SecurityError"}
		}
		return map[string]any{"id": cmd.ID, "result": result}
	})

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	if _, err := readPage(ctx, &browser{conn: c}, "9f0a", postURL); err == nil {
		t.Fatal("got no error")
	} else if !strings.Contains(err.Error(), "SecurityError") {
		t.Errorf("error = %q, want the reason the page gave", err)
	}
}
