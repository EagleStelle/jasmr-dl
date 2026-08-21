// Package naming expands the output template into the path a file is written to.
package naming

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/EagleStelle/jasmr-dl/internal/metadata"
	"github.com/EagleStelle/jasmr-dl/internal/tmpl"
	"github.com/EagleStelle/jasmr-dl/internal/util"
)

// Unknown stands in for any field the post does not carry, so a template keeps
// the shape it describes rather than collapsing a level.
const Unknown = "Unknown"

// Fields carries every value a template can name: the post's metadata, which is
// the same vocabulary --parse-metadata is written in and is known before
// anything is fetched, and the file fields, which settle only once a file is in
// hand and so may not name a directory.
type Fields struct {
	metadata.Fields

	Chapter string
	Ext     string

	// Number is the file's place in the post, of Total files.
	Number int
	Total  int
}

var fileField = map[string]func(Fields) string{
	"chapter":    func(f Fields) string { return f.Chapter },
	"ext":        func(f Fields) string { return f.Ext },
	"number":     func(f Fields) string { return number(f.Number, Width(f.Total)) },
	"track":      func(f Fields) string { return number(f.Number, 0) },
	"tracktotal": func(f Fields) string { return number(f.Total, 0) },
}

// Template is a parsed output template: directory segments and a filename.
type Template struct {
	dirs []segment
	file segment
}

type segment struct {
	nodes []tmpl.Node
}

// Parse reads a template. The last segment names the file, any before it name
// directories. A rooted template writes outside the working directory.
func Parse(raw string) (*Template, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("output template is empty")
	}

	parts := strings.Split(strings.ReplaceAll(raw, `\`, "/"), "/")
	// A trailing separator names no file, so the segment before it does.
	for len(parts) > 1 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}

	t := &Template{}
	for i, part := range parts {
		last := i == len(parts)-1
		seg, err := parseSegment(part, last)
		if err != nil {
			return nil, err
		}
		if last {
			t.file = seg
			continue
		}
		t.dirs = append(t.dirs, seg)
	}
	if len(t.file.nodes) == 0 {
		return nil, errors.New("output template names no file")
	}
	return t, checkExtension(t.file)
}

// checkExtension refuses a template that cannot end in an extension this tool
// will write. Nothing downstream would accept the file, and finding that out
// after a run has already fetched the cover art helps nobody.
func checkExtension(file segment) error {
	last := file.nodes[len(file.nodes)-1]
	switch {
	case last.Field == "ext":
		return nil
	case last.Field == "" && util.IsAudioFile(last.Lit):
		return nil
	}
	return errors.New("output template must name a file ending in {ext} or an audio extension; " +
		"a directory on its own belongs in -P")
}

// parseSegment reads one path segment. Every brace has to name a field: a
// filename holding one of anything else is more likely a mistake than a wish.
func parseSegment(raw string, allowFile bool) (segment, error) {
	nodes, err := tmpl.Parse(raw, func(name string) (string, error) {
		if metadata.Readable(name) {
			return name, nil
		}
		if _, ok := fileField[name]; !ok {
			return "", fmt.Errorf("unknown field {%s}; known fields are %s", name, knownFields())
		}
		if !allowFile {
			return "", fmt.Errorf("{%s} names one file, so it cannot appear in a directory", name)
		}
		return name, nil
	})
	return segment{nodes}, err
}

func knownFields() string {
	return tmpl.Known(append(metadata.ReadNames(), slices.Collect(maps.Keys(fileField))...))
}

// Dir is the directory the template puts a file in. A segment whose fields are
// all empty is dropped rather than written as a blank level.
func (t *Template) Dir(f Fields) string {
	return join(t.expandDirs(f))
}

// Path is the full path to one file.
func (t *Template) Path(f Fields) string {
	return join(append(t.expandDirs(f), t.File(f)))
}

// File is the name the template gives one file.
func (t *Template) File(f Fields) string {
	if t == nil {
		return ""
	}
	return t.file.expand(f)
}

func (t *Template) expandDirs(f Fields) []string {
	if t == nil {
		return nil
	}
	segs := make([]string, 0, len(t.dirs))
	for _, seg := range t.dirs {
		segs = append(segs, seg.expand(f))
	}
	return segs
}

// expand renders a segment. Only substituted values are sanitized: they carry
// page content, so a separator inside one must never open a directory. Literal
// text is the caller's own, and keeps whatever it says.
func (s segment) expand(f Fields) string {
	return strings.TrimSpace(tmpl.Expand(s.nodes, func(name string) string {
		v, ok := f.Read(name)
		if !ok {
			v = fileField[name](f)
		}
		if v == "" {
			v = Unknown
		}
		return util.Sanitize(v)
	}))
}

// join keeps a rooted template rooted, which filepath.Join alone would not do
// for a bare Windows volume.
func join(segs []string) string {
	if len(segs) == 0 {
		return "."
	}
	return filepath.Clean(filepath.FromSlash(strings.Join(segs, "/")))
}

// number renders a counter padded to width. Zero is absent, not "0".
func number(n, width int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("%0*d", width, n)
}

// Width is the digit count of the largest index a post will number to.
func Width(n int) int {
	return len(fmt.Sprint(max(n, 1)))
}
