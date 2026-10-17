package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	didit "github.com/didit-protocol/sdk-go"
)

// Header names Didit sends with every webhook.
const (
	headerSignature               = "X-Signature"
	headerSignatureV2             = "X-Signature-V2"
	headerSignatureSimple         = "X-Signature-Simple"
	headerSignaturePrevious       = "X-Signature-Previous"
	headerSignatureV2Previous     = "X-Signature-V2-Previous"
	headerSignatureSimplePrevious = "X-Signature-Simple-Previous"
	headerTimestamp               = "X-Timestamp"
)

// Verify checks a webhook delivery and returns its typed event.
//
// payload is the request body exactly as received and header the request
// headers (names are matched case-insensitively). secret is the destination's
// signing secret; WithSecrets adds more secrets for a rotation.
//
// The checks run in this order:
//
//  1. An empty secret returns a *didit.ConfigurationError.
//  2. The body must be a UTF-8 JSON object (invalid_payload).
//  3. The body's timestamp must be an integer (missing_timestamp), equal to
//     X-Timestamp when that header is present (timestamp_mismatch), and
//     within the tolerance of the current time (timestamp_out_of_tolerance).
//  4. For each secret, the delivery is accepted when X-Signature is the
//     HMAC-SHA256 of the body, or X-Signature-V2 the HMAC-SHA256 of its
//     canonical JSON (see Canonicalize), or X-Signature-Previous or
//     X-Signature-V2-Previous match likewise. With WithAllowSimple,
//     X-Signature-Simple and X-Signature-Simple-Previous count too, and the
//     event then reports Authenticated() == false. Without any of these
//     headers the reason is missing_signature, otherwise invalid_signature.
//
// Signatures are compared in constant time, and only values made of 64
// lowercase hex characters are considered. Every verification failure is a
// *didit.WebhookVerificationError whose Reason says what failed.
func Verify(payload []byte, header http.Header, secret string, opts ...Option) (Event, error) {
	cfg, err := newConfig(secret, opts)
	if err != nil {
		return nil, err
	}
	v, err := parseJSON(payload)
	if err != nil {
		return nil, err
	}
	body, ok := v.(map[string]any)
	if !ok {
		return nil, invalidPayload("body is not a JSON object")
	}
	canonical, err := canonicalJSON(body)
	if err != nil {
		return nil, invalidPayload(err.Error())
	}
	ts, err := checkTimestamp(body, header, cfg)
	if err != nil {
		return nil, err
	}
	authenticated, err := checkSignatures(cfg, header, payload, canonical, body, ts)
	if err != nil {
		return nil, err
	}
	return newEvent(body, bytes.Clone(payload), authenticated), nil
}

// VerifyParsed is Verify for frameworks that only expose the parsed JSON body.
// payload must be the map[string]any that encoding/json produces; decode it
// with json.Decoder.UseNumber so that integers above 2^53 survive, because
// the signature covers their exact digits.
//
// Without the raw body X-Signature cannot be checked, so VerifyParsed checks
// X-Signature-V2 and X-Signature-V2-Previous, plus X-Signature-Simple and
// X-Signature-Simple-Previous with WithAllowSimple. The timestamp may be a
// float64 with an integral value, since decoding without UseNumber turns
// every number into a float64. The event's Raw returns the canonical JSON.
func VerifyParsed(payload any, header http.Header, secret string, opts ...Option) (Event, error) {
	cfg, err := newConfig(secret, opts)
	if err != nil {
		return nil, err
	}
	body, ok := payload.(map[string]any)
	if !ok {
		switch payload.(type) {
		case []byte, json.RawMessage, string:
			return nil, invalidPayload("VerifyParsed needs the parsed body (map[string]any); use Verify for the raw body")
		}
		return nil, invalidPayload("body is not a JSON object")
	}
	canonical, err := canonicalJSON(body)
	if err != nil {
		return nil, invalidPayload(err.Error())
	}
	ts, err := checkTimestamp(body, header, cfg)
	if err != nil {
		return nil, err
	}
	authenticated, err := checkSignatures(cfg, header, nil, canonical, body, ts)
	if err != nil {
		return nil, err
	}
	return newEvent(body, canonical, authenticated), nil
}

func verificationError(reason didit.WebhookVerificationReason, msg string) error {
	return &didit.WebhookVerificationError{Reason: reason, Message: msg}
}

