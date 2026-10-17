package webhook_test

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	didit "github.com/didit-protocol/sdk-go"
	"github.com/didit-protocol/sdk-go/webhook"
)

func sessionPayload() map[string]any {
	return with(envelope("status.updated"),
		"session_id", "11111111-2222-3333-4444-555555555555",
		"status", "Approved",
	)
}

func TestConfigurationErrors(t *testing.T) {
	body, h := signedBody(t, sessionPayload(), testSecret)
	cases := []struct {
		name   string
		secret string
		opts   []webhook.Option
	}{
		{"empty secret", "", nil},
		{"empty extra secret", testSecret, []webhook.Option{webhook.WithSecrets(otherTestSecret, "")}},
		{"negative tolerance", testSecret, []webhook.Option{webhook.WithTolerance(-time.Second)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, payload := range [][]byte{body, []byte("not json")} {
				_, err := webhook.Verify(payload, h, tc.secret, append(tc.opts, testNow)...)
				var cerr *didit.ConfigurationError
				if !errors.As(err, &cerr) {
					t.Fatalf("Verify: got %v (%T), want *didit.ConfigurationError", err, err)
				}
				if strings.Contains(err.Error(), testSecret) {
					t.Fatal("error message contains the secret")
				}
			}
			_, err := webhook.VerifyParsed(decodeUseNumber(t, body), h, tc.secret, append(tc.opts, testNow)...)
			var cerr *didit.ConfigurationError
			if !errors.As(err, &cerr) {
				t.Fatalf("VerifyParsed: got %v (%T), want *didit.ConfigurationError", err, err)
			}
		})
	}
}

func TestErrorsAreDiditErrors(t *testing.T) {
	_, err := webhook.Verify([]byte("{}"), nil, testSecret)
	var e didit.Error
	if !errors.As(err, &e) {
		t.Fatalf("%T is not a didit.Error", err)
	}
	if !strings.Contains(err.Error(), "missing_timestamp") {
		t.Fatalf("error message %q does not name the reason", err)
	}
}

func TestNilOptionsAndClockAreIgnored(t *testing.T) {
	body, h := signedBody(t, with(sessionPayload(), "timestamp", time.Now().Unix()), testSecret)
	mustVerify(t, body, h, testSecret, nil, webhook.WithClock(nil))
}

