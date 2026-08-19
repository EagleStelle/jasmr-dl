//go:build !windows

package util

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// browserBinaries are asked their version in order; macOS installs are on no PATH.
var browserBinaries = []string{
	"google-chrome",
	"google-chrome-stable",
	"chromium",
	"chromium-browser",
	"microsoft-edge",
	"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
	"/Applications/Chromium.app/Contents/MacOS/Chromium",
	"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
}

const versionProbe = 3 * time.Second

// reportedVersion reads the version out of "Google Chrome 151.0.7258.67".
var reportedVersion = regexp.MustCompile(`(\d+)\.\d+\.\d+`)

func detectUserAgent() string {
	ctx, cancel := context.WithTimeout(context.Background(), versionProbe)
	defer cancel()

	for _, name := range browserBinaries {
		out, err := exec.CommandContext(ctx, name, "--version").Output()
		if err != nil {
			continue
		}
		m := reportedVersion.FindSubmatch(out)
		if m == nil {
			continue
		}

		major := string(m[1])
		ua := chromeUAFor(major)
		if strings.Contains(strings.ToLower(name), "edge") {
			ua += " Edg/" + major + ".0.0.0"
		}
		return ua
	}
	return ""
}
