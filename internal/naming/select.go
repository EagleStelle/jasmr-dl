package naming

import (
	"fmt"
	"strings"
)

// Default puts every post under its year and RJ code, with a leaf per shape.
const Default = "{year}/{rjcode}/<{rjcode}_{number}.{ext}|{number}_{chapter}.{ext}>"

// One -o value may name a template for each shape a post takes: <A|B> uses A
// per track and B per chapter, a branch of * keeping that side's default.
// util.Sanitize already replaces all four characters in every substituted
// value, so nothing needs escaping.
const (
	groupOpen  = '<'
	groupClose = '>'
	branchSep  = '|'
	useDefault = "*"
)

// Select resolves the <…> groups in raw, keeping the branch this run calls for.
// split takes the right branch. A branch of "*" stands for Default, which only
// fits where the group is the whole template, since Default names a path of its
// own.
func Select(raw string, split bool) (string, error) {
	return resolve(raw, split, true)
}

func resolve(raw string, split, allowDefault bool) (string, error) {
	raw = strings.TrimSpace(raw)
	whole := len(raw) > 1 && raw[0] == groupOpen &&
		strings.IndexByte(raw, groupClose) == len(raw)-1

	var b strings.Builder
	for rest := raw; ; {
		open := strings.IndexByte(rest, groupOpen)
		if open < 0 {
			if err := checkStray(rest); err != nil {
				return "", err
			}
			b.WriteString(rest)
			return b.String(), nil
		}
		if err := checkStray(rest[:open]); err != nil {
			return "", err
		}
		b.WriteString(rest[:open])

		shut := strings.IndexByte(rest[open:], groupClose)
		if shut < 0 {
			return "", fmt.Errorf("unclosed %c in %q", groupOpen, raw)
		}
		shut += open

		branch, err := pick(rest[open+1:shut], split, whole, allowDefault)
		if err != nil {
			return "", err
		}
		b.WriteString(branch)
		rest = rest[shut+1:]
	}
}

func pick(body string, split, whole, allowDefault bool) (string, error) {
	if strings.ContainsRune(body, groupOpen) {
		return "", fmt.Errorf("%c…%c groups cannot nest, in %q", groupOpen, groupClose, body)
	}

	parts := strings.Split(body, string(branchSep))
	if len(parts) != 2 {
		return "", fmt.Errorf("a %c…%c group takes two branches separated by %c, got %d in %q",
			groupOpen, groupClose, branchSep, len(parts), body)
	}
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
		if parts[i] == "" {
			return "", fmt.Errorf("branch %d of %q names nothing; write %q to keep the default",
				i+1, body, useDefault)
		}
	}
	if parts[0] == useDefault && parts[1] == useDefault {
		return "", fmt.Errorf("both branches of %q are %q, leaving the group nothing to divide",
			body, useDefault)
	}

	// Both branches are checked, not just the one taken.
	for _, p := range parts {
		if p == useDefault {
			if !allowDefault {
				return "", fmt.Errorf("%q stands for the default template, so it cannot appear inside it",
					useDefault)
			}
			if !whole {
				return "", fmt.Errorf("%q stands for the whole default template, so it cannot sit inside a longer one",
					useDefault)
			}
			continue
		}
		if err := checkStray(p); err != nil {
			return "", err
		}
	}

	chosen := parts[0]
	if split {
		chosen = parts[1]
	}
	if chosen == useDefault {
		return resolve(Default, split, false)
	}
	return chosen, nil
}

// checkStray refuses a reserved character where it means nothing.
func checkStray(s string) error {
	if strings.ContainsRune(s, groupClose) {
		return fmt.Errorf("%c in %q closes no %c", groupClose, s, groupOpen)
	}
	if strings.ContainsRune(s, branchSep) {
		return fmt.Errorf("%c only separates the two branches of a %c…%c group, in %q",
			branchSep, groupOpen, groupClose, s)
	}
	if strings.Contains(s, useDefault) {
		return fmt.Errorf("%q stands for a whole default template, so it cannot sit inside one, in %q",
			useDefault, s)
	}
	return nil
}

