package challenge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

// closeGrace is how long the browser gets to exit before it is killed. Asking
// first lets the profile reach disk, which is the point of keeping one.
const closeGrace = 5 * time.Second

// browser is a running browser and the DevTools connection to it.
type browser struct {
	cmd    *exec.Cmd
	conn   *conn
	exited chan struct{}
}

// launch starts the browser and connects to it. The caller must Close the result.
func launch(ctx context.Context, exe string, o Options, userAgent, profileDir string) (*browser, error) {
	watcher := &endpointWatcher{found: make(chan string, 1)}

	cmd := exec.Command(exe, browserArgs(o, profileDir, userAgent)...)
	// A writer rather than a pipe keeps Wait clear of the "read it all first" rule.
	cmd.Stderr = watcher
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", exe, err)
	}

	b := &browser{cmd: cmd, exited: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(b.exited)
	}()

	var wsURL string
	select {
	case <-ctx.Done():
		b.Close()
		return nil, ctx.Err()
	case <-b.exited:
		return nil, fmt.Errorf("%s exited before naming a DevTools endpoint", filepath.Base(exe))
	case wsURL = <-watcher.found:
	}

	conn, err := dial(wsURL)
	if err != nil {
		b.Close()
		return nil, err
	}
	b.conn = conn
	return b, nil
}

// browserArgs are the flags a solve runs under. The absences matter as much:
// --enable-automation sets navigator.webdriver, and --disable-gpu strips the
// WebGL details a challenge reads.
func browserArgs(o Options, profileDir, userAgent string) []string {
	args := []string{
		"--remote-debugging-port=0", // read back off stderr, so nothing races for a fixed one
		"--remote-allow-origins=*",  // the handshake must send an Origin; Chrome 111+ refuses unknown ones
		"--user-data-dir=" + profileDir,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-blink-features=AutomationControlled",
		"--window-size=1280,800",
		"--lang=ja,en-US",
	}
	if userAgent != "" {
		args = append(args, "--user-agent="+userAgent)
	}
	if !o.Visible {
		args = append(args, "--headless=new")
	}
	args = append(args, o.Args...) // last, so a caller's flag wins

	// about:blank keeps the new tab page from reaching out on its own.
	return append(args, "about:blank")
}

// Close ends the browser, asking before killing it.
func (b *browser) Close() {
	if b.conn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), closeGrace)
		_ = b.conn.call(ctx, "Browser.close", nil, nil)
		cancel()
		b.conn.Close()
	}
	if b.cmd.Process == nil {
		return
	}

	select {
	case <-b.exited:
	case <-time.After(closeGrace):
		_ = b.cmd.Process.Kill()
		<-b.exited
	}
}

var devToolsURL = regexp.MustCompile(`ws://\S+`)

// endpointWatcher stands in for the browser's stderr, waiting for the one line
// that names the endpoint and discarding the rest.
type endpointWatcher struct {
	found chan string
	buf   []byte
	done  bool
}

// maxWatched bounds what is held while waiting.
const maxWatched = 64 << 10

// Write is only ever called by the goroutine os/exec copies output on, so the
// buffer needs no lock.
func (w *endpointWatcher) Write(p []byte) (int, error) {
	if w.done {
		return len(p), nil
	}
	w.buf = append(w.buf, p...)

	// Whole lines only: a URL split across two writes would match on its first half.
	if end := bytes.LastIndexByte(w.buf, '\n'); end >= 0 {
		if u := devToolsURL.Find(w.buf[:end]); u != nil {
			w.done, w.buf = true, nil
			w.found <- string(u)
			return len(p), nil
		}
		w.buf = w.buf[end+1:]
	}
	if len(w.buf) > maxWatched {
		w.buf = w.buf[len(w.buf)-maxWatched:]
	}
	return len(p), nil
}

// conn is a Chrome DevTools Protocol connection: JSON over a WebSocket, each
// command answered by the message carrying its id.
//
// Only browser-level commands are used, so nothing attaches to a page and the
// Runtime domain, which a challenge watches for, is never enabled.
type conn struct {
	ws  *websocket.Conn
	wmu sync.Mutex // one writer at a time on the socket

	mu      sync.Mutex
	nextID  int
	pending map[int]chan reply
	closed  error
}

type command struct {
	ID     int    `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type reply struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *protocolError  `json:"error"`
}

type protocolError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *protocolError) Error() string { return fmt.Sprintf("%s (%d)", e.Message, e.Code) }

// dial connects to the browser's DevTools endpoint. The Origin is there only
// because the handshake demands one.
func dial(wsURL string) (*conn, error) {
	cfg, err := websocket.NewConfig(wsURL, "http://127.0.0.1")
	if err != nil {
		return nil, err
	}
	ws, err := websocket.DialConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to DevTools: %w", err)
	}

	c := &conn{ws: ws, pending: make(map[int]chan reply)}
	go c.read()
	return c, nil
}

func (c *conn) Close() error { return c.ws.Close() }

// call sends one command and unmarshals its result into out, which may be nil.
func (c *conn) call(ctx context.Context, method string, params, out any) error {
	c.mu.Lock()
	if c.closed != nil {
		err := c.closed
		c.mu.Unlock()
		return fmt.Errorf("%s: %w", method, err)
	}
	c.nextID++
	id := c.nextID
	ch := make(chan reply, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	c.wmu.Lock()
	err := websocket.JSON.Send(c.ws, command{ID: id, Method: method, Params: params})
	c.wmu.Unlock()
	if err != nil {
		c.forget(id)
		return fmt.Errorf("%s: %w", method, err)
	}

	select {
	case <-ctx.Done():
		c.forget(id)
		return ctx.Err()
	case msg, ok := <-ch:
		switch {
		case !ok: // the socket ended first
			return fmt.Errorf("%s: %w", method, c.reason())
		case msg.Error != nil:
			return fmt.Errorf("%s: %w", method, msg.Error)
		case out == nil:
			return nil
		}
		return json.Unmarshal(msg.Result, out)
	}
}

// read hands each reply to whoever is waiting on it, until the socket ends.
func (c *conn) read() {
	for {
		var msg reply
		if err := websocket.JSON.Receive(c.ws, &msg); err != nil {
			c.fail(err)
			return
		}
		// Events carry no id, and nothing here subscribes to any.
		if msg.ID == 0 {
			continue
		}

		c.mu.Lock()
		ch, ok := c.pending[msg.ID]
		delete(c.pending, msg.ID)
		c.mu.Unlock()
		if ok {
			ch <- msg
		}
	}
}

// fail wakes everyone still waiting, since no further reply is coming.
func (c *conn) fail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed == nil {
		c.closed = err
	}
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
}

func (c *conn) forget(id int) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *conn) reason() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed == nil {
		return fmt.Errorf("the DevTools connection ended")
	}
	return c.closed
}
