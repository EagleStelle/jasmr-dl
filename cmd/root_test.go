package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EagleStelle/jasmr-dl/internal/naming"
)

func readme(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestEveryExampleIsInTheREADME(t *testing.T) {
	doc := readme(t)
	for _, line := range strings.Split(rootCmd.Example, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.Contains(doc, line) {
			t.Errorf("--help shows %q, which the README does not", line)
		}
	}
}

func TestTheDefaultTemplateIsInTheREADME(t *testing.T) {
	if !strings.Contains(readme(t), naming.Default) {
		t.Errorf("the README does not name the default %q", naming.Default)
	}
}