func TestInvalidPayload(t *testing.T) {
	cases := map[string]string{
		"empty":             "",
		"whitespace":        "  \n",
		"array":             "[]",
		"string":            `"x"`,
		"number":            "1",
		"null":              "null",
		"true":              "true",
		"truncated":         `{"timestamp":1760702400`,
		"trailing data":     `{"timestamp":1760702400} {}`,
		"trailing garbage":  `{"timestamp":1760702400}x`,
		"NaN":               `{"timestamp":1760702400,"x":NaN}`,
		"Infinity":          `{"timestamp":1760702400,"x":Infinity}`,
		"overflow":          `{"timestamp":1760702400,"x":1e400}`,
		"negative overflow": `{"timestamp":1760702400,"x":[-1e400]}`,
		"invalid UTF-8":     "{\"timestamp\":1760702400,\"x\":\"\xff\"}",
		"BOM":               "\ufeff{\"timestamp\":1760702400}",
		"single quotes":     `{'timestamp':1760702400}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			h := signHeadersRaw(body)
			_, err := webhook.Verify([]byte(body), h, testSecret, testNow)
			wantReason(t, err, didit.WebhookReasonInvalidPayload)
		})
	}
}

// signHeadersRaw signs any body with X-Signature, so that only the payload
// can be at fault.
func signHeadersRaw(body string) http.Header {
	h := http.Header{}
	h.Set("X-Signature", hmacHex(testSecret, []byte(body)))
	return h
}

func TestTimestampRules(t *testing.T) {
	type tc struct {
		name      string
		timestamp any // value of the body's timestamp; omitted when nil and absent is true
		absent    bool
		header    string // X-Timestamp; "" for none
		now       int64
		tolerance time.Duration
		want      didit.WebhookVerificationReason // "" for success
	}
	cases := []tc{
		{name: "absent", absent: true, want: didit.WebhookReasonMissingTimestamp},
		{name: "null", timestamp: nil, want: didit.WebhookReasonMissingTimestamp},
		{name: "string", timestamp: "1760702400", want: didit.WebhookReasonMissingTimestamp},
		{name: "bool", timestamp: true, want: didit.WebhookReasonMissingTimestamp},
		{name: "float literal", timestamp: json.Number("1760702400.0"), want: didit.WebhookReasonMissingTimestamp},
		{name: "exponent literal", timestamp: json.Number("1.7607024e9"), want: didit.WebhookReasonMissingTimestamp},
		{name: "fraction", timestamp: 1760702400.5, want: didit.WebhookReasonMissingTimestamp},
		{name: "object", timestamp: map[string]any{}, want: didit.WebhookReasonMissingTimestamp},
		{name: "header equal", timestamp: testTimestamp, header: "1760702400"},
		{name: "header leading zero", timestamp: testTimestamp, header: "01760702400"},
		{name: "header plus sign", timestamp: testTimestamp, header: "+1760702400"},
		{name: "header differs", timestamp: testTimestamp, header: "1760702401", want: didit.WebhookReasonTimestampMismatch},
		{name: "header float", timestamp: testTimestamp, header: "1760702400.0", want: didit.WebhookReasonTimestampMismatch},
		{name: "header text", timestamp: testTimestamp, header: "yesterday", want: didit.WebhookReasonTimestampMismatch},
		{name: "header signs only", timestamp: testTimestamp, header: "+-1", want: didit.WebhookReasonTimestampMismatch},
		{name: "header sign alone", timestamp: testTimestamp, header: "-", want: didit.WebhookReasonTimestampMismatch},
		{name: "mismatch before tolerance", timestamp: testTimestamp, header: "1", now: testTimestamp + 3600, want: didit.WebhookReasonTimestampMismatch},
		{name: "exactly at tolerance in the past", timestamp: testTimestamp, now: testTimestamp + 300},
		{name: "exactly at tolerance in the future", timestamp: testTimestamp, now: testTimestamp - 300},
		{name: "one second too old", timestamp: testTimestamp, now: testTimestamp + 301, want: didit.WebhookReasonTimestampOutOfTolerance},
		{name: "one second too new", timestamp: testTimestamp, now: testTimestamp - 301, want: didit.WebhookReasonTimestampOutOfTolerance},
		{name: "custom tolerance", timestamp: testTimestamp, now: testTimestamp + 20, tolerance: 10 * time.Second, want: didit.WebhookReasonTimestampOutOfTolerance},
		{name: "wide tolerance", timestamp: testTimestamp, now: testTimestamp + 3600, tolerance: time.Hour},
		{name: "beyond int64", timestamp: json.Number("99999999999999999999999"), want: didit.WebhookReasonTimestampOutOfTolerance},
		{name: "negative zero", timestamp: json.Number("-0"), now: 100},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := sessionPayload()
			if c.absent {
				delete(p, "timestamp")
			} else {
				p["timestamp"] = c.timestamp
			}
			body, h := signedBody(t, p, testSecret)
			h.Del("X-Timestamp")
			if c.header != "" {
				h.Set("X-Timestamp", c.header)
			}
			now := c.now
			if now == 0 {
				now = testTimestamp + 10
			}
			opts := []webhook.Option{clockAt(now)}
			if c.tolerance != 0 {
				opts = append(opts, webhook.WithTolerance(c.tolerance))
			}
			_, err := webhook.Verify(body, h, testSecret, opts...)
			if c.want == "" {
				if err != nil {
					t.Fatalf("Verify: %v", err)
				}
				return
			}
			wantReason(t, err, c.want)
		})
	}
}

func TestToleranceUsesSubSecondTime(t *testing.T) {
	body, h := signedBody(t, sessionPayload(), testSecret)
	at := func(d time.Duration) webhook.Option {
		return webhook.WithClock(func() time.Time { return time.Unix(testTimestamp, 0).Add(d) })
	}
	mustVerify(t, body, h, testSecret, at(300*time.Second))
	_, err := webhook.Verify(body, h, testSecret, at(300*time.Second+time.Millisecond))
	wantReason(t, err, didit.WebhookReasonTimestampOutOfTolerance)
	_, err = webhook.Verify(body, h, testSecret, at(-300*time.Second-time.Millisecond))
	wantReason(t, err, didit.WebhookReasonTimestampOutOfTolerance)
}

func TestTimestampBeforeSignature(t *testing.T) {
	// A stale delivery with a bad signature reports the timestamp.
	body, _ := signedBody(t, sessionPayload(), testSecret)
	h := http.Header{}
	h.Set("X-Signature", strings.Repeat("0", 64))
	_, err := webhook.Verify(body, h, testSecret, clockAt(testTimestamp+1000))
	wantReason(t, err, didit.WebhookReasonTimestampOutOfTolerance)
	// And with no signature at all.
	_, err = webhook.Verify(body, http.Header{}, testSecret, clockAt(testTimestamp+1000))
	wantReason(t, err, didit.WebhookReasonTimestampOutOfTolerance)
}

func TestSignatureFormat(t *testing.T) {
	body, good := signedBody(t, sessionPayload(), testSecret)
	cases := []struct {
		name   string
		mangle func(string) string
		ok     bool
	}{
		{"lowercase", func(s string) string { return s }, true},
		{"surrounding spaces", func(s string) string { return "  " + s + " " }, true},
		{"uppercase", strings.ToUpper, false},
		{"one uppercase letter", func(s string) string {
			i := strings.IndexAny(s, "abcdef")
			return s[:i] + strings.ToUpper(s[i:i+1]) + s[i+1:]
		}, false},
		{"too short", func(s string) string { return s[:63] }, false},
		{"too long", func(s string) string { return s + "0" }, false},
		{"sha256= prefix", func(s string) string { return "sha256=" + s }, false},
		{"not hex", func(s string) string { return strings.Repeat("g", 64) }, false},
		{"zeros", func(s string) string { return strings.Repeat("0", 64) }, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, name := range []string{"X-Signature", "X-Signature-V2"} {
				h := http.Header{}
				h.Set(name, c.mangle(good.Get(name)))
				_, err := webhook.Verify(body, h, testSecret, testNow)
				if c.ok {
					if err != nil {
						t.Fatalf("%s: %v", name, err)
					}
					continue
				}
				wantReason(t, err, didit.WebhookReasonInvalidSignature)
			}
		})
	}
}

func TestMissingSignature(t *testing.T) {
	body, _ := signedBody(t, sessionPayload(), testSecret)
	h := http.Header{}
	h.Set("X-Timestamp", "1760702400")
	h.Set("X-Signature-V3", strings.Repeat("a", 64))
	h["X-Signature"] = []string{""} // present but empty counts as absent
	_, err := webhook.Verify(body, h, testSecret, testNow)
	wantReason(t, err, didit.WebhookReasonMissingSignature)
	_, err = webhook.Verify(body, nil, testSecret, testNow)
	wantReason(t, err, didit.WebhookReasonMissingSignature)
	_, err = webhook.VerifyParsed(decodeUseNumber(t, body), nil, testSecret, testNow)
	wantReason(t, err, didit.WebhookReasonMissingSignature)
}

func TestHeaderNamesAreCaseInsensitive(t *testing.T) {
	body, good := signedBody(t, sessionPayload(), testSecret)

	h := http.Header{}
	h.Set("x-signature-v2", good.Get("X-Signature-V2"))
	h.Set("x-timestamp", "1760702400")
	mustVerify(t, body, h, testSecret, testNow)

	// Keys that are not in canonical form, as in a map literal.
	h = http.Header{
		"x-signature": {good.Get("X-Signature")},
		"x-timestamp": {"1760702401"},
	}
	_, err := webhook.Verify(body, h, testSecret, testNow)
	wantReason(t, err, didit.WebhookReasonTimestampMismatch)
	h["x-timestamp"] = []string{"1760702400"}
	mustVerify(t, body, h, testSecret, testNow)
	h = http.Header{"X-SIGNATURE-V2": {good.Get("X-Signature-V2")}}
	if _, err := webhook.VerifyParsed(decodeUseNumber(t, body), h, testSecret, testNow); err != nil {
		t.Fatal(err)
	}
}

func TestEachSignatureHeaderAlone(t *testing.T) {
	body, good := signedBody(t, sessionPayload(), testSecret)
	for _, name := range []string{"X-Signature", "X-Signature-V2"} {
		for _, header := range []string{name, name + "-Previous"} {
			h := http.Header{}
			h.Set(header, good.Get(name))
			ev := mustVerify(t, body, h, testSecret, testNow)
			if !ev.Authenticated() {
				t.Fatalf("%s: not authenticated", header)
			}
		}
	}
}

func TestWrongSignatureWithOtherHeadersPresent(t *testing.T) {
	// A wrong X-Signature does not prevent a correct X-Signature-V2 from
	// matching, and the reverse.
	body, good := signedBody(t, sessionPayload(), testSecret)
	h := http.Header{}
	h.Set("X-Signature", strings.Repeat("0", 64))
	h.Set("X-Signature-V2", good.Get("X-Signature-V2"))
	mustVerify(t, body, h, testSecret, testNow)
	h = http.Header{}
	h.Set("X-Signature", good.Get("X-Signature"))
	h.Set("X-Signature-V2", strings.Repeat("0", 64))
	mustVerify(t, body, h, testSecret, testNow)
}

func TestReencodedBodyPassesV2Only(t *testing.T) {
	// Middleware that re-encodes the body breaks X-Signature but not
	// X-Signature-V2.
	body, h := signedBody(t, with(sessionPayload(), "metadata", map[string]any{"name": "José", "score": 87.5}), testSecret)
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatal(err)
	}
	reencoded, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	ev := mustVerify(t, reencoded, h, testSecret, testNow)
	if string(ev.Raw()) != string(reencoded) {
		t.Fatal("Raw() is not the received body")
	}
	h.Del("X-Signature-V2")
	_, err = webhook.Verify(reencoded, h, testSecret, testNow)
	wantReason(t, err, didit.WebhookReasonInvalidSignature)
}

func TestRawIsACopy(t *testing.T) {
	body, h := signedBody(t, sessionPayload(), testSecret)
	ev := mustVerify(t, body, h, testSecret, testNow)
	want := string(body)
	for i := range body {
		body[i] = ' '
	}
	if string(ev.Raw()) != want {
		t.Fatal("Raw() aliases the caller's buffer")
	}
}

func TestWithSecretsOrderAndRotation(t *testing.T) {
	body, h := signedBody(t, sessionPayload(), otherTestSecret)
	_, err := webhook.Verify(body, h, testSecret, testNow)
	wantReason(t, err, didit.WebhookReasonInvalidSignature)
	mustVerify(t, body, h, testSecret, testNow, webhook.WithSecrets(otherTestSecret))
	mustVerify(t, body, h, testSecret, testNow, webhook.WithSecrets("whsec_test_unused"), webhook.WithSecrets(otherTestSecret))

	// WithSecrets copies its arguments.
	secrets := []string{otherTestSecret}
	opt := webhook.WithSecrets(secrets...)
	secrets[0] = "whsec_test_changed"
	mustVerify(t, body, h, testSecret, testNow, opt)
}

func TestAllowSimple(t *testing.T) {
	simpleHeaders := func(msg, secret string) http.Header {
		h := http.Header{}
		h.Set("X-Signature-Simple", hmacHex(secret, []byte(msg)))
		return h
	}
	cases := []struct {
		name string
		body string
		msg  string // what the backend signs
	}{
		{"strings", `{"timestamp":1760702400,"session_id":"abc","status":"Approved","webhook_type":"status.updated"}`,
			"1760702400:abc:Approved:status.updated"},
		{"missing fields", `{"timestamp":1760702400}`, "1760702400:::"},
		{"null", `{"timestamp":1760702400,"session_id":null,"status":"Approved","webhook_type":"status.updated"}`,
			"1760702400:None:Approved:status.updated"},
		{"booleans", `{"timestamp":1760702400,"session_id":true,"status":false,"webhook_type":"status.updated"}`,
			"1760702400:True:False:status.updated"},
		{"integers", `{"timestamp":1760702400,"session_id":12345678901234567890,"status":-0,"webhook_type":"x"}`,
			"1760702400:12345678901234567890:0:x"},
		{"floats", `{"timestamp":1760702400,"session_id":87.0,"status":1e16,"webhook_type":0.1}`,
			"1760702400:87.0:1e+16:0.1"},
		{"unicode", `{"timestamp":1760702400,"session_id":"Jos\u00e9","status":"In Review","webhook_type":"data.updated"}`,
			"1760702400:José:In Review:data.updated"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := simpleHeaders(c.msg, testSecret)
			_, err := webhook.Verify([]byte(c.body), h, testSecret, testNow)
			wantReason(t, err, didit.WebhookReasonInvalidSignature)
			if !strings.Contains(err.Error(), "WithAllowSimple") {
				t.Fatalf("error %q does not mention WithAllowSimple", err)
			}
			ev := mustVerify(t, []byte(c.body), h, testSecret, testNow, webhook.WithAllowSimple())
			if ev.Authenticated() {
				t.Fatal("simple signature reported as authenticated")
			}
			ev, err = webhook.VerifyParsed(decodeUseNumber(t, []byte(c.body)), h, testSecret, testNow, webhook.WithAllowSimple())
			if err != nil || ev.Authenticated() {
				t.Fatalf("VerifyParsed: %v, authenticated %v", err, ev != nil && ev.Authenticated())
			}
			// Previous header and an extra secret.
			h = http.Header{}
			h.Set("X-Signature-Simple-Previous", hmacHex(otherTestSecret, []byte(c.msg)))
			mustVerify(t, []byte(c.body), h, testSecret, testNow, webhook.WithAllowSimple(), webhook.WithSecrets(otherTestSecret))
		})
	}

	t.Run("object field never matches", func(t *testing.T) {
		body := `{"timestamp":1760702400,"session_id":{"a":1},"status":"Approved","webhook_type":"status.updated"}`
		h := simpleHeaders("1760702400:{'a': 1}:Approved:status.updated", testSecret)
		_, err := webhook.Verify([]byte(body), h, testSecret, testNow, webhook.WithAllowSimple())
		wantReason(t, err, didit.WebhookReasonInvalidSignature)
	})
	t.Run("full signature wins", func(t *testing.T) {
		p := sessionPayload()
		body, h := signedBody(t, p, testSecret)
		h.Set("X-Signature-Simple", hmacHex(testSecret, []byte("1760702400:11111111-2222-3333-4444-555555555555:Approved:status.updated")))
		ev := mustVerify(t, body, h, testSecret, testNow, webhook.WithAllowSimple())
		if !ev.Authenticated() {
			t.Fatal("full signature matched but event is not authenticated")
		}
	})
	t.Run("tampered envelope", func(t *testing.T) {
		body := `{"timestamp":1760702400,"session_id":"abc","status":"Declined","webhook_type":"status.updated"}`
		h := simpleHeaders("1760702400:abc:Approved:status.updated", testSecret)
		_, err := webhook.Verify([]byte(body), h, testSecret, testNow, webhook.WithAllowSimple())
		wantReason(t, err, didit.WebhookReasonInvalidSignature)
	})
}

func TestSimpleParsedFloat64Fields(t *testing.T) {
	// Decoding without UseNumber turns integers into float64; they are
	// formatted as integers again, like Python would print the original int.
	payload := map[string]any{
		"timestamp":    float64(testTimestamp),
		"session_id":   float64(42),
		"status":       2.5,
		"webhook_type": "status.updated",
	}
	h := http.Header{}
	h.Set("X-Signature-Simple", hmacHex(testSecret, []byte("1760702400:42:2.5:status.updated")))
	if _, err := webhook.VerifyParsed(payload, h, testSecret, testNow, webhook.WithAllowSimple()); err != nil {
		t.Fatal(err)
	}
	payload["status"] = []any{"x"}
	_, err := webhook.VerifyParsed(payload, h, testSecret, testNow, webhook.WithAllowSimple())
	wantReason(t, err, didit.WebhookReasonInvalidSignature)
}

func TestVerifyParsed(t *testing.T) {
	p := with(sessionPayload(), "metadata", map[string]any{"big": json.Number("9007199254740993"), "f": 0.5})
	body, h := signedBody(t, p, testSecret)

	t.Run("UseNumber", func(t *testing.T) {
		ev, err := webhook.VerifyParsed(decodeUseNumber(t, body), h, testSecret, testNow)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := ev.(*webhook.StatusUpdatedEvent); !ok || !ev.Authenticated() {
			t.Fatalf("got %T authenticated=%v", ev, ev.Authenticated())
		}
		canonical, _ := webhook.Canonicalize(body)
		if string(ev.Raw()) != string(canonical) {
			t.Fatalf("Raw() = %s, want the canonical JSON", ev.Raw())
		}
	})
	t.Run("float64 loses big integers", func(t *testing.T) {
		var m map[string]any
		if err := json.Unmarshal(body, &m); err != nil {
			t.Fatal(err)
		}
		_, err := webhook.VerifyParsed(m, h, testSecret, testNow)
		wantReason(t, err, didit.WebhookReasonInvalidSignature)
	})
	t.Run("float64 without big integers", func(t *testing.T) {
		body, h := signedBody(t, sessionPayload(), testSecret)
		var m map[string]any
		if err := json.Unmarshal(body, &m); err != nil {
			t.Fatal(err)
		}
		ev, err := webhook.VerifyParsed(m, h, testSecret, testNow)
		if err != nil {
			t.Fatal(err)
		}
		if ev.Envelope().Timestamp != testTimestamp {
			t.Fatalf("Timestamp = %d", ev.Envelope().Timestamp)
		}
	})
	t.Run("Go integer types", func(t *testing.T) {
		m := map[string]any{
			"timestamp": testTimestamp, "webhook_type": "x",
			"a": 1, "b": int8(-2), "c": int16(3), "d": int32(-4), "e": uint(5), "f": uint8(6),
			"g": uint16(7), "h": uint32(8), "i": uint64(math.MaxUint64), "j": float32(0.5),
		}
		canonical := `{"a":1,"b":-2,"c":3,"d":-4,"e":5,"f":6,"g":7,"h":8,"i":18446744073709551615,"j":0.5,"timestamp":1760702400,"webhook_type":"x"}`
		h := http.Header{}
		h.Set("X-Signature-V2", hmacHex(testSecret, []byte(canonical)))
		ev, err := webhook.VerifyParsed(m, h, testSecret, testNow)
		if err != nil {
			t.Fatal(err)
		}
		if string(ev.Raw()) != canonical {
			t.Fatalf("Raw() = %s", ev.Raw())
		}
		for _, ts := range []any{int(testTimestamp), int32(0), uint32(0), uint64(testTimestamp), float32(0)} {
			m["timestamp"] = ts
			_, err := webhook.VerifyParsed(m, h, testSecret, testNow)
			var verr *didit.WebhookVerificationError
			if errors.As(err, &verr) && verr.Reason == didit.WebhookReasonMissingTimestamp {
				t.Fatalf("timestamp of type %T rejected: %v", ts, err)
			}
		}
	})
	t.Run("raw signature only", func(t *testing.T) {
		h := http.Header{}
		h.Set("X-Signature", hmacHex(testSecret, body))
		_, err := webhook.VerifyParsed(decodeUseNumber(t, body), h, testSecret, testNow)
		wantReason(t, err, didit.WebhookReasonInvalidSignature)
		if !strings.Contains(err.Error(), "use Verify") {
			t.Fatalf("error %q does not point to Verify", err)
		}
	})
	t.Run("not a parsed object", func(t *testing.T) {
		for _, payload := range []any{body, json.RawMessage(body), string(body), []any{}, nil, 42, struct{}{}} {
			_, err := webhook.VerifyParsed(payload, h, testSecret, testNow)
			wantReason(t, err, didit.WebhookReasonInvalidPayload)
		}
	})
	t.Run("unsupported values", func(t *testing.T) {
		cyclic := map[string]any{}
		cyclic["self"] = cyclic
		for name, v := range map[string]any{
			"NaN":          math.NaN(),
			"Inf":          math.Inf(-1),
			"bad number":   json.Number("1.2.3"),
			"overflow":     json.Number("1e999"),
			"struct":       struct{ A int }{1},
			"string slice": []string{"a"},
			"bad UTF-8":    "\xff",
			"cyclic":       cyclic,
		} {
			t.Run(name, func(t *testing.T) {
				_, err := webhook.VerifyParsed(with(sessionPayload(), "x", v), h, testSecret, testNow)
				wantReason(t, err, didit.WebhookReasonInvalidPayload)
			})
		}
		m := sessionPayload()
		m["\xff"] = 1
		_, err := webhook.VerifyParsed(m, h, testSecret, testNow)
		wantReason(t, err, didit.WebhookReasonInvalidPayload)
	})
}
