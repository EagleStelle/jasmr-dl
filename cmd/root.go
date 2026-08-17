package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// defaultUserAgent spoofs a real browser. The Go default ("Go-http-client/1.1")
// is widely blocked.
const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) " +
	"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

var (
	outputDir   string
	concurrency int
	retries     int
	userAgent   string
	combined    bool
	verbose     bool
)

var rootCmd = &cobra.Command{
	Use:   "jasmr-dl [url]",
	Short: "Download audio albums from japaneseasmr.com",
	Long: "jasmr-dl downloads every track on a japaneseasmr.com album page,\n" +
		"concurrently, with resume and retry.\n\n" +
		"A bare URL is shorthand for the get subcommand.",
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		return runGet(cmd, args)
	},
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the CLI and exits non-zero on failure.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func init() {
	f := rootCmd.PersistentFlags()
	f.StringVarP(&outputDir, "output", "o", "", "download directory (default ./<Album Title>)")
	f.IntVarP(&concurrency, "concurrency", "c", 3, "simultaneous downloads")
	f.IntVar(&retries, "retries", 4, "per-file retry attempts")
	f.StringVar(&userAgent, "user-agent", defaultUserAgent, "User-Agent sent with every request")
	f.BoolVar(&combined, "combined", false, "also download the combined whole-work file")
	f.BoolVarP(&verbose, "verbose", "v", false, "debug logging")
}
