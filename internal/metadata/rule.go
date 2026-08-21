package metadata

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/EagleStelle/jasmr-dl/internal/tmpl"
)

// fieldLike is a brace holding something spelled like a field name. A brace
// holding anything else, {2,3} among them, belongs to the expression.
var fieldLike = regexp.MustCompile(`^[a-zA-Z_]+$`)

// Rule is one --parse-metadata FROM:TO. FROM is a template of the fields to
// read; TO is the expression that text has to match, each {field} in it
// capturing what that field becomes.
type Rule struct {
	raw  string
	from []tmpl.Node
	to   *regexp.Regexp
}

func (r Rule) String() string { return r.raw }

// ParseRules reads every rule a run was given, in the order they are applied.
func ParseRules(raws []string) ([]Rule, error) {
	if len(raws) == 0 {
		return nil, nil
	}
	rules := make([]Rule, 0, len(raws))
	for _, raw := range raws {
		rule, err := parseRule(raw)
		if err != nil {
			return nil, fmt.Errorf("parse-metadata %q: %w", raw, err)
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// parseRule reads one rule.
func parseRule(raw string) (Rule, error) {
	from, to, ok := cutRule(raw)
	if !ok {
		return Rule{}, fmt.Errorf(`a rule reads FROM:TO, as in "{title}:{artist} - {title}"; ` +
			`a colon of its own is written \:`)
	}

	rule := Rule{raw: raw}
	var err error
	if rule.from, err = parseFrom(from); err != nil {
		return Rule{}, err
	}
	if rule.to, err = compileTo(to); err != nil {
		return Rule{}, err
	}
	return rule, nil
}

// Apply rewrites f from one rule, reporting whether its pattern matched. A rule
// that matched nothing gives f back as it was: it was written for another post.
func (r Rule) Apply(f Fields) (Fields, bool) {
	// Nothing is sanitized here: this is metadata, not a path, so a separator
	// inside a value is the value's own.
	src := tmpl.Expand(r.from, func(field string) string {
		v, _ := f.Read(field)
		return v
	})

	m := r.to.FindStringSubmatchIndex(src)
	if m == nil {
		return f, false
	}
	for i, name := range r.to.SubexpNames() {
		// A group that took no part in the match says nothing about its field.
		// One that matched an empty string does, and empties it.
		start, end := m[2*i], m[2*i+1]
		if name == "" || start < 0 {
			continue
		}
		writeField[name](&f, src[start:end])
	}
	return f, true
}

// cutRule splits a rule at the colon between its halves. A colon belonging to
// either half is written \: and kept.
func cutRule(raw string) (from, to string, ok bool) {
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case '\\':
			i++
		case ':':
			return raw[:i], raw[i+1:], true
		}
	}
	return "", "", false
}

// parseFrom reads the half naming what to match against. A bare field name is
// that field; anything longer is a template.
func parseFrom(raw string) ([]tmpl.Node, error) {
	if name := tmpl.Field(raw); Readable(name) {
		return []tmpl.Node{{Field: name}}, nil
	}

	nodes, err := tmpl.Parse(raw, resolve(readField))
	if err != nil {
		return nil, err
	}
	if !tmpl.Named(nodes) {
		return nil, fmt.Errorf("%q reads no field; name the one to match against, as in title or {title}", raw)
	}
	return nodes, nil
}

// compileTo turns the half naming what to keep into the expression the matched
// text has to satisfy. It reads one of three ways, which is how yt-dlp reads
// its own:
//
//   - a bare field name keeps the whole of what was matched
//   - a half naming fields is a template, each {field} capturing what that
//     field becomes and everything around them standing for itself
//   - a half naming none is a regular expression of its own, its (?P<field>…)
//     groups saying what to keep
//
// The two cannot be mixed: a template's literal text is literal, so a rule
// reaching for a regular expression writes the whole half as one.
func compileTo(raw string) (*regexp.Regexp, error) {
	if name := tmpl.Field(raw); writeField[name] != nil {
		return compile(raw, "(?s)(?P<"+name+">.+)")
	}

	nodes, err := tmpl.Parse(raw, resolve(writeField))
	if err != nil {
		return nil, err
	}
	if !tmpl.Named(nodes) {
		return compile(raw, raw)
	}

	var b strings.Builder
	for _, n := range nodes {
		if n.Field == "" {
			b.WriteString(regexp.QuoteMeta(n.Lit))
			continue
		}
		b.WriteString("(?P<" + n.Field + ">.+)")
	}
	return compile(raw, b.String())
}

// resolve is what a brace means on one side of a rule: a field, a mistake
// where it is spelled like one and names none, or the expression's own text.
func resolve[T any](known map[string]T) tmpl.Resolve {
	return func(name string) (string, error) {
		if _, ok := known[name]; ok {
			return name, nil
		}
		if fieldLike.MatchString(name) {
			return "", unknownField(name, known)
		}
		return "", nil
	}
}

func unknownField[T any](name string, known map[string]T) error {
	return fmt.Errorf("unknown field {%s}; known fields are %s",
		name, tmpl.Known(slices.Collect(maps.Keys(known))))
}

// compile builds the expression, reporting it against the half it was written
// as rather than what that expanded to.
func compile(raw, pattern string) (*regexp.Regexp, error) {
	to, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("%q is not a pattern: %w", raw, err)
	}
	// A group named the long way round, (?P<title>…), sets its field too, so it
	// has to name one.
	for _, name := range to.SubexpNames() {
		if name != "" && writeField[name] == nil {
			return nil, unknownField(name, writeField)
		}
	}
	return to, nil
}
