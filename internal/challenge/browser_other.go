//go:build !windows

package challenge

import (
	"os"
	"os/exec"
)

// executables are tried in order; the names differ by distribution.
var executables = []string{
	"google-chrome",
	"google-chrome-stable",
	"chromium",
	"chromium-browser",
	"microsoft-edge",
}

// bundles are the macOS installs, which are on no PATH.
var bundles = []string{
	"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
	"/Applications/Chromium.app/Contents/MacOS/Chromium",
	"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
}

// findBrowser returns the browser to drive, or "" when this machine has none.
func findBrowser() string {
	for _, name := range executables {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	for _, path := range bundles {
		if exists(path) {
			return path
		}
	}
	return ""
}

func exists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
