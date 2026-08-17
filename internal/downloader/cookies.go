package downloader

import (
	"bufio"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// httpOnlyPrefix marks a cookie hidden from scripts. cf_clearance is one, so the
// line is stripped rather than skipped as the comment it looks like.
const httpOnlyPrefix = "#HttpOnly_"

// LoadCookieJar reads a Netscape cookies.txt file, the format curl, wget, yt-dlp
// and browser export extensions share.
//
// cf_clearance is bound to the IP and User-Agent that solved the challenge, so
// the export must come from this machine and --user-agent must match it.
func LoadCookieJar(path string) (http.CookieJar, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	// A jar is keyed by URL, so group by host rather than walking its locking
	// once per cookie.
	byHost := make(map[string][]*http.Cookie)
	order := make([]string, 0, 8)

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024) // cf_clearance values are long
	for line := 1; sc.Scan(); line++ {
		c, host, err := parseCookieLine(sc.Text())
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		if c == nil {
			continue
		}
		if _, seen := byHost[host]; !seen {
			order = append(order, host)
		}
		byHost[host] = append(byHost[host], c)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(order) == 0 {
		return nil, fmt.Errorf("%s holds no cookies", path)
	}

	for _, host := range order {
		u := &url.URL{Scheme: "https", Host: host, Path: "/"}
		jar.SetCookies(u, byHost[host])
	}
	return jar, nil
}

// parseCookieLine reads one entry and the host to file it under. A nil cookie
// with a nil error means the line held none.
func parseCookieLine(line string) (*http.Cookie, string, error) {
	// Trimming whitespace would eat the trailing tab of an empty value, taking
	// the field count with it.
	raw := strings.TrimRight(line, "\r\n")
	switch {
	case strings.TrimSpace(raw) == "":
		return nil, "", nil
	case strings.HasPrefix(raw, httpOnlyPrefix):
		raw = strings.TrimPrefix(raw, httpOnlyPrefix)
	case strings.HasPrefix(raw, "#"):
		return nil, "", nil
	}

	// Split on tabs, not whitespace, so an empty value stays one field.
	f := strings.Split(raw, "\t")
	if len(f) < 7 {
		return nil, "", fmt.Errorf("expected 7 tab-separated fields, got %d", len(f))
	}

	domain, path, name, value := f[0], f[2], f[5], f[6]
	host := strings.TrimPrefix(domain, ".")
	if host == "" {
		return nil, "", fmt.Errorf("empty domain")
	}
	if name == "" {
		return nil, "", fmt.Errorf("empty cookie name")
	}

	expires, err := strconv.ParseInt(f[4], 10, 64)
	if err != nil {
		return nil, "", fmt.Errorf("malformed expiry %q: %w", f[4], err)
	}

	c := &http.Cookie{
		Name:   name,
		Value:  value,
		Path:   path,
		Secure: strings.EqualFold(f[3], "TRUE"),
	}
	// A leading dot means "and every subdomain", which is how the jar reads a
	// bare Domain; an unset one is host-only.
	if strings.HasPrefix(domain, ".") {
		c.Domain = host
	}
	if expires > 0 { // 0 is a session cookie
		c.Expires = time.Unix(expires, 0)
	}
	return c, host, nil
}
