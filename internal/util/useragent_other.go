//go:build !windows

package util

// detectUserAgent has no non-Windows implementation; the fallback version stands.
func detectUserAgent() string { return "" }