// checkTimestamp applies the timestamp rules and returns the body's timestamp.
func checkTimestamp(body map[string]any, header http.Header, cfg *config) (*big.Int, error) {
	ts, ok := integerValue(body["timestamp"])
	if !ok {
		return nil, verificationError(didit.WebhookReasonMissingTimestamp, "body has no integer timestamp")
	}
	if h := headerValue(header, headerTimestamp); h != "" {
		hts, ok := parseDecimal(h)
		if !ok || hts.Cmp(ts) != 0 {
			return nil, verificationError(didit.WebhookReasonTimestampMismatch, "X-Timestamp differs from the body's timestamp")
		}
	}
	// |now - ts| > tolerance, computed exactly in nanoseconds.
	now := cfg.now()
	diff := new(big.Int).Mul(big.NewInt(now.Unix()), big.NewInt(int64(time.Second)))
	diff.Add(diff, big.NewInt(int64(now.Nanosecond())))
	diff.Sub(diff, new(big.Int).Mul(ts, big.NewInt(int64(time.Second))))
	if diff.Abs(diff).Cmp(big.NewInt(int64(cfg.tolerance))) > 0 {
		return nil, verificationError(didit.WebhookReasonTimestampOutOfTolerance,
			"timestamp is more than "+cfg.tolerance.String()+" away from the current time")
	}
	return ts, nil
}

// integerValue returns the value of an integer JSON number. A float64 or
// float32 counts when integral, because encoding/json without UseNumber
// decodes every number that way.
func integerValue(v any) (*big.Int, bool) {
	switch v := v.(type) {
	case json.Number:
		if !isJSONNumber(string(v)) || !isIntegerLiteral(string(v)) {
			return nil, false
		}
		return new(big.Int).SetString(string(v), 10)
	case float64:
		return integralFloat(v)
	case float32:
		return integralFloat(float64(v))
	case int:
		return big.NewInt(int64(v)), true
	case int64:
		return big.NewInt(v), true
	case int32:
		return big.NewInt(int64(v)), true
	case uint64:
		return new(big.Int).SetUint64(v), true
	case uint32:
		return big.NewInt(int64(v)), true
	}
	return nil, false
}

func integralFloat(f float64) (*big.Int, bool) {
	if math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) {
		return nil, false
	}
	n, _ := new(big.Float).SetFloat64(f).Int(nil)
	return n, true
}

// parseDecimal parses an optionally signed decimal integer.
func parseDecimal(s string) (*big.Int, bool) {
	digits := strings.TrimLeft(s, "+-")
	if len(s)-len(digits) > 1 || digits == "" {
		return nil, false
	}
	for i := 0; i < len(digits); i++ {
		if !isDigit(digits[i]) {
			return nil, false
		}
	}
	return new(big.Int).SetString(s, 10)
}

// headerValue returns the first value of the named header, trimmed. It also
// finds keys that are not in canonical form, as in an http.Header built
// from a map literal.
func headerValue(h http.Header, name string) string {
	if v := h.Get(name); v != "" {
		return strings.TrimSpace(v)
	}
	for k, vs := range h {
		if len(vs) > 0 && vs[0] != "" && strings.EqualFold(k, name) {
			return strings.TrimSpace(vs[0])
		}
	}
	return ""
}

type signatureHeaders struct {
	raw, rawPrev, v2, v2Prev, simple, simplePrev string
}

func readSignatures(h http.Header) signatureHeaders {
	return signatureHeaders{
		raw:        headerValue(h, headerSignature),
		rawPrev:    headerValue(h, headerSignaturePrevious),
		v2:         headerValue(h, headerSignatureV2),
		v2Prev:     headerValue(h, headerSignatureV2Previous),
		simple:     headerValue(h, headerSignatureSimple),
		simplePrev: headerValue(h, headerSignatureSimplePrevious),
	}
}

