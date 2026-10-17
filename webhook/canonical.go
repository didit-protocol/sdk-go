package webhook

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	didit "github.com/didit-protocol/sdk-go"
)

// Canonicalize returns the canonical JSON of a JSON document: the bytes
// X-Signature-V2 signs (docs/DESIGN.md section 9.2). It equals Python's
//
//	json.dumps(shorten_floats(value), sort_keys=True, separators=(",", ":"), ensure_ascii=False)
//
// on the parsed document: object keys sorted by code point, no whitespace,
// strings with only the mandatory escapes and everything else as literal
// UTF-8, integers with their exact digits, integral floats as integers (87.0
// becomes 87) and other floats in Python repr form (1e-05, 0.1). When a key
// repeats, the last value wins.
//
// The input must be UTF-8 JSON. Invalid input, NaN or Infinity, and numbers
// that overflow a float64 return a *didit.WebhookVerificationError with reason
// invalid_payload.
func Canonicalize(payload []byte) ([]byte, error) {
	v, err := parseJSON(payload)
	if err != nil {
		return nil, err
	}
	out, err := canonicalJSON(v)
	if err != nil {
		return nil, invalidPayload(err.Error())
	}
	return out, nil
}

// parseJSON decodes a UTF-8 JSON document, keeping numbers as json.Number so
// that no precision is lost.
func parseJSON(payload []byte) (any, error) {
	if !utf8.Valid(payload) {
		return nil, invalidPayload("body is not valid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, invalidPayload("body is empty")
		}
		return nil, invalidPayload("body is not valid JSON: " + err.Error())
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, invalidPayload("body has data after the JSON value")
	}
	return v, nil
}

func invalidPayload(msg string) error {
	return &didit.WebhookVerificationError{Reason: didit.WebhookReasonInvalidPayload, Message: msg}
}

// maxDepth bounds nesting, like encoding/json does, and stops a
// self-referencing map passed to VerifyParsed from recursing forever.
const maxDepth = 10000

// canonicalJSON encodes a decoded JSON value (as produced by encoding/json,
// with or without UseNumber) in canonical form.
func canonicalJSON(v any) ([]byte, error) {
	e := encoder{buf: make([]byte, 0, 512)}
	if err := e.value(v, 0); err != nil {
		return nil, err
	}
	return e.buf, nil
}

type encoder struct {
	buf []byte
}

func (e *encoder) value(v any, depth int) error {
	if depth > maxDepth {
		return errors.New("JSON nesting is too deep")
	}
	switch v := v.(type) {
	case nil:
		e.buf = append(e.buf, "null"...)
	case bool:
		e.buf = strconv.AppendBool(e.buf, v)
	case string:
		if !utf8.ValidString(v) {
			return errors.New("string is not valid UTF-8")
		}
		e.buf = appendString(e.buf, v)
	case json.Number:
		return e.number(string(v))
	case float64:
		return e.float(v)
	case float32:
		return e.float(float64(v))
	case int:
		e.buf = strconv.AppendInt(e.buf, int64(v), 10)
	case int8:
		e.buf = strconv.AppendInt(e.buf, int64(v), 10)
	case int16:
		e.buf = strconv.AppendInt(e.buf, int64(v), 10)
	case int32:
		e.buf = strconv.AppendInt(e.buf, int64(v), 10)
	case int64:
		e.buf = strconv.AppendInt(e.buf, v, 10)
	case uint:
		e.buf = strconv.AppendUint(e.buf, uint64(v), 10)
	case uint8:
		e.buf = strconv.AppendUint(e.buf, uint64(v), 10)
	case uint16:
		e.buf = strconv.AppendUint(e.buf, uint64(v), 10)
	case uint32:
		e.buf = strconv.AppendUint(e.buf, uint64(v), 10)
	case uint64:
		e.buf = strconv.AppendUint(e.buf, v, 10)
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			if !utf8.ValidString(k) {
				return errors.New("object key is not valid UTF-8")
			}
			keys = append(keys, k)
		}
		// Byte order of UTF-8 strings is code point order, which is what
		// Python's sort_keys uses.
		slices.Sort(keys)
		e.buf = append(e.buf, '{')
		for i, k := range keys {
			if i > 0 {
				e.buf = append(e.buf, ',')
			}
			e.buf = appendString(e.buf, k)
			e.buf = append(e.buf, ':')
			if err := e.value(v[k], depth+1); err != nil {
				return err
			}
		}
		e.buf = append(e.buf, '}')
	case []any:
		e.buf = append(e.buf, '[')
		for i, item := range v {
			if i > 0 {
				e.buf = append(e.buf, ',')
			}
			if err := e.value(item, depth+1); err != nil {
				return err
			}
		}
		e.buf = append(e.buf, ']')
	default:
		return fmt.Errorf("unsupported value of type %T in the JSON payload", v)
	}
	return nil
}

