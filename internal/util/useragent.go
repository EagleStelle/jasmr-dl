package util

import (
	"fmt"
	"regexp"
	"sync"
)

// chromeUA is Chrome's User-Agent with the major version left open. Chrome
// itself reports the minor parts as zeroes, so this is the real shape.
const chromeUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) " +
	"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s.0.0.0 Safari/537.36"

// fallbackMajor is used when no installed browser version can be read.
const fallbackMajor = "151"

var chromeVersion = regexp.MustCompile(`Chrome/(\d+)`)

// UserAgent is the User-Agent every request sends, read from the browser
// installed on this machine.
//
// Cloudflare binds cf_clearance to the User-Agent that solved the challenge, so
// a pinned string silently stops matching the moment the browser updates and
// every exported cookie is refused. Detecting it keeps the two in step.
var UserAgent = sync.OnceValue(func() string {
	if ua := detectUserAgent(); ua != "" {
		return ua
	}
	return chromeUAFor(fallbackMajor)
})

func chromeUAFor(major string) string {
	return fmt.Sprintf(chromeUA, major)
}

// ChromeMajorVersion reads the Chrome major version a User-Agent claims, so the
// Sec-CH-UA hints sent with it agree. It returns "" for a non-Chrome UA.
func ChromeMajorVersion(userAgent string) string {
	m := chromeVersion.FindStringSubmatch(userAgent)
	if m == nil {
		return ""
	}
	return m[1]
}
