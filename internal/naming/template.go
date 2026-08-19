// Package naming expands the output template into the path a file is written to.
package naming

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/EagleStelle/jasmr-dl/internal/util"
)

// Unknown stands in for any field the post does not carry, so a template keeps
// the shape it describes rather than collapsing a level.
const Unknown = "Unknown"

// Fields carries every value a template can name. The album fields are known
// before anything is fetched; the file fields settle only once a file is in
// hand, which is why a directory may not name them.
type Fields struct {
	Title  string
	RJCode string
	Circle string
	Artist string
	Date   string
	Year   string
	Month  string
	Day    string

	Chapter string
	Ext     string

	// Number is the file's place in the post, of Total files.
	Number int
	Total  int
}

var albumField = map[string]func(Fields) string{
	"title":  func(f Fields) string { return f.Title },
	"rjcode": func(f Fields) string { return f.RJCode },
	"circle": func(f Fields) string { return f.Circle },
	"artist": func(f Fields) string { return f.Artist },
	"date":   func(f Fields) string { return f.Date },
	"year":   func(f Fields) string { return f.Year },
	"month":  func(f Fields) string { return f.Month },
	"day":    func(f Fields) string { return f.Day },
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
	nodes []node
}

type node struct {
	lit   string
	field string
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
// after a run has already fetched the jacket art helps nobody.
func checkExtension(file segment) error {
	last := file.nodes[len(file.nodes)-1]
	switch {
	case last.field == "ext":
		return nil
	case last.field == "" && util.IsAudioFile(last.lit):
		return nil
	}
	return errors.New("output template must name a file ending in {ext} or an audio extension; " +
		"a directory on its own belongs in -P")
}

func parseSegment(raw string, allowFile bool) (segment, error) {
	var seg segment
	for {
		open := strings.IndexByte(raw, '{')
		if open < 0 {
			break
		}
		shut := strings.IndexByte(raw[open:], '}')
		if shut < 0 {
			return seg, fmt.Errorf("unclosed { in %q", raw)
		}
		shut += open

		name := strings.ToLower(strings.TrimSpace(raw[open+1 : shut]))
		if strings.ContainsRune(name, '{') {
			return seg, fmt.Errorf("unclosed { in %q", raw)
		}
		if err := checkField(name, allowFile); err != nil {
			return seg, err
		}
		if open > 0 {
			seg.nodes = append(seg.nodes, node{lit: raw[:open]})
		}
		seg.nodes = append(seg.nodes, node{field: name})
		raw = raw[shut+1:]
	}
	if raw != "" {
		seg.nodes = append(seg.nodes, node{lit: raw})
	}
	return seg, nil
}

func checkField(name string, allowFile bool) error {
	if _, ok := albumField[name]; ok {
		return nil
	}
	if _, ok := fileField[name]; !ok {
		return fmt.Errorf("unknown field {%s}; known fields are %s", name, knownFields())
	}
	if !allowFile {
		return fmt.Errorf("{%s} names one file, so it cannot appear in a directory", name)
	}
	return nil
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
	var b strings.Builder
	for _, n := range s.nodes {
		if n.field == "" {
			b.WriteString(n.lit)
			continue
		}
		v := value(n.field, f)
		if v == "" {
			v = Unknown
		}
		b.WriteString(util.Sanitize(v))
	}
	return strings.TrimSpace(b.String())
}

func value(name string, f Fields) string {
	if fn, ok := albumField[name]; ok {
		return fn(f)
	}
	return fileField[name](f)
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

func knownFields() string {
	names := make([]string, 0, len(albumField)+len(fileField))
	for name := range albumField {
		names = append(names, name)
	}
	for name := range fileField {
		names = append(names, name)
	}
	sort.Strings(names)
	for i, name := range names {
		names[i] = "{" + name + "}"
	}
	return strings.Join(names, " ")
}