// WithNumber gives the filename a counter unless it already places {number}
// itself. prefix leads with it rather than trailing it.
func WithNumber(raw string, prefix bool) string {
	start, end := filenameBounds(raw)
	file := raw[start:end]
	if namesNumber(file) {
		return raw
	}

	at := len(file)
	if !prefix {
		at = extensionAt(file)
	}
	switch {
	case prefix, at == 0:
		file = "{number}_" + file
	default:
		file = file[:at] + "_{number}" + file[at:]
	}
	return raw[:start] + file + raw[end:]
}

// WithoutNumber drops the counter, and the separator beside it, from a filename
// that has one file to name. A filename of nothing else keeps it.
func WithoutNumber(raw string) string {
	start, end := filenameBounds(raw)
	file := raw[start:end]

	at, width := numberBounds(file)
	if at < 0 {
		return raw
	}

	left, right := file[:at], file[at+width:]
	kept := strings.TrimRight(left, glue)
	switch dropped := left[len(kept):]; {
	case dropped == "":
		right = strings.TrimLeft(right, glue)
	case strings.ContainsAny(dropped, "(["):
		right = strings.TrimLeft(right, ")]")
	}

	file = kept + right
	if strings.Trim(file[:extensionAt(file)], glue) == "" {
		return raw
	}
	return raw[:start] + file + raw[end:]
}

// glue is the punctuation a counter is set off with.
const glue = " _-.,#()[]"

// numberBounds locates {number} in a filename, or -1 where it names none.
func numberBounds(file string) (at, width int) {
	for i := 0; ; {
		open := strings.IndexByte(file[i:], '{')
		if open < 0 {
			return -1, 0
		}
		open += i
		shut := strings.IndexByte(file[open:], '}')
		if shut < 0 {
			return -1, 0
		}
		shut += open
		if strings.ToLower(strings.TrimSpace(file[open+1:shut])) == "number" {
			return open, shut + 1 - open
		}
		i = shut + 1
	}
}

// filenameBounds locates the segment naming the file. A trailing separator
// names no segment, so the one before it is the file.
func filenameBounds(raw string) (start, end int) {
	end = len(raw)
	for end > 0 && isSep(raw[end-1]) {
		end--
	}
	for i := end - 1; i >= 0; i-- {
		if isSep(raw[i]) {
			return i + 1, end
		}
	}
	return 0, end
}

func isSep(c byte) bool { return c == '/' || c == '\\' }

func namesNumber(file string) bool {
	for _, name := range fieldNames(file) {
		if name == "number" {
			return true
		}
	}
	return false
}

// fieldNames lists the fields a segment names, spelled as checkField spells
// them. An unclosed brace is left for Parse to report.
func fieldNames(s string) []string {
	var names []string
	for {
		open := strings.IndexByte(s, '{')
		if open < 0 {
			return names
		}
		shut := strings.IndexByte(s[open:], '}')
		if shut < 0 {
			return names
		}
		shut += open
		names = append(names, strings.ToLower(strings.TrimSpace(s[open+1:shut])))
		s = s[shut+1:]
	}
}

// extensionAt is where the extension starts. A template ending in {ext} may
// carry no dot of its own, leaving the field itself as the mark.
func extensionAt(file string) int {
	dot, field, depth := -1, -1, 0
	for i := 0; i < len(file); i++ {
		switch file[i] {
		case '{':
			depth++
			field = i
		case '}':
			if depth > 0 {
				depth--
			}
		case '.':
			if depth == 0 {
				dot = i
			}
		}
	}
	if dot >= 0 {
		return dot
	}
	if names := fieldNames(file); len(names) > 0 && names[len(names)-1] == "ext" && field >= 0 {
		return field
	}
	return len(file)
}
