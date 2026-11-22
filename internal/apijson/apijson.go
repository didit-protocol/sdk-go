// Package apijson encodes request parameters and decodes responses.
//
// Encoding skips optional fields that were never set (see didit.Opt).
// Decoding is lenient: a field whose JSON value does not fit its Go type is
// left at its zero value and reported as invalid instead of failing the whole
// response, and fields the Go type does not know are kept.
package apijson

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Omittable is implemented by optional values; an omitted value is not sent.
type Omittable interface {
	IsOmitted() bool
}

// FieldState says how a known field appeared in a decoded object.
type FieldState uint8

// Field states.
const (
	// FieldPresent: the field had a value that fits its Go type.
	FieldPresent FieldState = iota + 1
	// FieldNull: the field was JSON null.
	FieldNull
	// FieldInvalid: the field had a value that does not fit its Go type; the
	// Go field holds its zero value.
	FieldInvalid
)

// Metadata describes a decoded JSON object.
type Metadata struct {
	Raw    json.RawMessage
	Fields map[string]FieldState
	Extra  map[string]json.RawMessage
}

type fieldInfo struct {
	index int
	name  string
}

var fieldCache sync.Map // reflect.Type -> []fieldInfo

func fields(t reflect.Type, tag string) []fieldInfo {
	key := struct {
		t   reflect.Type
		tag string
	}{t, tag}
	if cached, ok := fieldCache.Load(key); ok {
		return cached.([]fieldInfo)
	}
	var out []fieldInfo
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get(tag), ",")
		if name == "" || name == "-" {
			continue
		}
		out = append(out, fieldInfo{index: i, name: name})
	}
	fieldCache.Store(key, out)
	return out
}

// Fields returns the exported struct fields of t carrying tag, in order, as
// (field index, tag name) pairs.
func Fields(t reflect.Type, tag string) (indexes []int, names []string) {
	for _, f := range fields(t, tag) {
		indexes = append(indexes, f.index)
		names = append(names, f.name)
	}
	return indexes, names
}

// MarshalObject encodes the json-tagged fields of the struct v, in field
// order, skipping omitted optional values. It is used by the generated
// MarshalJSON methods of parameter types.
func MarshalObject(v any) ([]byte, error) {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return []byte("null"), nil
		}
		rv = rv.Elem()
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	first := true
	for _, f := range fields(rv.Type(), "json") {
		fv := rv.Field(f.index)
		if o, ok := fv.Interface().(Omittable); ok && o.IsOmitted() {
			continue
		}
		value, err := Marshal(fv.Interface())
		if err != nil {
			return nil, err
		}
		if !first {
			buf.WriteByte(',')
		}
		first = false
		name, _ := Marshal(f.name)
		buf.Write(name)
		buf.WriteByte(':')
		buf.Write(value)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// Marshal is json.Marshal without HTML escaping and without the trailing
// newline.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// UnmarshalObject decodes the JSON object data into the struct pointed to by
// dst and describes what it found. A JSON value that is not an object leaves
// dst unchanged and is not an error; invalid JSON is.
func UnmarshalObject(data []byte, dst any) (Metadata, error) {
	meta := Metadata{Raw: append(json.RawMessage(nil), data...)}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		if len(trimmed) > 0 {
			var probe any
			if err := json.Unmarshal(trimmed, &probe); err != nil {
				return meta, err
			}
		}
		return meta, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return meta, err
	}
	rv := reflect.ValueOf(dst).Elem()
	meta.Fields = make(map[string]FieldState)
	for _, f := range fields(rv.Type(), "json") {
		raw, ok := obj[f.name]
		if !ok {
			continue
		}
		delete(obj, f.name)
		if isNull(raw) {
			meta.Fields[f.name] = FieldNull
			continue
		}
		if decodeValue(raw, rv.Field(f.index)) {
			meta.Fields[f.name] = FieldPresent
		} else {
			meta.Fields[f.name] = FieldInvalid
		}
	}
	if len(obj) > 0 {
		meta.Extra = obj
	}
	return meta, nil
}

func isNull(raw json.RawMessage) bool {
	return string(bytes.TrimSpace(raw)) == "null"
}

var timeType = reflect.TypeOf(time.Time{})

// decodeValue decodes raw into v, coercing near misses (a number sent as a
// string, a date-time without a time zone, ...). It reports whether the value
// could be decoded; on failure v is left at its zero value.
func decodeValue(raw json.RawMessage, v reflect.Value) bool {
	if err := json.Unmarshal(raw, v.Addr().Interface()); err == nil {
		return true
	}
	v.Set(reflect.Zero(v.Type()))
	switch {
	case v.Type() == timeType:
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return false
		}
		t, ok := parseTime(s)
		if ok {
			v.Set(reflect.ValueOf(t))
		}
		return ok
	case v.Kind() == reflect.Slice:
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			return false
		}
		out := reflect.MakeSlice(v.Type(), len(items), len(items))
		for i, item := range items {
			if !isNull(item) {
				decodeValue(item, out.Index(i))
			}
		}
		v.Set(out)
		return true
	case v.Kind() == reflect.Map && v.Type().Key().Kind() == reflect.String:
		var items map[string]json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			return false
		}
		out := reflect.MakeMapWithSize(v.Type(), len(items))
		for key, item := range items {
			elem := reflect.New(v.Type().Elem()).Elem()
			if !isNull(item) {
				decodeValue(item, elem)
			}
			out.SetMapIndex(reflect.ValueOf(key).Convert(v.Type().Key()), elem)
		}
		v.Set(out)
		return true
	}
	return coerceScalar(raw, v)
}

func coerceScalar(raw json.RawMessage, v reflect.Value) bool {
	text := string(bytes.TrimSpace(raw))
	var s string
	isString := json.Unmarshal(raw, &s) == nil
	switch v.Kind() {
	case reflect.String:
		if text == "true" || text == "false" || (len(text) > 0 && (text[0] == '-' || (text[0] >= '0' && text[0] <= '9'))) {
			v.SetString(text)
			return true
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if isString {
			text = strings.TrimSpace(s)
		}
		if n, err := strconv.ParseInt(text, 10, 64); err == nil && !v.OverflowInt(n) {
			v.SetInt(n)
			return true
		}
		if f, err := strconv.ParseFloat(text, 64); err == nil && f == float64(int64(f)) && !v.OverflowInt(int64(f)) {
			v.SetInt(int64(f))
			return true
		}
	case reflect.Float32, reflect.Float64:
		if isString {
			if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
				v.SetFloat(f)
				return true
			}
		}
	case reflect.Bool:
		if isString {
			if b, err := strconv.ParseBool(strings.TrimSpace(s)); err == nil {
				v.SetBool(b)
				return true
			}
		}
	}
	return false
}

var timeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02",
}

func parseTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
