//go:build windows

package challenge

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// executables are tried in order. Chrome first: its User-Agent is the more
// ordinary one to wear.
var executables = []string{"chrome.exe", "msedge.exe"}

// appPaths records installed programs by executable name, and is checked before
// the well-known directories because it survives a custom install location.
const appPaths = `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\`

var installDirs = map[string]string{
	"chrome.exe": `Google\Chrome\Application`,
	"msedge.exe": `Microsoft\Edge\Application`,
}

// findBrowser returns the browser to drive, or "" when this machine has none.
func findBrowser() string {
	for _, exe := range executables {
		for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
			if path := regDefault(root, appPaths+exe); exists(path) {
				return path
			}
		}
	}

	// Machine-wide installs land under Program Files, per-user ones under LOCALAPPDATA.
	roots := []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LOCALAPPDATA")}
	for _, exe := range executables {
		for _, root := range roots {
			if root == "" {
				continue
			}
			if path := filepath.Join(root, installDirs[exe], exe); exists(path) {
				return path
			}
		}
	}
	return ""
}

// regDefault reads a key's unnamed value, where App Paths keeps the executable.
func regDefault(root registry.Key, path string) string {
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()

	v, _, err := k.GetStringValue("")
	if err != nil {
		return ""
	}
	return v
}

func exists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
