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
//
// Anyone can send a request to a webhook endpoint, so the checks before a
// signature matches cost time and memory in proportion to the body's size.
// The canonical JSON is built only for a well-formed X-Signature-V2 value.
// Integral numbers written as floats far outside the int64 range expand in
// it (1e308 has 309 digits) by at most 64 KiB in total; Didit sends such
// numbers as integers, so a body that needs more is not a delivery from
// Didit, and its X-Signature-V2 does not match.
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
	c, err := newCanonicalBody(body)
	if err != nil {
		return nil, err
	}
	ts, err := checkTimestamp(body, header, cfg)
	if err != nil {
		return nil, err
	}
	authenticated, err := checkSignatures(cfg, header, payload, c, ts)
	if err != nil {
		return nil, err
	}
	return newEvent(body, bytes.Clone(payload), authenticated, c.limit), nil
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
// The limits Verify documents apply too.
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
	c, err := newCanonicalBody(body)
	if err != nil {
		return nil, err
	}
	ts, err := checkTimestamp(body, header, cfg)
	if err != nil {
		return nil, err
	}
	authenticated, err := checkSignatures(cfg, header, nil, c, ts)
	if err != nil {
		return nil, err
	}
	canonical := c.bytes()
	if canonical == nil {
		// Only a simple signature, which leaves the body unsigned, gets here.
		return nil, invalidPayload("integral numbers far outside the int64 range make the canonical JSON too long")
	}
	return newEvent(body, canonical, authenticated, c.limit), nil
}

// maxCanonicalGrowth bounds how much longer the canonical JSON may be than
// the length measureJSON gives (see Verify).
const maxCanonicalGrowth = 64 << 10

// canonicalBody is the canonical JSON of a webhook body, built when a
// signature needs it, and at most limit bytes long.
type canonicalBody struct {
	body  map[string]any
	limit int
	built bool
	json  []byte // nil when longer than limit
}

// newCanonicalBody checks that body can be made canonical (invalid_payload
// otherwise), without building it.
func newCanonicalBody(body map[string]any) (*canonicalBody, error) {
	n, err := measureJSON(body)
	if err != nil {
		return nil, invalidPayload(err.Error())
	}
	return &canonicalBody{body: body, limit: n + maxCanonicalGrowth}, nil
}

// bytes returns the canonical JSON, or nil when it is longer than the limit.
func (c *canonicalBody) bytes() []byte {
	if !c.built {
		// Once measureJSON passed, errTooLong is the only possible error.
		c.json, _ = canonicalJSON(c.body, c.limit)
		c.built = true
	}
	return c.json
}

func verificationError(reason didit.WebhookVerificationReason, msg string) error {
	return &didit.WebhookVerificationError{Reason: reason, Message: msg}
}

// checkTimestamp applies the timestamp rules and returns the body's timestamp
// in decimal (see integerText).
func checkTimestamp(body map[string]any, header http.Header, cfg *config) (string, error) {
	ts, ok := integerText(body["timestamp"])
	if !ok {
		return "", verificationError(didit.WebhookReasonMissingTimestamp, "body has no integer timestamp")
	}
	if h := headerValue(header, headerTimestamp); h != "" {
		if hts, ok := decimalText(h); !ok || hts != ts {
			return "", verificationError(didit.WebhookReasonTimestampMismatch, "X-Timestamp differs from the body's timestamp")
		}
	}
	if !withinTolerance(ts, cfg.now(), cfg.tolerance) {
		return "", verificationError(didit.WebhookReasonTimestampOutOfTolerance,
			"timestamp is more than "+cfg.tolerance.String()+" away from the current time")
	}
	return ts, nil
}

// maxTimestampDigits bounds the timestamps parsed as numbers. One with more
// digits is at least 10^19 seconds from 1970, and every time.Time is within
// 2^63 seconds of it, so the gap is far beyond any tolerance (a
// time.Duration is at most about 292 years). Not parsing it matters, because
// math/big takes time quadratic in the number of digits.
const maxTimestampDigits = 19