// checkSignatures returns whether a full-body signature matched (true) or
// only a simple signature did (false). raw is nil for VerifyParsed.
func checkSignatures(cfg *config, header http.Header, raw, canonical []byte, body map[string]any, ts *big.Int) (bool, error) {
	h := readSignatures(header)
	hasRaw := h.raw != "" || h.rawPrev != ""
	hasV2 := h.v2 != "" || h.v2Prev != ""
	hasSimple := h.simple != "" || h.simplePrev != ""
	if !hasRaw && !hasV2 && !hasSimple {
		return false, verificationError(didit.WebhookReasonMissingSignature,
			"no X-Signature, X-Signature-V2 or X-Signature-Simple header")
	}

	var rawSigs [][]byte
	if raw != nil {
		rawSigs = decodeSignatures(h.raw, h.rawPrev)
	}
	v2Sigs := decodeSignatures(h.v2, h.v2Prev)
	for _, secret := range cfg.secrets {
		key := []byte(secret)
		if matches(key, raw, rawSigs) || matches(key, canonical, v2Sigs) {
			return true, nil
		}
	}

	if cfg.allowSimple {
		if simpleSigs := decodeSignatures(h.simple, h.simplePrev); len(simpleSigs) > 0 {
			if msg, ok := simpleMessage(body, ts); ok {
				for _, secret := range cfg.secrets {
					if matches([]byte(secret), msg, simpleSigs) {
						return false, nil
					}
				}
			}
		}
	}

	msg := "no signature matches the secret"
	switch {
	case raw == nil && hasRaw && !hasV2 && !(cfg.allowSimple && hasSimple):
		msg = "X-Signature signs the raw body, which VerifyParsed does not have; use Verify, or check X-Signature-V2"
	case !hasRaw && !hasV2 && !cfg.allowSimple:
		msg = "only X-Signature-Simple is present, and it counts only with WithAllowSimple"
	}
	return false, verificationError(didit.WebhookReasonInvalidSignature, msg)
}

// decodeSignatures decodes the well-formed (64 lowercase hex characters)
// values among vs and ignores the others, which can never match.
func decodeSignatures(vs ...string) [][]byte {
	var out [][]byte
	for _, v := range vs {
		if !isLowerHex64(v) {
			continue
		}
		b, err := hex.DecodeString(v)
		if err == nil {
			out = append(out, b)
		}
	}
	return out
}

func isLowerHex64(s string) bool {
	if len(s) != 2*sha256.Size {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !isDigit(c) && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// matches reports whether HMAC-SHA256(key, msg) equals one of sigs, comparing
// in constant time.
func matches(key, msg []byte, sigs [][]byte) bool {
	if len(sigs) == 0 {
		return false
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(msg)
	sum := mac.Sum(nil)
	ok := false
	for _, sig := range sigs {
		if hmac.Equal(sum, sig) {
			ok = true
		}
	}
	return ok
}

// simpleMessage builds what X-Signature-Simple signs:
// "{timestamp}:{session_id}:{status}:{webhook_type}", each field formatted
// like the backend's str(data.get(k, "")) in Python: a missing field is "",
// null is "None", true and false are "True" and "False", integers are
// decimal, floats use Python's repr and strings are used as they are. A
// field holding an object or array cannot be reproduced, and then no simple
// signature matches.
func simpleMessage(body map[string]any, ts *big.Int) ([]byte, bool) {
	var b []byte
	b = ts.Append(b, 10)
	for _, k := range [...]string{"session_id", "status", "webhook_type"} {
		b = append(b, ':')
		v, present := body[k]
		if !present {
			continue
		}
		var ok bool
		if b, ok = appendPyStr(b, v); !ok {
			return nil, false
		}
	}
	return b, true
}

// appendPyStr appends Python's str() of a decoded JSON scalar.
func appendPyStr(b []byte, v any) ([]byte, bool) {
	switch v := v.(type) {
	case nil:
		return append(b, "None"...), true
	case bool:
		if v {
			return append(b, "True"...), true
		}
		return append(b, "False"...), true
	case string:
		return append(b, v...), true
	case json.Number:
		s := string(v)
		if !isJSONNumber(s) {
			return nil, false
		}
		if isIntegerLiteral(s) {
			if s == "-0" {
				s = "0"
			}
			return append(b, s...), true
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, false
		}
		return appendPyRepr(b, f), true
	case float64:
		return appendPyStrFloat(b, v)
	case float32:
		return appendPyStrFloat(b, float64(v))
	}
	if n, ok := integerValue(v); ok {
		return n.Append(b, 10), true
	}
	return nil, false
}

// appendPyStrFloat formats a float64 from a body decoded without UseNumber.
// An integral value was most likely an integer literal, which Python would
// print without ".0".
func appendPyStrFloat(b []byte, f float64) ([]byte, bool) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil, false
	}
	if f == math.Trunc(f) && math.Abs(f) <= 1<<53 {
		return strconv.AppendInt(b, int64(f), 10), true
	}
	return appendPyRepr(b, f), true
}
