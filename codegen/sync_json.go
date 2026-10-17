package main

import (
	"fmt"
	"math/big"
	"sort"
	"strings"
	"unicode/utf8"
)

// JSON helpers of the spec sync. The files the sync writes (SOURCE.json,
// overlay.json, deprecated-operations.json) keep the layout of the Python
// reference implementation, json.dumps(value, indent=1, ensure_ascii=False),
// so that the seeded files round-trip byte for byte and the same file looks
// the same in every SDK repository. The summary quotes values the way
// Python's json.dumps does by default.

// jsonStyle selects the json.dumps arguments to imitate.
type jsonStyle struct {
	// indent is the number of spaces per level; negative writes one line.
	indent int
	// ascii escapes every non-ASCII character (ensure_ascii=True).
	ascii bool
	// sortKeys writes object keys in code point order (sort_keys=True).
	sortKeys bool
}

var (
	// fileStyle is json.dumps(value, indent=1, ensure_ascii=False).
	fileStyle = jsonStyle{indent: 1}
	// inlineStyle is json.dumps(value).
	inlineStyle = jsonStyle{indent: -1, ascii: true}
	// sortedInlineStyle is json.dumps(value, sort_keys=True).
	sortedInlineStyle = jsonStyle{indent: -1, ascii: true, sortKeys: true}
)

// encodeJSON renders n like Python's json.dumps with the given style.
func encodeJSON(n *Node, st jsonStyle) string {
	var b strings.Builder
	writeJSON(&b, n, st, 0)
	return b.String()
}

func writeJSON(b *strings.Builder, n *Node, st jsonStyle, level int) {
	if n == nil {
		b.WriteString("null")
		return
	}
	newline := func(level int) {
		if st.indent >= 0 {
			b.WriteByte('\n')
			b.WriteString(strings.Repeat(" ", st.indent*level))
		}
	}
	// Python separators: (", ", ": ") on one line, ("," + newline, ": ") indented.
	sep := ", "
	if st.indent >= 0 {
		sep = ","
	}
	switch n.Kind {
	case Null:
		b.WriteString("null")
	case Bool:
		if n.Bool {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case Number:
		b.WriteString(n.Str)
	case String:
		writeJSONString(b, n.Str, st.ascii)
	case Array:
		if len(n.Arr) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteByte('[')
		for i, v := range n.Arr {
			if i > 0 {
				b.WriteString(sep)
			}
			newline(level + 1)
			writeJSON(b, v, st, level+1)
		}
		newline(level)
		b.WriteByte(']')
	case Object:
		if len(n.Keys) == 0 {
			b.WriteString("{}")
			return
		}
		keys := n.Keys
		if st.sortKeys {
			keys = sortedKeys(n)
		}
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteString(sep)
			}
			newline(level + 1)
			writeJSONString(b, k, st.ascii)
			b.WriteString(": ")
			writeJSON(b, n.Get(k), st, level+1)
		}
		newline(level)
		b.WriteByte('}')
	}
}

// writeJSONString quotes s like Python's json module: the short escapes for
// '"', '\\', '\n', '\r', '\t', '\b' and '\f', \u00XX for the other control
// characters and, with ascii, \uXXXX (surrogate pairs above U+FFFF) for
// everything outside printable ASCII. Unlike encoding/json it leaves '<',
// '>', '&', U+2028 and U+2029 alone.
func writeJSONString(b *strings.Builder, s string, ascii bool) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			switch {
			case r < 0x20:
				fmt.Fprintf(b, `\u%04x`, r)
			case ascii && r > 0x7e && r <= 0xffff:
				fmt.Fprintf(b, `\u%04x`, r)
			case ascii && r > 0xffff:
				r -= 0x10000
				fmt.Fprintf(b, `\u%04x\u%04x`, 0xd800+(r>>10), 0xdc00+(r&0x3ff))
			default:
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}

// pyStrings renders a list of strings like Python's repr of a list
// (['a', 'b']), as the reference summary does for content types.
func pyStrings(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = "'" + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "'", `\'`) + "'"
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// nodeEqual reports whether a and b are the same JSON value with Python's
// equality: object key order does not matter, and numbers compare by value
// (1 equals 1.0). nil equals nil only.
func nodeEqual(a, b *Node) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil || a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case Null:
		return true
	case Bool:
		return a.Bool == b.Bool
	case Number:
		return numberEqual(a.Str, b.Str)
	case String:
		return a.Str == b.Str
	case Array:
		if len(a.Arr) != len(b.Arr) {
			return false
		}
		for i := range a.Arr {
			if !nodeEqual(a.Arr[i], b.Arr[i]) {
				return false
			}
		}
		return true
	case Object:
		if len(a.Keys) != len(b.Keys) {
			return false
		}
		for _, k := range a.Keys {
			bv := b.Get(k)
			if bv == nil || !nodeEqual(a.Get(k), bv) {
				return false
			}
		}
		return true
	}
	return false
}

func numberEqual(a, b string) bool {
	if a == b {
		return true
	}
	x, okx := new(big.Rat).SetString(a)
	y, oky := new(big.Rat).SetString(b)
	return okx && oky && x.Cmp(y) == 0
}

// newObject returns an empty JSON object.
func newObject() *Node { return &Node{Kind: Object, vals: map[string]*Node{}} }

// newString returns a JSON string.
func newString(s string) *Node { return &Node{Kind: String, Str: s} }

// newArray returns a JSON array of items.
func newArray(items []*Node) *Node { return &Node{Kind: Array, Arr: items} }

// copyObject returns a shallow copy of an object (the values are shared).
func copyObject(n *Node) *Node {
	out := newObject()
	for _, k := range n.ObjKeys() {
		out.Set(k, n.Get(k))
	}
	return out
}

// deleteKey removes key from an object, keeping the order of the others.
func deleteKey(n *Node, key string) {
	if n == nil || n.Kind != Object || n.vals[key] == nil {
		return
	}
	delete(n.vals, key)
	keys := n.Keys[:0:0]
	for _, k := range n.Keys {
		if k != key {
			keys = append(keys, k)
		}
	}
	n.Keys = keys
}

// sortedKeys returns the keys of an object in code point order.
func sortedKeys(n *Node) []string {
	keys := append([]string(nil), n.ObjKeys()...)
	sort.Strings(keys) // UTF-8 byte order is code point order
	return keys
}

// isObject reports whether n is a JSON object.
func isObject(n *Node) bool { return n != nil && n.Kind == Object }

// orEmpty returns n, or an empty object for nil, like Python's
// dict.get(key, {}).
func orEmpty(n *Node) *Node {
	if n == nil {
		return newObject()
	}
	return n
}

// validUTF8 reports whether data is valid UTF-8 (the downloads must be).
func validUTF8(data []byte) bool { return utf8.Valid(data) }