// withinTolerance reports whether |now - ts| <= tolerance, computed exactly
// in nanoseconds.
func withinTolerance(ts string, now time.Time, tolerance time.Duration) bool {
	if len(strings.TrimPrefix(ts, "-")) > maxTimestampDigits {
		return false
	}
	t, _ := new(big.Int).SetString(ts, 10)
	diff := new(big.Int).Mul(big.NewInt(now.Unix()), big.NewInt(int64(time.Second)))
	diff.Add(diff, big.NewInt(int64(now.Nanosecond())))
	diff.Sub(diff, t.Mul(t, big.NewInt(int64(time.Second))))
	return diff.Abs(diff).Cmp(big.NewInt(int64(tolerance))) <= 0
}

// integerText returns an integer JSON value in decimal: an optional "-",
// then digits without leading zeros. A float64 or float32 counts when
// integral, because encoding/json without UseNumber decodes every number
// that way. Equal values give equal text, so comparing them needs no
// parsing, however many digits a body sends.
func integerText(v any) (string, bool) {
	switch v := v.(type) {
	case json.Number:
		// A JSON integer has no leading zeros; only -0 needs rewriting.
		s := string(v)
		if !isJSONNumber(s) || !isIntegerLiteral(s) {
			return "", false
		}
		if s == "-0" {
			return "0", true
		}
		return s, true
	case float64:
		return integralFloat(v)
	case float32:
		return integralFloat(float64(v))
	case int:
		return strconv.FormatInt(int64(v), 10), true
	case int8:
		return strconv.FormatInt(int64(v), 10), true
	case int16:
		return strconv.FormatInt(int64(v), 10), true
	case int32:
		return strconv.FormatInt(int64(v), 10), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case uint:
		return strconv.FormatUint(uint64(v), 10), true
	case uint8:
		return strconv.FormatUint(uint64(v), 10), true
	case uint16:
		return strconv.FormatUint(uint64(v), 10), true
	case uint32:
		return strconv.FormatUint(uint64(v), 10), true
	case uint64:
		return strconv.FormatUint(v, 10), true
	}
	return "", false
}

// integralFloat returns an integral float's exact value in decimal.
func integralFloat(f float64) (string, bool) {
	if math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) {
		return "", false
	}
	return string(appendIntegral(nil, f)), true
}

// decimalText returns an optionally signed decimal integer in the form
// integerText uses.
func decimalText(s string) (string, bool) {
	neg := strings.HasPrefix(s, "-")
	if neg || strings.HasPrefix(s, "+") {
		s = s[1:]
	}
	if s == "" {
		return "", false
	}
	for i := 0; i < len(s); i++ {
		if !isDigit(s[i]) {
			return "", false
		}
	}
	s = strings.TrimLeft(s, "0")
	switch {
	case s == "":
		return "0", true
	case neg:
		return "-" + s, true
	}
	return s, true
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
func checkSignatures(cfg *config, header http.Header, raw []byte, c *canonicalBody, ts string) (bool, error) {
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
	for _, secret := range cfg.secrets {
		if matches([]byte(secret), raw, rawSigs) {
			return true, nil
		}
	}
	if v2Sigs := decodeSignatures(h.v2, h.v2Prev); len(v2Sigs) > 0 {
		if canonical := c.bytes(); canonical != nil {
			for _, secret := range cfg.secrets {
				if matches([]byte(secret), canonical, v2Sigs) {
					return true, nil
				}
			}
		}
	}

	if cfg.allowSimple {
		if simpleSigs := decodeSignatures(h.simple, h.simplePrev); len(simpleSigs) > 0 {
			if msg, ok := simpleMessage(c.body, ts); ok {
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
	case raw == nil && hasRaw && !hasV2 && (!cfg.allowSimple || !hasSimple):
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
func simpleMessage(body map[string]any, ts string) ([]byte, bool) {
	b := []byte(ts)
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
	if s, ok := integerText(v); ok {
		return append(b, s...), true
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
