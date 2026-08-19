package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/EagleStelle/jasmr-dl/internal/session"
)

const (
	// configName is the file read from the state directory, localName the one
	// read from the working directory.
	configName = "config"
	localName  = "jasmr-dl.conf"

	// envPrefix precedes a flag name turned into an environment variable.
	envPrefix = "JASMR_DL_"

	// noConfigEnv skips the files, leaving flags and the environment.
	noConfigEnv = envPrefix + "NO_CONFIG"
)

// applyConfig fills in every flag the command line did not set, from the
// environment first and the config files behind it.
func applyConfig(cmd *cobra.Command) error {
	f := cmd.Flags()

	typed := make(map[string]bool)
	f.VisitAll(func(fl *pflag.Flag) {
		if fl.Changed {
			typed[fl.Name] = true
		}
	})

	values, err := configValues(f)
	if err != nil {
		return err
	}

	for _, name := range slices.Sorted(maps.Keys(values)) {
		if typed[name] {
			continue
		}
		if err := f.Set(name, values[name]); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

// configValues merges the files, then the environment over them.
func configValues(f *pflag.FlagSet) (map[string]string, error) {
	values := make(map[string]string)
	for _, path := range configPaths() {
		if err := readConfigFile(path, f, values); err != nil {
			return nil, err
		}
	}

	f.VisitAll(func(fl *pflag.Flag) {
		if v, ok := os.LookupEnv(envName(fl.Name)); ok && v != "" {
			values[fl.Name] = v
		}
	})
	return values, nil
}

// configPaths are read in order, the working directory last so it wins.
func configPaths() []string {
	if os.Getenv(noConfigEnv) != "" {
		return nil
	}
	return []string{
		filepath.Join(session.DefaultDir(), configName),
		localName,
	}
}

func envName(flag string) string {
	return envPrefix + strings.ToUpper(strings.ReplaceAll(flag, "-", "_"))
}

// readConfigFile reads one file into values. A missing file is not an error.
func readConfigFile(path string, f *pflag.FlagSet, values map[string]string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	n := 0
	for line := range strings.Lines(string(data)) {
		n++
		name, value, err := parseConfigLine(line, f)
		if err != nil {
			return fmt.Errorf("%s:%d: %w", path, n, err)
		}
		if name != "" {
			values[name] = value
		}
	}
	return nil
}

// parseConfigLine reads one setting. An empty name means the line held none.
func parseConfigLine(line string, f *pflag.FlagSet) (name, value string, err error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", nil
	}

	left, right, hasValue := strings.Cut(line, "=")
	name = strings.TrimLeft(strings.TrimSpace(left), "-")

	fl := f.Lookup(name)
	if fl == nil {
		return "", "", fmt.Errorf("unknown setting %q", name)
	}

	if !hasValue {
		if fl.Value.Type() != "bool" {
			return "", "", fmt.Errorf("%s needs a value", name)
		}
		return name, "true", nil
	}
	return name, unquote(strings.TrimSpace(right)), nil
}

// unquote strips one matching pair of quotes, which is how a value keeps its
// surrounding spaces or a leading #.
func unquote(s string) string {
	if len(s) < 2 {
		return s
	}
	q := s[0]
	if (q == '"' || q == '\'') && s[len(s)-1] == q {
		return s[1 : len(s)-1]
	}
	return s
}
