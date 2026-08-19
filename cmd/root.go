package cmd

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/EagleStelle/jasmr-dl/internal/naming"
	"github.com/EagleStelle/jasmr-dl/internal/session"
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
	noJacket    bool
	noImages    bool
	noChapters  bool
	noSplit     bool
	noTags      bool
	verbose     bool
}

var opts options

var rootCmd = &cobra.Command{
	Use:     "jasmr-dl <url>...",
	Version: resolveVersion(),
	Short:   "Download audio from japaneseasmr.com posts, tagged and with jacket art",
	Long: "Download audio from japaneseasmr.com posts, tagged and with jacket art.\n\n" +
		"Several URLs may be given at once, and -a reads a list of them from a\n" +
		"file. -N and -j hold across the whole run, however many posts it\n" +
		"covers.\n\n" +
		"Cookies and the User-Agent they were earned under are kept in\n" +
		session.DefaultDir() + ", which " + session.DirEnv + " moves.\n\n" +
		"Output template (-o):\n" +
		"  directories  {title} {rjcode} {circle} {artist} {date} {year} {month} {day}\n" +
		"  filename     all of the above, plus {number} {chapter} {track} {tracktotal} {ext}\n\n" +
		"  <A|B> names a template per shape: A per track, B per chapter, and * on\n" +
		"  either side keeps that side's default. Quote the whole template, since\n" +
		"  < > | * are shell syntax.\n\n" +
		"  default  " + naming.Default,
	Example: "  jasmr-dl https://japaneseasmr.com/12345/\n" +
		"  jasmr-dl https://japaneseasmr.com/12345/ https://japaneseasmr.com/12346/\n" +
		"  jasmr-dl -a urls.txt\n" +
		"  jasmr-dl https://japaneseasmr.com/12345/ -j 64 -N 8\n" +
		"  jasmr-dl https://japaneseasmr.com/12345/ -o \"{circle}/{rjcode}/{number}. {chapter}.{ext}\"\n" +
		"  jasmr-dl https://japaneseasmr.com/12345/ -o \"C:/Audio/{year}/{title}/{rjcode}_{number}.{ext}\"\n" +
		"  jasmr-dl https://japaneseasmr.com/12345/ -o \"<*|{number}_{chapter} [{circle}].{ext}>\"\n" +
		"  jasmr-dl https://japaneseasmr.com/12345/ -P ./out -o \"{rjcode}/{number}.{ext}\"\n" +
		"  jasmr-dl https://japaneseasmr.com/12345/ -J -I -T\n" +
		"  jasmr-dl https://japaneseasmr.com/12345/ -c C:\\path\\cookies.txt",
	Args: cobra.ArbitraryArgs,
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
	f := rootCmd.PersistentFlags()
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
	f.BoolVarP(&opts.noJacket, "no-jacket", "J", false, "do not embed jacket art")
	f.BoolVarP(&opts.noImages, "no-images", "I", false, "do not save the rest of the post's gallery")
	f.BoolVarP(&opts.noChapters, "no-chapters", "H", false, "do not use the track list: no chapters, no split")
	f.BoolVarP(&opts.noSplit, "no-split", "S", false, "do not cut a chaptered stream into one file per chapter")
	f.BoolVarP(&opts.noTags, "no-tags", "T", false, "do not write title, artist or album metadata")
	f.BoolVarP(&opts.verbose, "verbose", "v", false, "debug logging on stderr")

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
