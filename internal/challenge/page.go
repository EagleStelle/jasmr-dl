package challenge

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const pageTimeout = 30 * time.Second

// settledHTML returns the document, or "" while the tab is still moving: a
// challenge running, a load unfinished, or the empty document a navigation
// commits before the real one.
const settledHTML = `(() => {
  if (window._cf_chl_opt || document.readyState !== "complete") return "";
  if (!document.body || !document.body.firstElementChild) return "";
  return document.documentElement.outerHTML;
})()`

// readPage returns the cleared page as the browser holds it, so the clearance
// never has to be replayed from another client.
func readPage(ctx context.Context, b *browser, targetID, pageURL string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, pageTimeout)
	defer cancel()

	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := b.conn.call(ctx, "Target.attachToTarget", map[string]any{
		"targetId": targetID,
		"flatten":  true,
	}, &attached); err != nil {
		return "", err
	}

	// The tab is wherever the interstitial left it, so ask for the page itself
	// now that the cookie will carry it.
	if err := b.conn.callOn(ctx, attached.SessionID, "Page.navigate", map[string]any{"url": pageURL}, nil); err != nil {
		return "", err
	}

	t := time.NewTicker(pollInterval)
	defer t.Stop()

	var last error
	for {
		html, err := evaluate(ctx, b, attached.SessionID, settledHTML)
		switch {
		// A call the deadline cut short says nothing about the page.
		case err != nil && ctx.Err() == nil:
			last = err
		case html != "":
			return html, nil
		}

		select {
		case <-t.C:
		case <-ctx.Done():
			if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return "", ctx.Err()
			}
			if last != nil {
				return "", fmt.Errorf("read the cleared page: %w", last)
			}
			return "", fmt.Errorf("the cleared page had not settled after %s", pageTimeout)
		}
	}
}

func evaluate(ctx context.Context, b *browser, sessionID, expression string) (string, error) {
	var out struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
		Exception *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := b.conn.callOn(ctx, sessionID, "Runtime.evaluate", map[string]any{
		"expression":    expression,
		"returnByValue": true,
	}, &out); err != nil {
		return "", err
	}
	if out.Exception != nil {
		return "", fmt.Errorf("the page raised %s", out.Exception.Text)
	}
	return out.Result.Value, nil
}
