package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var (
	outputDir   string
	concurrency int
	connections int
	retries     int
	cookieFile  string
	browserPath string
	showBrowser bool
	noCover     bool
	noImages    bool
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
	f.StringVarP(&outputDir, "output", "o", "", "path to the download directory")
	f.IntVarP(&concurrency, "concurrency", "N", 3, "files to download at once")
	f.IntVarP(&connections, "connections", "j", 32, "ranged requests in flight, which is what sets speed (max 128)")
	f.IntVarP(&retries, "retries", "R", 4, "retry attempts per ranged request")
	f.StringVarP(&cookieFile, "cookies", "c", "", "path to a cookies.txt export, saved for later runs")
	f.StringVar(&browserPath, "use-browser", "", "path to a browser executable that clears a Cloudflare challenge")
	f.BoolVar(&showBrowser, "show-browser", false, "show the browser clearing a Cloudflare challenge instead of running it headless")
	f.BoolVarP(&noCover, "no-cover", "C", false, "do not embed cover art")
	f.BoolVarP(&noImages, "no-images", "I", false, "do not save the rest of the post's gallery")
	f.BoolVarP(&noChapters, "no-chapters", "H", false, "do not embed the track list as chapters")
	f.BoolVarP(&noTags, "no-tags", "T", false, "do not write title, artist or album metadata")
	f.BoolVarP(&verbose, "verbose", "v", false, "debug logging")

	f.VisitAll(func(fl *pflag.Flag) { fl.Value = upperType{fl.Value} })
}

type upperType struct{ pflag.Value }

func (u upperType) Type() string {
	t := u.Value.Type()
	if t == "bool" {
		return t
	}
	return strings.ToUpper(t)
}
