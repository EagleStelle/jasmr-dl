package util

import (
	"net/url"
	"path"
	"strings"
)

// ResolveURL makes a possibly-relative reference absolute against base. It
// returns "" unless the result is http(s), so a page-supplied "javascript:" or
// "data:" reference can never reach a fetch.
func ResolveURL(base *url.URL, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	abs := base.ResolveReference(u)
	if abs.Scheme != "http" && abs.Scheme != "https" {
		return ""
	}
	return abs.String()
}

// URLBase is a URL's final path element, percent-decoded, or "" where there is
// none. It reads only the path, so a query cannot hide the extension.
func URLBase(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	b := path.Base(u.Path)
	if b == "." || b == "/" {
		return ""
	}
	return b
}
