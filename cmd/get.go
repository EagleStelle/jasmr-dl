package cmd

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

const targetHost = "japaneseasmr.com"

var getCmd = &cobra.Command{
	Use:   "get <url>",
	Short: "Download every track from a japaneseasmr.com album page",
	Args:  cobra.ExactArgs(1),
	RunE:  runGet,
}

func init() {
	rootCmd.AddCommand(getCmd)
}

func runGet(cmd *cobra.Command, args []string) error {
	target, err := parseAlbumURL(args[0])
	if err != nil {
		return err
	}
	if concurrency < 1 {
		return fmt.Errorf("--concurrency must be at least 1, got %d", concurrency)
	}
	if retries < 0 {
		return fmt.Errorf("--retries cannot be negative, got %d", retries)
	}

	if verbose {
		cmd.Printf("url:         %s\n", target)
		cmd.Printf("output:      %s\n", outputDirOrDefault())
		cmd.Printf("concurrency: %d\n", concurrency)
		cmd.Printf("retries:     %d\n", retries)
		cmd.Printf("user-agent:  %s\n", userAgent)
	}

	return fmt.Errorf("scraper not implemented yet (phase 2)")
}

// parseAlbumURL rejects anything that is not an http(s) URL on the target host.
// Matching on the parsed hostname, not a substring, keeps
// japaneseasmr.com.evil.com from passing.
func parseAlbumURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("malformed URL %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("URL must be http or https, got %q", raw)
	}
	host := strings.ToLower(u.Hostname())
	if host != targetHost && host != "www."+targetHost {
		return nil, fmt.Errorf("not a %s URL: %q", targetHost, raw)
	}
	return u, nil
}

func outputDirOrDefault() string {
	if outputDir == "" {
		return "./<Album Title>"
	}
	return outputDir
}
