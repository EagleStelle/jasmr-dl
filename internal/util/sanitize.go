package util

import (
	"path/filepath"
	"regexp"
	"strings"
)

var illegal = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

// reserved names are device names on Windows; a file cannot be created with
// one even with an extension appended.
var reserved = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// audioExts is an allowlist, not a blocklist. Link targets come from a file
// host that is documented to also carry executables, so anything not named
// here is refused rather than filtered.
var audioExts = map[string]bool{
	".m4a":  true,
	".mp3":  true,
	".flac": true,
	".opus": true,
	".wav":  true,
}

// imageExts is an allowlist for the same reason audioExts is: a gallery link is
// page content, so nothing decides what it points at except this list.
var imageExts = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".webp": true,
	".gif":  true,
}

// maxNameRunes leaves room for the album directory inside the 255-unit limit
// most filesystems place on a single path component.
const maxNameRunes = 120

// Sanitize makes name safe to use as a single path component. It never returns
// a name containing a separator, so callers can join it without re-checking.
func Sanitize(name string) string {
	name = illegal.ReplaceAllString(name, "_")
	name = strings.TrimSpace(name)
	name = strings.Trim(name, ".")
	if name == "" {
		return "untitled"
	}

	// Truncate by runes, not bytes. Titles here are Japanese, 3 bytes per
	// character in UTF-8, so a byte slice can split a rune and produce an
	// invalid filename.
	if r := []rune(name); len(r) > maxNameRunes {
		name = strings.TrimSpace(string(r[:maxNameRunes]))
	}

	stem := name
	if i := strings.LastIndex(name, "."); i > 0 {
		stem = name[:i]
	}
	if reserved[strings.ToUpper(stem)] {
		name = "_" + name
	}
	return name
}

// IsAudioFile reports whether name carries an allowlisted audio extension.
func IsAudioFile(name string) bool {
	return audioExts[strings.ToLower(filepath.Ext(strings.TrimSpace(name)))]
}

// IsImageFile reports whether name carries an allowlisted image extension.
func IsImageFile(name string) bool {
	return imageExts[strings.ToLower(filepath.Ext(strings.TrimSpace(name)))]
}