// number writes a JSON number literal. Python parses a literal without
// fraction or exponent as an int and anything else as a float.
func (e *encoder) number(s string) error {
	if !isJSONNumber(s) {
		return fmt.Errorf("invalid JSON number %q", s)
	}
	if isIntegerLiteral(s) {
		if s == "-0" {
			s = "0"
		}
		e.buf = append(e.buf, s...)
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		// Only overflow is possible for a valid literal; Python would turn it
		// into Infinity, which is not JSON.
		return fmt.Errorf("number %s overflows a float64", s)
	}
	return e.float(f)
}

func (e *encoder) float(f float64) error {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return errors.New("NaN and Infinity are not valid JSON")
	}
	if f == math.Trunc(f) {
		e.buf = appendIntegral(e.buf, f)
	} else {
		e.buf = appendPyRepr(e.buf, f)
	}
	return nil
}

// isIntegerLiteral reports whether a valid JSON number literal has neither a
// fraction nor an exponent.
func isIntegerLiteral(s string) bool {
	return !strings.ContainsAny(s, ".eE")
}

// appendIntegral appends the exact integer value of an integral float, like
// Python's int(f).
func appendIntegral(b []byte, f float64) []byte {
	if f >= -(1<<63) && f < 1<<63 {
		return strconv.AppendInt(b, int64(f), 10) // -0.0 becomes 0
	}
	n, _ := new(big.Float).SetFloat64(f).Int(nil)
	return n.Append(b, 10)
}

// appendPyRepr appends Python's repr of a finite float: the shortest digits
// that round-trip, in fixed notation when the decimal exponent is in
// [-4, 16) (with ".0" for integral values) and in scientific notation
// otherwise, written like 1e-05 or 1.5e+16.
func appendPyRepr(b []byte, f float64) []byte {
	sci := strconv.AppendFloat(nil, f, 'e', -1, 64)
	i := bytes.IndexByte(sci, 'e')
	exp, _ := strconv.Atoi(string(sci[i+1:]))
	if exp < -4 || exp >= 16 {
		// Go writes the exponent like Python: a sign and at least two digits.
		return append(b, sci...)
	}
	start := len(b)
	b = strconv.AppendFloat(b, f, 'f', -1, 64)
	if bytes.IndexByte(b[start:], '.') < 0 {
		b = append(b, ".0"...)
	}
	return b
}

const hexDigits = "0123456789abcdef"

// appendString appends s as a JSON string the way Python's json.dumps does
// with ensure_ascii=False.
func appendString(b []byte, s string) []byte {
	b = append(b, '"')
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x20 && c != '"' && c != '\\' {
			continue
		}
		b = append(b, s[start:i]...)
		switch c {
		case '"':
			b = append(b, '\\', '"')
		case '\\':
			b = append(b, '\\', '\\')
		case '\n':
			b = append(b, '\\', 'n')
		case '\r':
			b = append(b, '\\', 'r')
		case '\t':
			b = append(b, '\\', 't')
		case '\b':
			b = append(b, '\\', 'b')
		case '\f':
			b = append(b, '\\', 'f')
		default:
			b = append(b, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xf])
		}
		start = i + 1
	}
	b = append(b, s[start:]...)
	return append(b, '"')
}

// isJSONNumber reports whether s is a valid JSON number literal.
func isJSONNumber(s string) bool {
	if s == "" {
		return false
	}
	if s[0] == '-' {
		s = s[1:]
		if s == "" {
			return false
		}
	}
	switch {
	case s[0] == '0':
		s = s[1:]
	case '1' <= s[0] && s[0] <= '9':
		s = s[1:]
		for len(s) > 0 && isDigit(s[0]) {
			s = s[1:]
		}
	default:
		return false
	}
	if len(s) >= 2 && s[0] == '.' && isDigit(s[1]) {
		s = s[2:]
		for len(s) > 0 && isDigit(s[0]) {
			s = s[1:]
		}
	}
	if len(s) >= 2 && (s[0] == 'e' || s[0] == 'E') {
		s = s[1:]
		if s[0] == '+' || s[0] == '-' {
			s = s[1:]
			if s == "" {
				return false
			}
		}
		for len(s) > 0 && isDigit(s[0]) {
			s = s[1:]
		}
	}
	return s == ""
}

func isDigit(c byte) bool { return '0' <= c && c <= '9' }
