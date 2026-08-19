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

// UserAgent is the fallback User-Agent, read from the browser installed on this
// machine. A run carries its own, and a clearance brings the one it was earned
// under; either wins over this.
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
