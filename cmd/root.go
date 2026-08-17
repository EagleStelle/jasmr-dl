package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	outputDir   string
	concurrency int
	retries     int
	cookieFile  string
	noCover     bool
	noChapters  bool
	noTags      bool
	verbose     bool
)

var rootCmd = &cobra.Command{
	Use: "jasmr-dl <url>",
	Example: "  jasmr-dl https://japaneseasmr.com/12345/\n" +
		"  jasmr-dl https://japaneseasmr.com/12345/ -o ./out -N 8\n" +
		"  jasmr-dl https://japaneseasmr.com/12345/ --no-cover\n" +
		"  jasmr-dl https://japaneseasmr.com/12345/ -c C:\\path\\cookies.txt",
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		return runDownload(cmd, args)
	},
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the CLI and exits non-zero on failure.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "[error]", err)
		os.Exit(1)
	}
}

func init() {
	f := rootCmd.PersistentFlags()
	f.StringVarP(&outputDir, "output", "o", "", "download directory (default: the album title)")
	f.IntVarP(&concurrency, "concurrency", "N", 3, "files to download at once")
	f.IntVarP(&retries, "retries", "R", 4, "per-file retry attempts")
	f.StringVarP(&cookieFile, "cookies", "c", "", "path to a cookies.txt export, saved for later runs")
	f.BoolVarP(&noCover, "no-cover", "C", false, "do not embed cover art")
	f.BoolVarP(&noChapters, "no-chapters", "H", false, "do not embed the track list as chapters")
	f.BoolVarP(&noTags, "no-tags", "T", false, "do not write title, artist or album metadata")
	f.BoolVarP(&verbose, "verbose", "v", false, "debug logging")
}
