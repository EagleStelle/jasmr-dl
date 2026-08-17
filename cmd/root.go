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
	ffmpegPath  string
	noCover     bool
	noChapters  bool
	noTags      bool
	verbose     bool
)

var rootCmd = &cobra.Command{
	Use:   "jasmr-dl <url>",
	Short: "Download audio albums from japaneseasmr.com",
	Long: "jasmr-dl downloads every track on a japaneseasmr.com album page,\n" +
		"straight from the page's own players, with resume and retry.\n\n" +
		"Album art and, where the post has them, chapters are embedded in the\n" +
		"output. Posts that serve only the site's stream are reassembled with\n" +
		"ffmpeg, which must be installed for those.",
	Example: "  jasmr-dl https://japaneseasmr.com/12345/\n" +
		"  jasmr-dl https://japaneseasmr.com/12345/ -o ./out -c 8\n" +
		"  jasmr-dl https://japaneseasmr.com/12345/ --no-cover",
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
		fmt.Fprintln(os.Stderr, "[error]", err)
		os.Exit(1)
	}
}

func init() {
	f := rootCmd.PersistentFlags()
	f.StringVarP(&outputDir, "output", "o", "", "download directory (default: the album title)")
	f.IntVarP(&concurrency, "concurrency", "c", 3, "files to download at once")
	f.IntVarP(&retries, "retries", "r", 4, "per-file retry attempts")
	f.StringVarP(&userAgent, "user-agent", "u", defaultUserAgent, "User-Agent sent with every request")
	f.BoolVarP(&noCover, "no-cover", "C", false, "do not embed album art")
	f.BoolVarP(&noChapters, "no-chapters", "H", false, "do not embed the track list as chapters")
	f.BoolVarP(&noTags, "no-tags", "T", false, "do not write title, artist or album metadata")
	f.StringVarP(&ffmpegPath, "ffmpeg", "f", "", "ffmpeg binary, needed for HLS posts and cover art (default: found on PATH)")
	f.BoolVarP(&verbose, "verbose", "v", false, "debug logging")
}
