package app

import (
	"fmt"
	"net/url"
	"strings"
)

// ParseTarget normalises one post URL, matching the hostname rather than a
// substring so evil.com cannot pass.
func ParseTarget(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("malformed URL %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("URL must be http or https, got %q", raw)
	}
	host := strings.ToLower(u.Hostname())
	if host != Host && host != "www."+Host {
		return "", fmt.Errorf("not a %s URL: %q", Host, raw)
	}
	return u.String(), nil
}

// ParseTargets runs raw through ParseTarget, dropping repeats. onRepeat may be nil.
func ParseTargets(raw []string, onRepeat func(target string)) ([]string, error) {
	seen := make(map[string]bool, len(raw))
	targets := make([]string, 0, len(raw))

	for _, r := range raw {
		target, err := ParseTarget(r)
		if err != nil {
			return nil, err
		}
		if seen[target] {
			if onRepeat != nil {
				onRepeat(target)
			}
			continue
		}
		seen[target] = true
		targets = append(targets, target)
	}
	return targets, nil
}
