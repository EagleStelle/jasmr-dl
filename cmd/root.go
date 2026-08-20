package cmd

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// version is the build revision. A release build replaces it via -ldflags -X,
// which the linker only honours on a variable holding a constant string, so it
// has to start life as a plain literal.
var version = "dev"

// resolveVersion keeps whatever -ldflags wrote, falling back to the version the
// module proxy recorded.
func resolveVersion() string {
	if version != "dev" {
		return version
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok || bi.Main.Version == "" || bi.Main.Version == "(devel)" {
		return version
	}
	// goreleaser strips the tag's leading v; match it, whichever path built us.
	return strings.TrimPrefix(bi.Main.Version, "v")
}

// options is every flag, gathered rather than scattered across package globals
// so nothing downstream reads one.
type options struct {
	outputTmpl  string
	basePath    string
	batchFile   string
	concurrency int
	connections int
	retries     int
	cookieFile  string
	browserPath string
	showBrowser bool
	noMetadata  bool
	noCover    bool
	noImages    bool
	noChapters  bool
	noSplit     bool
	verbose     bool
}

var opts options

var rootCmd = &cobra.Command{
	Use:     "jasmr-dl <url>...",
	Version: resolveVersion(),
	Example: "  jasmr-dl https://japaneseasmr.com/12345/\n" +
		"  jasmr-dl https://japaneseasmr.com/12345/ https://japaneseasmr.com/12346/\n" +
		"  jasmr-dl -a urls.txt\n" +
		"  jasmr-dl https://japaneseasmr.com/12345/ -j 64 -N 8\n" +
		"  jasmr-dl https://japaneseasmr.com/12345/ -o \"{circle}/{rjcode}/{number}. {chapter}.{ext}\"\n" +
		"  jasmr-dl https://japaneseasmr.com/12345/ -o \"C:/Audio/{year}/{title}/{rjcode}_{number}.{ext}\"\n" +
		"  jasmr-dl https://japaneseasmr.com/12345/ -o \"<*|{number}_{chapter} [{circle}].{ext}>\"\n" +
		"  jasmr-dl https://japaneseasmr.com/12345/ -P ./out -o \"{rjcode}/{number}.{ext}\"\n" +
		"  jasmr-dl https://japaneseasmr.com/12345/ -M -C -I\n" +
		"  jasmr-dl https://japaneseasmr.com/12345/ -c C:\\path\\cookies.txt",
	Args: cobra.ArbitraryArgs,
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		return applyConfig(cmd)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 && opts.batchFile == "" {
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
	registerFlags(rootCmd.PersistentFlags(), &opts)
	// The set --help reads from is not the one the flags were registered on.
	rootCmd.Flags().SortFlags = false
}

func registerFlags(f *pflag.FlagSet, opts *options) {
	f.StringVarP(&opts.outputTmpl, "output", "o", "",
		"template naming each file and the directories above it")
	f.StringVarP(&opts.basePath, "paths", "P", "", "directory everything is written under")
	f.StringVarP(&opts.batchFile, "batch-file", "a", "", "path to a URL list, one per line, or - for stdin")
	f.IntVarP(&opts.concurrency, "concurrency", "N", 3, "posts, and files within them, downloaded at once")
	f.IntVarP(&opts.connections, "connections", "j", 32, "ranged requests in flight, across every post (max 128)")
	f.IntVarP(&opts.retries, "retries", "R", 4, "retry attempts per ranged request")
	f.StringVarP(&opts.cookieFile, "cookies", "c", "", "path to a cookies.txt export, kept for later runs")
	f.StringVar(&opts.browserPath, "use-browser", "", "path to a browser executable that clears a Cloudflare challenge")
	f.BoolVar(&opts.showBrowser, "show-browser", false, "show that browser instead of running it headless")
	f.BoolVarP(&opts.noMetadata, "no-metadata", "M", false, "do not write title, artist or album metadata")
	f.BoolVarP(&opts.noCover, "no-cover", "C", false, "do not embed cover art")
	f.BoolVarP(&opts.noImages, "no-images", "I", false, "do not save the rest of the post's gallery")
	f.BoolVarP(&opts.noChapters, "no-chapters", "H", false, "do not use the track list: no chapters, no split")
	f.BoolVarP(&opts.noSplit, "no-split", "S", false, "do not cut a chaptered stream into one file per chapter")
	f.BoolVarP(&opts.verbose, "verbose", "v", false, "debug logging on stderr")

	// --help lists flags as registered above, rather than alphabetically.
	f.SortFlags = false

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
