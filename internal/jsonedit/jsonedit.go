// Package jsonedit edits a JSON document as text so every byte outside
// the edited container stays exactly as the user wrote it: key order,
// indentation, escaping and unrelated values are untouched.
//
// Why not unmarshal + marshal: a round trip through map[string]any
// reorders keys, re-indents the whole file and escapes <, > and & in
// shell commands (`&&` becomes `&&`). For a file other tools
// and the user edit by hand (Claude Code's settings.json), that churn
// is unacceptable. Instead this package locates the byte span of each
// object member / array element and rebuilds only the one container
// being changed, reusing the raw bytes of every item it keeps.
//
// Input must be valid JSON (checked with json.Valid); the scanner
// relies on that and does not re-validate.
package jsonedit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrInvalid is returned when the document is not valid JSON. Callers
// must refuse to edit such a file rather than guess.
var ErrInvalid = errors.New("jsonedit: document is not valid JSON")

// defaultIndentUnit is used when the document gives no indentation to
// copy (empty or single-line containers).
const defaultIndentUnit = "  "

// Item is one member of an object or element of an array, located by
// byte offsets into the document.
type Item struct {
	Start    int    // first byte of the item (the key's quote for members)
	End      int    // one past the last byte of the value
	Key      string // decoded member key; empty for array elements
	ValStart int    // first byte of the value
}

// Raw returns the item's bytes (key + colon + value for members).
func (it Item) Raw(doc []byte) []byte { return doc[it.Start:it.End] }

// Value returns the value bytes only.
func (it Item) Value(doc []byte) []byte { return doc[it.ValStart:it.End] }

// Container is a parsed object or array: its bracket offsets and items.
type Container struct {
	Open   int // offset of '{' or '['
	Close  int // offset of the matching '}' or ']'
	Object bool
	Items  []Item
}

// Find returns the member with the given key, or false.
func (c Container) Find(key string) (Item, bool) {
	for _, it := range c.Items {
		if it.Key == key {
			return it, true
		}
	}
	return Item{}, false
}

// Parse returns the container whose opening bracket is at offset pos
// (after skipping leading whitespace).
func Parse(doc []byte, pos int) (Container, error) {
	if !json.Valid(doc) {
		return Container{}, ErrInvalid
	}
	return parseAt(doc, skipWS(doc, pos))
}

// Root parses the top-level value, which must be an object.
func Root(doc []byte) (Container, error) {
	c, err := Parse(doc, 0)
	if err != nil {
		return Container{}, err
	}
	if !c.Object {
		return Container{}, fmt.Errorf("jsonedit: top-level value is not an object")
	}
	return c, nil
}

func parseAt(doc []byte, pos int) (Container, error) {
	if pos >= len(doc) || (doc[pos] != '{' && doc[pos] != '[') {
		return Container{}, fmt.Errorf("jsonedit: no object or array at offset %d", pos)
	}
	c := Container{Open: pos, Object: doc[pos] == '{'}
	closer := byte(']')
	if c.Object {
		closer = '}'
	}
	i := skipWS(doc, pos+1)
	for i < len(doc) && doc[i] != closer {
		it := Item{Start: i}
		if c.Object {
			keyEnd := scanString(doc, i)
			if err := json.Unmarshal(doc[i:keyEnd], &it.Key); err != nil {
				return Container{}, ErrInvalid
			}
			i = skipWS(doc, keyEnd)
			i = skipWS(doc, i+1) // ':'
		}
		it.ValStart = i
		it.End = scanValue(doc, i)
		c.Items = append(c.Items, it)
		i = skipWS(doc, it.End)
		if i < len(doc) && doc[i] == ',' {
			i = skipWS(doc, i+1)
		}
	}
	c.Close = i
	return c, nil
}

// scanValue returns one past the end of the value starting at i.
func scanValue(doc []byte, i int) int {
	switch doc[i] {
	case '"':
		return scanString(doc, i)
	case '{', '[':
		depth := 0
		for j := i; j < len(doc); j++ {
			switch doc[j] {
			case '"':
				j = scanString(doc, j) - 1
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return j + 1
				}
			}
		}
		return len(doc)
	default: // number, true, false, null
		j := i
		for j < len(doc) && !strings.ContainsRune(",]} \t\r\n", rune(doc[j])) {
			j++
		}
		return j
	}
}

// scanString returns one past the closing quote of the string at i.
func scanString(doc []byte, i int) int {
	for j := i + 1; j < len(doc); j++ {
		switch doc[j] {
		case '\\':
			j++
		case '"':
			return j + 1
		}
	}
	return len(doc)
}

