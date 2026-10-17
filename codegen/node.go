package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// Kind is the JSON kind of a Node.
type Kind uint8

// JSON kinds.
const (
	Null Kind = iota
	Bool
	Number
	String
	Array
	Object
)

// Node is a JSON value that keeps the key order of objects, so the generated
// code follows the order of the spec.
type Node struct {
	Kind Kind
	Bool bool
	// Str holds a string, or the text of a number.
	Str  string
	Arr  []*Node
	Keys []string
	vals map[string]*Node
}

// ParseNode parses JSON into a Node.
func ParseNode(data []byte) (*Node, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	n, err := parseValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing data after JSON value")
	}
	return n, nil
}

func parseValue(dec *json.Decoder) (*Node, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case nil:
		return &Node{Kind: Null}, nil
	case bool:
		return &Node{Kind: Bool, Bool: t}, nil
	case json.Number:
		return &Node{Kind: Number, Str: t.String()}, nil
	case string:
		return &Node{Kind: String, Str: t}, nil
	case json.Delim:
		switch t {
		case '[':
			n := &Node{Kind: Array}
			for dec.More() {
				v, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				n.Arr = append(n.Arr, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return n, nil
		case '{':
			n := &Node{Kind: Object, vals: map[string]*Node{}}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key := kt.(string)
				v, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				if _, dup := n.vals[key]; !dup {
					n.Keys = append(n.Keys, key)
				}
				n.vals[key] = v
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return n, nil
		}
	}
	return nil, fmt.Errorf("unexpected token %v", tok)
}

// Get returns the value of key, or nil. It is nil-safe.
func (n *Node) Get(key string) *Node {
	if n == nil || n.Kind != Object {
		return nil
	}
	return n.vals[key]
}

// Path follows keys.
func (n *Node) Path(keys ...string) *Node {
	for _, k := range keys {
		n = n.Get(k)
	}
	return n
}

// Has reports whether the object has key.
func (n *Node) Has(key string) bool { return n.Get(key) != nil }

// Text returns the string value, or "".
func (n *Node) Text() string {
	if n == nil || n.Kind != String {
		return ""
	}
	return n.Str
}

// Truthy reports whether n is JSON true.
func (n *Node) Truthy() bool { return n != nil && n.Kind == Bool && n.Bool }

// Strings returns the string items of an array.
func (n *Node) Strings() []string {
	if n == nil {
		return nil
	}
	var out []string
	for _, v := range n.Arr {
		if v.Kind == String {
			out = append(out, v.Str)
		}
	}
	return out
}

// Set sets key (used to build synthetic examples).
func (n *Node) Set(key string, v *Node) {
	if n.vals == nil {
		n.vals = map[string]*Node{}
	}
	if _, ok := n.vals[key]; !ok {
		n.Keys = append(n.Keys, key)
	}
	n.vals[key] = v
}

// JSON encodes n compactly, keeping key order.
func (n *Node) JSON() string {
	var b strings.Builder
	n.write(&b, false)
	return b.String()
}

// SortedJSON encodes n compactly with sorted keys.
func (n *Node) SortedJSON() string {
	var b strings.Builder
	n.write(&b, true)
	return b.String()
}

func (n *Node) write(b *strings.Builder, sorted bool) {
	if n == nil {
		b.WriteString("null")
		return
	}
	switch n.Kind {
	case Null:
		b.WriteString("null")
	case Bool:
		b.WriteString(strconv.FormatBool(n.Bool))
	case Number:
		b.WriteString(n.Str)
	case String:
		raw, _ := json.Marshal(n.Str)
		b.Write(raw)
	case Array:
		b.WriteByte('[')
		for i, v := range n.Arr {
			if i > 0 {
				b.WriteByte(',')
			}
			v.write(b, sorted)
		}
		b.WriteByte(']')
	case Object:
		keys := n.Keys
		if sorted {
			keys = append([]string(nil), keys...)
			sort.Strings(keys)
		}
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			raw, _ := json.Marshal(k)
			b.Write(raw)
			b.WriteByte(':')
			n.vals[k].write(b, sorted)
		}
		b.WriteByte('}')
	}
}
