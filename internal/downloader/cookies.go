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

// uaPrefix carries the User-Agent the cookies were earned under, since
// cf_clearance is worthless without it. A browser export has no such line.
const uaPrefix = "# User-Agent: "

// Cookie is one cookie and its domain, as both the Netscape file and Chrome
// write it: a leading dot means "and every subdomain", a bare host means that
// host only.
type Cookie struct {
	Domain   string
	Name     string
	Value    string
	Path     string
	Expires  time.Time // zero for a session cookie
	Secure   bool
	HTTPOnly bool
}

// Host is the key a jar files the cookie under either way.
func (c Cookie) Host() string { return strings.TrimPrefix(c.Domain, ".") }

// Wildcard reports whether the cookie reaches subdomains.
func (c Cookie) Wildcard() bool { return strings.HasPrefix(c.Domain, ".") }

// LoadCookies reads a Netscape cookies.txt file, the format curl, wget, yt-dlp
// and browser export extensions share, plus the User-Agent recorded beside them
// when this tool wrote the file.
//
// cf_clearance is bound to the IP and User-Agent that cleared the challenge, so a
// hand-exported file must come from this machine.
func LoadCookies(path string) ([]Cookie, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()

	var (
		cookies   []Cookie
		userAgent string
	)

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024) // cf_clearance values are long
	for line := 1; sc.Scan(); line++ {
		// Trimming whitespace would eat the trailing tab of an empty value,
		// taking the field count with it.
		raw := strings.TrimRight(sc.Text(), "\r\n")
		if ua, ok := strings.CutPrefix(raw, uaPrefix); ok {
			userAgent = strings.TrimSpace(ua)
			continue
		}

		c, err := parseCookieLine(raw)
		if err != nil {
			return nil, "", fmt.Errorf("%s:%d: %w", path, line, err)
		}
		if c == nil {
			continue
		}
		cookies = append(cookies, *c)
	}
	if err := sc.Err(); err != nil {
		return nil, "", err
	}
	if len(cookies) == 0 {
		return nil, "", fmt.Errorf("%s holds no cookies", path)
	}
	return cookies, userAgent, nil
}

// SaveCookies writes the file LoadCookies reads. Mode 0600: cf_clearance is a
// credential while it lasts.
func SaveCookies(path, userAgent string, cookies []Cookie) error {
	var b strings.Builder
	b.WriteString("# Netscape HTTP Cookie File\n")
	if userAgent != "" {
		b.WriteString(uaPrefix + userAgent + "\n")
	}
	b.WriteString("\n")

	for _, c := range cookies {
		var expires int64 // 0 is a session cookie
		if !c.Expires.IsZero() {
			expires = c.Expires.Unix()
		}
		cookiePath := c.Path
		if cookiePath == "" {
			cookiePath = "/"
		}
		if c.HTTPOnly {
			b.WriteString(httpOnlyPrefix)
		}
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
			c.Domain, flag(c.Wildcard()), cookiePath, flag(c.Secure), expires, c.Name, c.Value)
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

// NewJar files cookies under the host each names. A jar is keyed by URL, so they
// are grouped first rather than walking its locking once per cookie.
func NewJar(cookies []Cookie) (http.CookieJar, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	byHost := make(map[string][]*http.Cookie)
	order := make([]string, 0, 8)
	for _, c := range cookies {
		host := c.Host()
		if _, seen := byHost[host]; !seen {
			order = append(order, host)
		}

		hc := &http.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Path:     c.Path,
			Expires:  c.Expires,
			Secure:   c.Secure,
			HttpOnly: c.HTTPOnly,
		}
		// A leading dot means "and every subdomain", which is how the jar reads
		// a bare Domain; an unset one is host-only.
		if c.Wildcard() {
			hc.Domain = host
		}
		byHost[host] = append(byHost[host], hc)
	}

	for _, host := range order {
		u := &url.URL{Scheme: "https", Host: host, Path: "/"}
		jar.SetCookies(u, byHost[host])
	}
	return jar, nil
}

// parseCookieLine reads one entry. A nil cookie with a nil error means the line
// held none.
func parseCookieLine(raw string) (*Cookie, error) {
	var httpOnly bool
	switch {
	case strings.TrimSpace(raw) == "":
		return nil, nil
	case strings.HasPrefix(raw, httpOnlyPrefix):
		raw, httpOnly = strings.TrimPrefix(raw, httpOnlyPrefix), true
	case strings.HasPrefix(raw, "#"):
		return nil, nil
	}

	// Split on tabs, not whitespace, so an empty value stays one field.
	f := strings.Split(raw, "\t")
	if len(f) < 7 {
		return nil, fmt.Errorf("expected 7 tab-separated fields, got %d", len(f))
	}

	c := &Cookie{
		Domain:   f[0],
		Path:     f[2],
		Secure:   strings.EqualFold(f[3], "TRUE"),
		Name:     f[5],
		Value:    f[6],
		HTTPOnly: httpOnly,
	}
	if c.Host() == "" {
		return nil, fmt.Errorf("empty domain")
	}
	if c.Name == "" {
		return nil, fmt.Errorf("empty cookie name")
	}

	expires, err := strconv.ParseInt(f[4], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("malformed expiry %q: %w", f[4], err)
	}
	if expires > 0 { // 0 is a session cookie
		c.Expires = time.Unix(expires, 0)
	}
	return c, nil
}

// flag writes the format's uppercase booleans.
func flag(v bool) string {
	if v {
		return "TRUE"
	}
	return "FALSE"
}
