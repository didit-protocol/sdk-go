package main

import (
	"strconv"
	"strings"
	"time"
)

// exampleBuilder turns spec examples into values that fit the schema, and
// synthesises values where the spec has none, for the generated tests.
type exampleBuilder struct {
	doc *Doc
	// side decides how required fields and nulls are treated.
	side Side
}

const maxExampleDepth = 6

// timeLayouts mirror internal/apijson, so a date-time the SDK decodes is
// accepted here.
var timeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02",
}

func parseTime(s string) (time.Time, bool) {
	for _, l := range timeLayouts {
		if t, err := time.Parse(l, strings.TrimSpace(s)); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// props returns the merged properties and required set of an object-like
// schema (following $ref, allOf, oneOf and anyOf).
func (b *exampleBuilder) props(s *Node) (keys []string, schemas map[string]*Node, required map[string]bool) {
	schemas = map[string]*Node{}
	required = map[string]bool{}
	var visit func(s *Node, depth int, union bool)
	visit = func(s *Node, depth int, union bool) {
		s = view(b.doc, s)
		if s == nil || depth > 8 {
			return
		}
		if !union {
			for _, r := range s.Get("required").Strings() {
				required[r] = true
			}
		}
		for _, p := range s.Get("allOf").Elems() {
			visit(p, depth+1, union)
		}
		for _, key := range []string{"oneOf", "anyOf"} {
			for _, p := range s.Get(key).Elems() {
				visit(p, depth+1, true)
			}
		}
		ps := s.Get("properties")
		for _, k := range ps.ObjKeys() {
			if _, ok := schemas[k]; !ok {
				keys = append(keys, k)
				schemas[k] = ps.Get(k)
			}
		}
	}
	visit(s, 0, false)
	return keys, schemas, required
}

// fit returns a copy of ex that fits schema s, dropping what does not fit,
// or nil when ex itself does not fit.
func (b *exampleBuilder) fit(ex, s *Node, depth int) *Node {
	if ex == nil {
		return nil
	}
	if depth > maxExampleDepth {
		return nil
	}
	v := view(b.doc, s)
	if v == nil {
		return ex
	}
	if ex.Kind == Null {
		if v.Get("nullable").Truthy() || s.Get("nullable").Truthy() {
			return ex
		}
		return nil
	}
	if isObjectLike(b.doc, v) {
		if ex.Kind != Object {
			return nil
		}
		keys, schemas, required := b.props(v)
		out := &Node{Kind: Object}
		for _, k := range keys {
			pv := ex.Get(k)
			ps := schemas[k]
			pview := view(b.doc, ps)
			if b.side == Request && pview != nil && pview.Get("readOnly").Truthy() {
				continue
			}
			if b.side == Request && pview != nil && pview.Get("format").Text() == "binary" {
				continue
			}
			if pv != nil {
				if f := b.fit(pv, ps, depth+1); f != nil {
					out.Set(k, f)
					continue
				}
			}
			if b.side == Request && required[k] {
				// The SDK always sends required fields: give them a value.
				if syn := b.synth(ps, depth+1, map[string]bool{}); syn != nil {
					out.Set(k, syn)
				}
			}
		}
		return out
	}
	switch schemaType(b.doc, v) {
	case "object":
		if ex.Kind != Object {
			return nil
		}
		ap := v.Get("additionalProperties")
		if ap != nil && ap.Kind == Object && len(ap.Keys) > 0 {
			out := &Node{Kind: Object}
			for _, k := range ex.Keys {
				f := b.fit(ex.Get(k), ap, depth+1)
				if f == nil {
					return nil
				}
				out.Set(k, f)
			}
			return out
		}
		return ex
	case "array":
		if ex.Kind != Array {
			return nil
		}
		out := &Node{Kind: Array}
		for _, item := range ex.Arr {
			f := b.fit(item, v.Get("items"), depth+1)
			if f == nil {
				return nil
			}
			out.Arr = append(out.Arr, f)
		}
		return out
	case "string":
		if ex.Kind != String {
			return nil
		}
		if v.Get("format").Text() == "date-time" {
			t, ok := parseTime(ex.Str)
			if !ok {
				return nil
			}
			if b.side == Request {
				// What the SDK will send for this time.
				return &Node{Kind: String, Str: t.Format(time.RFC3339Nano)}
			}
		}
		return ex
	case "integer":
		if ex.Kind != Number {
			return nil
		}
		if _, err := strconv.ParseInt(ex.Str, 10, 64); err != nil {
			return nil
		}
		return ex
	case "number":
		if ex.Kind != Number {
			return nil
		}
		return ex
	case "boolean":
		if ex.Kind != Bool {
			return nil
		}
		return ex
	}
	// oneOf/anyOf of non-objects, or untyped: any value fits.
	if parts := v.Get("oneOf"); parts != nil {
		return ex
	}
	if parts := v.Get("anyOf"); parts != nil {
		return ex
	}
	return ex
}

// example returns the first usable example of s: its own example, or nil.
func schemaExample(s *Node) *Node {
	if ex := s.Get("example"); ex != nil {
		return ex
	}
	if exs := s.Get("examples"); exs != nil && exs.Kind == Array && len(exs.Arr) > 0 {
		return exs.Arr[0]
	}
	return nil
}

// synth builds a value that fits s.
func (b *exampleBuilder) synth(s *Node, depth int, seen map[string]bool) *Node {
	if s == nil {
		return &Node{Kind: String, Str: "value"}
	}
	if ref := s.Get("$ref").Text(); ref != "" {
		if seen[ref] || depth > maxExampleDepth {
			return nil
		}
		seen = copySeen(seen)
		seen[ref] = true
	}
	v := view(b.doc, s)
	if v == nil {
		return &Node{Kind: String, Str: "value"}
	}
	if ex := schemaExample(s); ex != nil {
		if f := b.fit(ex, s, depth); f != nil {
			return f
		}
	}
	if ex := schemaExample(v); ex != nil {
		if f := b.fit(ex, v, depth); f != nil {
			return f
		}
	}
	if isObjectLike(b.doc, v) {
		if depth > maxExampleDepth {
			return nil
		}
		keys, schemas, required := b.props(v)
		out := &Node{Kind: Object}
		for _, k := range keys {
			ps := schemas[k]
			pv := view(b.doc, ps)
			if b.side == Request && pv != nil && (pv.Get("readOnly").Truthy() || pv.Get("format").Text() == "binary") {
				continue
			}
			if b.side == Request && !required[k] && depth > 2 {
				continue
			}
			if val := b.synth(ps, depth+1, seen); val != nil {
				out.Set(k, val)
			} else if b.side == Request && required[k] {
				return nil
			}
		}
		return out
	}
	if e := v.Get("enum"); e != nil {
		for _, item := range e.Arr {
			if item.Kind == String {
				return item
			}
		}
	}
	for _, key := range []string{"oneOf", "anyOf"} {
		if parts := v.Get(key); parts != nil && len(parts.Arr) > 0 {
			return b.synth(parts.Arr[0], depth+1, seen)
		}
	}
	switch schemaType(b.doc, v) {
	case "string":
		switch v.Get("format").Text() {
		case "uuid":
			return &Node{Kind: String, Str: "3fa85f64-5717-4562-b3fc-2c963f66afa6"}
		case "date-time":
			return &Node{Kind: String, Str: "2025-01-02T03:04:05Z"}
		case "date":
			return &Node{Kind: String, Str: "2025-01-02"}
		case "email":
			return &Node{Kind: String, Str: "user@example.com"}
		case "uri":
			return &Node{Kind: String, Str: "https://example.com/path"}
		}
		return &Node{Kind: String, Str: "string"}
	case "integer":
		if m := v.Get("minimum"); m != nil && m.Kind == Number {
			if _, err := strconv.ParseInt(m.Str, 10, 64); err == nil {
				return m
			}
		}
		return &Node{Kind: Number, Str: "1"}
	case "number":
		return &Node{Kind: Number, Str: "1.5"}
	case "boolean":
		return &Node{Kind: Bool, Bool: true}
	case "array":
		item := b.synth(v.Get("items"), depth+1, seen)
		out := &Node{Kind: Array}
		if item != nil {
			out.Arr = append(out.Arr, item)
		}
		return out
	case "object":
		out := &Node{Kind: Object}
		if ap := v.Get("additionalProperties"); ap != nil && ap.Kind == Object && len(ap.Keys) > 0 {
			if val := b.synth(ap, depth+1, seen); val != nil {
				out.Set("key", val)
			}
		} else {
			out.Set("key", &Node{Kind: String, Str: "value"})
		}
		return out
	}
	return &Node{Kind: String, Str: "value"}
}

func copySeen(m map[string]bool) map[string]bool {
	out := make(map[string]bool, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}

// value returns the example for schema s: the given example when it fits,
// otherwise a synthesised one.
func (b *exampleBuilder) value(ex, s *Node) *Node {
	if ex != nil {
		if f := b.fit(ex, s, 0); f != nil {
			return f
		}
	}
	return b.synth(s, 0, map[string]bool{})
}
