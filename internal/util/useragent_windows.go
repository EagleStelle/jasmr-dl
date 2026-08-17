//go:build windows

package util

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

// browserVersionKeys are where Chromium browsers record the installed version.
// BLBeacon is checked first: it is per-user and written on every update, where
// the Update\Clients keys only exist for a machine-wide install.
var browserVersionKeys = []struct {
	root registry.Key
	path string
	name string
	edge bool
}{
	{registry.CURRENT_USER, `Software\Google\Chrome\BLBeacon`, "version", false},
	{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Google\Update\Clients\{8A69D345-D564-463c-AFF1-A69D9E530F96}`, "pv", false},
	{registry.CURRENT_USER, `Software\Microsoft\Edge\BLBeacon`, "version", true},
	{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{56EB18F8-B008-4CBD-B6D2-8C97FE7E9062}`, "pv", true},
}

func detectUserAgent() string {
	for _, k := range browserVersionKeys {
		major, _, _ := strings.Cut(regValue(k.root, k.path, k.name), ".")
		if major == "" {
			continue
		}
		ua := chromeUAFor(major)
		if k.edge {
			ua += " Edg/" + major + ".0.0.0"
		}
		return ua
	}
	return ""
}

func regValue(root registry.Key, path, name string) string {
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()

	v, _, err := k.GetStringValue(name)
	if err != nil {
		return ""
	}
	return v
}