func skipWS(doc []byte, i int) int {
	for i < len(doc) && (doc[i] == ' ' || doc[i] == '\t' || doc[i] == '\n' || doc[i] == '\r') {
		i++
	}
	return i
}

// lineIndent returns the leading whitespace of the line containing pos.
func lineIndent(doc []byte, pos int) string {
	start := bytes.LastIndexByte(doc[:pos], '\n') + 1
	end := start
	for end < len(doc) && (doc[end] == ' ' || doc[end] == '\t') {
		end++
	}
	return string(doc[start:end])
}

// IndentUnit guesses one level of indentation from the document: the
// leading whitespace of the first indented line. Falls back to two
// spaces.
func IndentUnit(doc []byte) string {
	for _, line := range bytes.Split(doc, []byte("\n"))[1:] {
		n := 0
		for n < len(line) && (line[n] == ' ' || line[n] == '\t') {
			n++
		}
		if n > 0 && n < len(line) {
			return string(line[:n])
		}
	}
	return defaultIndentUnit
}

// Multiline reports whether the document is written across lines
// (pretty-printed) rather than compact on one line. A trailing newline
// alone does not count.
func Multiline(doc []byte) bool {
	return bytes.Contains(bytes.TrimRight(doc, " \t\r\n"), []byte("\n"))
}

// layout captures how a container separates its items so a rebuilt
// container looks like the original.
type layout struct {
	lead  string // whitespace between the opening bracket and the first item
	sep   string // separator between items (comma plus whitespace)
	trail string // whitespace between the last item and the closing bracket
}

func (c Container) layout(doc []byte) layout {
	if len(c.Items) == 0 {
		// Nothing to copy from: follow the document's own style —
		// multi-line if it has any newline, compact otherwise.
		if !Multiline(doc) {
			return layout{sep: ","}
		}
		ind := lineIndent(doc, c.Open)
		unit := IndentUnit(doc)
		return layout{lead: "\n" + ind + unit, sep: ",\n" + ind + unit, trail: "\n" + ind}
	}
	l := layout{
		lead:  string(doc[c.Open+1 : c.Items[0].Start]),
		trail: string(doc[c.Items[len(c.Items)-1].End:c.Close]),
	}
	if len(c.Items) > 1 {
		l.sep = string(doc[c.Items[0].End:c.Items[1].Start])
	} else {
		l.sep = "," + l.lead
	}
	return l
}

// ItemIndent is the indentation a new item of c should be written at.
func (c Container) ItemIndent(doc []byte) string {
	l := c.layout(doc)
	if i := strings.LastIndexByte(l.lead, '\n'); i >= 0 {
		return l.lead[i+1:]
	}
	return ""
}

// Rebuild replaces container c with its kept items (raw bytes, in the
// given order) plus the extra item texts appended at the end. Every
// byte outside c, and every kept item's bytes, are unchanged.
func Rebuild(doc []byte, c Container, keep []Item, extra ...string) []byte {
	l := c.layout(doc)
	parts := make([]string, 0, len(keep)+len(extra))
	for _, it := range keep {
		parts = append(parts, string(it.Raw(doc)))
	}
	parts = append(parts, extra...)
	var b strings.Builder
	b.Write(doc[:c.Open+1])
	if len(parts) > 0 {
		b.WriteString(l.lead)
		b.WriteString(strings.Join(parts, l.sep))
		b.WriteString(l.trail)
	} else if len(c.Items) == 0 {
		b.Write(doc[c.Open+1 : c.Close]) // untouched empty container
	}
	b.Write(doc[c.Close:])
	return []byte(b.String())
}

// Append adds item texts at the end of container c.
func Append(doc []byte, c Container, items ...string) []byte {
	return Rebuild(doc, c, c.Items, items...)
}

// Encode marshals v for insertion at the given indentation, without
// HTML escaping (shell commands keep their literal `&&`, `<`, `>`).
// indent "" with unit "" gives compact output.
func Encode(v any, indent, unit string) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if unit != "" {
		enc.SetIndent(indent, unit)
	}
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

// Member renders `"key": value` with value encoded at indent.
func Member(key string, v any, indent, unit string) (string, error) {
	k, err := Encode(key, "", "")
	if err != nil {
		return "", err
	}
	val, err := Encode(v, indent, unit)
	if err != nil {
		return "", err
	}
	if unit == "" {
		return k + ":" + val, nil
	}
	return k + ": " + val, nil
}
