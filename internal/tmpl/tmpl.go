// Package tmpl reads the {field} template the output template and a metadata
// rule are both written in. What the fields mean is the caller's; the shape
// they are named in is here.
package tmpl

import (
	"fmt"
	"sort"
	"strings"
)

// Node is one piece of a template: literal text, or a field to substitute.
type Node struct {
	Lit   string
	Field string
}

// Resolve says what one brace names: the field to substitute, an empty name to
// keep the brace as literal text, or an error naming the mistake.
type Resolve func(name string) (string, error)

// Parse splits raw into its literal text and the fields named in it. A name is
// normalised before resolve sees it, and \x is the character x rather than
// anything of its own.
func Parse(raw string, resolve Resolve) ([]Node, error) {
	var (
		nodes []Node
		lit   strings.Builder
	)
	flush := func() {
		if lit.Len() > 0 {
			nodes = append(nodes, Node{Lit: lit.String()})
			lit.Reset()
		}
	}

	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case '\\':
			if i+1 < len(raw) {
				i++
			}
			lit.WriteByte(raw[i])
		case '{':
			shut := strings.IndexByte(raw[i:], '}')
			if shut < 0 {
				return nil, fmt.Errorf("unclosed { in %q", raw)
			}
			field, err := resolve(Field(raw[i+1 : i+shut]))
			if err != nil {
				return nil, err
			}
			if field == "" {
				lit.WriteString(raw[i : i+shut+1])
			} else {
				flush()
				nodes = append(nodes, Node{Field: field})
			}
			i += shut
		default:
			lit.WriteByte(raw[i])
		}
	}
	flush()
	return nodes, nil
}

// Expand renders nodes, value giving the text of each field.
func Expand(nodes []Node, value func(field string) string) string {
	var b strings.Builder
	for _, n := range nodes {
		if n.Field == "" {
			b.WriteString(n.Lit)
			continue
		}
		b.WriteString(value(n.Field))
	}
	return b.String()
}

// Named reports whether nodes name any field at all.
func Named(nodes []Node) bool {
	for _, n := range nodes {
		if n.Field != "" {
			return true
		}
	}
	return false
}

// Scan lists the fields raw names, for a reader that has to look before the
// template is known to parse. An unclosed brace ends the list.
func Scan(raw string) []string {
	var out []string
	for {
		open := strings.IndexByte(raw, '{')
		if open < 0 {
			return out
		}
		shut := strings.IndexByte(raw[open:], '}')
		if shut < 0 {
			return out
		}
		shut += open
		out = append(out, Field(raw[open+1:shut]))
		raw = raw[shut+1:]
	}
}

// Field normalises a brace's body into the name Parse resolves.
func Field(body string) string {
	return strings.ToLower(strings.TrimSpace(body))
}

// Known spells a field set the way an error message names it.
func Known(names []string) string {
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = "{" + name + "}"
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}
