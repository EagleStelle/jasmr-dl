package challenge

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

// answer serves a DevTools endpoint replying to each command with reply(cmd).
func answer(t *testing.T, reply func(command) any) *conn {
	t.Helper()

	srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		for {
			var cmd command
			if err := websocket.JSON.Receive(ws, &cmd); err != nil {
				return
			}
			if err := websocket.JSON.Send(ws, reply(cmd)); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	c, err := dial("ws" + strings.TrimPrefix(srv.URL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestCallRoundTrip(t *testing.T) {
	c := answer(t, func(cmd command) any {
		return map[string]any{
			"id":     cmd.ID,
			"result": map[string]any{"userAgent": "answered " + cmd.Method},
		}
	})

	var got struct {
		UserAgent string `json:"userAgent"`
	}
	for _, method := range []string{"Browser.getVersion", "Storage.getCookies"} {
		if err := c.call(t.Context(), method, nil, &got); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		if want := "answered " + method; got.UserAgent != want {
			t.Errorf("got %q, want %q", got.UserAgent, want)
		}
	}
}

// Events carry no id and must not be mistaken for the reply being waited on.
func TestCallIgnoresEvents(t *testing.T) {
	srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		var cmd command
		if err := websocket.JSON.Receive(ws, &cmd); err != nil {
			return
		}
		_ = websocket.JSON.Send(ws, map[string]any{
			"method": "Target.targetCreated",
			"params": map[string]any{"targetInfo": map[string]any{"type": "page"}},
		})
		_ = websocket.JSON.Send(ws, map[string]any{
			"id":     cmd.ID,
			"result": map[string]any{"targetId": "9f0a"},
		})
	}))
	defer srv.Close()

	c, err := dial("ws" + strings.TrimPrefix(srv.URL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	var got struct {
		TargetID string `json:"targetId"`
	}
	if err := c.call(t.Context(), "Target.createTarget", map[string]any{"url": "about:blank"}, &got); err != nil {
		t.Fatal(err)
	}
	if got.TargetID != "9f0a" {
		t.Errorf("targetId = %q, want 9f0a", got.TargetID)
	}
}

func TestCallReportsProtocolError(t *testing.T) {
	c := answer(t, func(cmd command) any {
		return map[string]any{
			"id":    cmd.ID,
			"error": map[string]any{"code": -32601, "message": "'Nope.nope' wasn't found"},
		}
	})

	err := c.call(t.Context(), "Nope.nope", nil, nil)
	if err == nil {
		t.Fatal("got no error")
	}
	if !strings.Contains(err.Error(), "wasn't found") || !strings.Contains(err.Error(), "Nope.nope") {
		t.Errorf("error = %q, want the method and the reason", err)
	}
}

// A browser that dies mid-solve must fail the call rather than hang it.
func TestCallWakesOnDisconnect(t *testing.T) {
	srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		var cmd command
		_ = websocket.JSON.Receive(ws, &cmd)
		ws.Close()
	}))
	defer srv.Close()

	c, err := dial("ws" + strings.TrimPrefix(srv.URL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := c.call(ctx, "Storage.getCookies", nil, nil); err == nil {
		t.Fatal("got no error")
	} else if ctx.Err() != nil {
		t.Fatalf("waited for the deadline instead of noticing the socket: %v", err)
	}
}

func TestEndpointWatcher(t *testing.T) {
	w := &endpointWatcher{found: make(chan string, 1)}

	// stderr arrives in arbitrary chunks, so a half-written URL must not match.
	if _, err := w.Write([]byte("[0818/2231] Starting\nDevTools listening on ws://127.0.0")); err != nil {
		t.Fatal(err)
	}
	select {
	case u := <-w.found:
		t.Fatalf("matched a half-written line: %q", u)
	default:
	}

	if _, err := w.Write([]byte(".1:52341/devtools/browser/9f0a\n[0818/2231] noise\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-w.found:
		const want = "ws://127.0.0.1:52341/devtools/browser/9f0a"
		if got != want {
			t.Errorf("endpoint = %q, want %q", got, want)
		}
	default:
		t.Fatal("no endpoint found")
	}
}

// No endpoint ever comes, so the buffer must not grow with every line.
func TestEndpointWatcherBoundsItsBuffer(t *testing.T) {
	w := &endpointWatcher{found: make(chan string, 1)}

	line := strings.Repeat("x", 4096) + "\n"
	for range 64 {
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if len(w.buf) > maxWatched {
		t.Errorf("buffered %d bytes, want at most %d", len(w.buf), maxWatched)
	}
}
