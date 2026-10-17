package webhook_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	didit "github.com/didit-protocol/sdk-go"
	"github.com/didit-protocol/sdk-go/webhook"
)

// pythonFloats lists float literals with the canonical form Python produces,
// json.dumps(shorten_floats(float(literal))), taken from CPython 3.11.
var pythonFloats = []struct{ in, want string }{
	{"0.1", "0.1"},
	{"1e-05", "1e-05"},
	{"0.0001", "0.0001"},
	{"1e+16", "10000000000000000"},
	{"1e16", "10000000000000000"},
	{"1.7976931348623157e+308", "179769313486231570814527423731704356798070567525844996598917476803157260780028538760589558632766878171540458953514382464234321326889464182768467546703537516986049910576551282076245490090389328944075868508455133942304583236903222948165808559332123348274797826204144723168738177180919299881250404026184124858368"},
	{"5e-324", "5e-324"},
	{"4.9e-324", "5e-324"},
	{"123456789.123456789", "123456789.12345679"},
	{"1e+22", "10000000000000000000000"},
	{"0.30000000000000004", "0.30000000000000004"},
	{"-2.5e-07", "-2.5e-07"},
	{"1000000000000000.0", "1000000000000000"},
	{"9999999999999998.0", "9999999999999998"},
	{"1.5e-07", "1.5e-07"},
	{"1.2345e+20", "123450000000000000000"},
	{"0.5", "0.5"},
	{"-0.0", "0"},
	{"0.0", "0"},
	{"87.0", "87"},
	{"87.000", "87"},
	{"1.5e+300", "1500000000000000078757140382806630373056702871662238732373781173267703686983362293679557062620671796065556665749325817265413784853040645863467188277180060474272580801389863705606745350692182135089053429852456199917621678558451461320111979114170213741868888183230085264257173504208294580298189100810240"},
	{"2.2250738585072014e-308", "2.2250738585072014e-308"},
	{"0.3333333333333333", "0.3333333333333333"},
	{"0.6666666666666666", "0.6666666666666666"},
	{"100.0", "100"},
	{"0.00012345", "0.00012345"},
	{"1.0000000000000002", "1.0000000000000002"},
	{"4.35", "4.35"},
	{"123456789012345.6", "123456789012345.6"},
	{"1234567890123456.7", "1234567890123456.8"},
	{"12345678901234567.0", "12345678901234568"},
	{"-1E-5", "-1e-05"},
	{"2.5", "2.5"},
	{"1e+100", "10000000000000000159028911097599180468360808563945281389781327557747838772170381060813469985856815104"},
	{"3.141592653589793", "3.141592653589793"},
	{"5e-05", "5e-05"},
	{"9.999999999999999e-05", "9.999999999999999e-05"},
	{"1e-07", "1e-07"},
	{"1E+21", "1000000000000000000000"},
	{"299792458.0", "299792458"},
	{"1e-400", "0"},
	{"0.1e1", "1"},
	{"10E-1", "1"},
	{"-1.5e300", "-1500000000000000078757140382806630373056702871662238732373781173267703686983362293679557062620671796065556665749325817265413784853040645863467188277180060474272580801389863705606745350692182135089053429852456199917621678558451461320111979114170213741868888183230085264257173504208294580298189100810240"},
}

func TestCanonicalizeFloatsMatchPython(t *testing.T) {
	for _, c := range pythonFloats {
		got, err := webhook.Canonicalize([]byte(c.in))
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if string(got) != c.want {
			t.Errorf("%s: got %s, want %s", c.in, got, c.want)
		}
		// Inside a structure too.
		got, err = webhook.Canonicalize([]byte(`{"x":[` + c.in + `]}`))
		if err != nil || string(got) != `{"x":[`+c.want+`]}` {
			t.Errorf("%s in an object: got %s, %v", c.in, got, err)
		}
	}
}

func TestCanonicalizeIntegers(t *testing.T) {
	cases := map[string]string{
		"0":                         "0",
		"-0":                        "0",
		"42":                        "42",
		"-17":                       "-17",
		"9007199254740993":          "9007199254740993",
		"-9223372036854775809":      "-9223372036854775809",
		"123456789012345678901234567890123456789": "123456789012345678901234567890123456789",
	}
	for in, want := range cases {
		got, err := webhook.Canonicalize([]byte(in))
		if err != nil || string(got) != want {
			t.Errorf("%s: got %s, %v; want %s", in, got, err, want)
		}
	}
}

func TestCanonicalizeStrings(t *testing.T) {
	cases := []struct{ in, want string }{
		{`"plain"`, `"plain"`},
		{`"a\/b"`, `"a/b"`},
		{`"say \"hi\""`, `"say \"hi\""`},
		{`"C:\\path"`, `"C:\\path"`},
		{`"\n\r\t\b\f"`, `"\n\r\t\b\f"`},
		{`"\u0000\u0001\u001f\u000b"`, `"\u0000\u0001\u001f\u000b"`},
		{`"\u007f\u0080"`, "\"\u007f\u0080\""},
		{`"\u2028\u2029"`, "\"\u2028\u2029\""},
		{`"\u00e9\ud83d\ude00"`, `"é😀"`},
		{`"<script>&amp;</script>"`, `"<script>&amp;</script>"`},
		{"\"raw é 😀\"", "\"raw é 😀\""},
		{`"\ud83d"`, "\"\ufffd\""}, // lone surrogate: replaced, as Go strings are UTF-8
	}
	for _, c := range cases {
		got, err := webhook.Canonicalize([]byte(c.in))
		if err != nil || string(got) != c.want {
			t.Errorf("%s: got %s, %v; want %s", c.in, got, err, c.want)
		}
	}
}

func TestCanonicalizeStructure(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"whitespace", " {\n \"b\" : [ 1 , 2 ] ,\t\"a\" : { } }\r\n", `{"a":{},"b":[1,2]}`},
		{"code point order", `{"b":1,"a":2,"B":3,"\u00e9":4,"\ufb00":5,"\ud83d\ude00":6,"10":7,"9":8,"_":9,"":10}`,
			`{"":10,"10":7,"9":8,"B":3,"_":9,"a":2,"b":1,"é":4,"ﬀ":5,"😀":6}`},
		{"duplicate keys, last wins", `{"a":1,"a":2}`, `{"a":2}`},
		{"literals", `[true,false,null]`, `[true,false,null]`},
		{"nested", `{"z":{"y":[{"b":1.0,"a":[]}]}}`, `{"z":{"y":[{"a":[],"b":1}]}}`},
		{"empty containers", `[{},[],""]`, `[{},[],""]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := webhook.Canonicalize([]byte(c.in))
			if err != nil || string(got) != c.want {
				t.Fatalf("got %s, %v; want %s", got, err, c.want)
			}
		})
	}
}

func TestCanonicalizeErrors(t *testing.T) {
	for _, in := range []string{"", "{", "[1,]", "NaN", "-Infinity", "1e400", `{"a":-1e999}`, "\xff", "{} {}", "01", `"\x"`} {
		_, err := webhook.Canonicalize([]byte(in))
		wantReason(t, err, didit.WebhookReasonInvalidPayload)
	}
	deep := strings.Repeat("[", 10001) + strings.Repeat("]", 10001)
	_, err := webhook.Canonicalize([]byte(deep))
	wantReason(t, err, didit.WebhookReasonInvalidPayload)
}

func FuzzCanonicalize(f *testing.F) {
	if raw, err := os.ReadFile("../openapi/webhook-vectors.json"); err == nil {
		var vf vectorFile
		if json.Unmarshal(raw, &vf) == nil {
			for _, v := range vf.Vectors {
				f.Add([]byte(v.Body))
			}
		}
	}
	for _, c := range pythonFloats {
		f.Add([]byte(c.in))
	}
	f.Add([]byte(`{"a":"\u0000\ud83d\ude00\u2028","b":[1e-7,-0,1.0,{}]}`))
	h := http.Header{}
	h.Set("X-Signature", strings.Repeat("a", 64))
	h.Set("X-Signature-V2", strings.Repeat("b", 64))
	h.Set("X-Signature-Simple", strings.Repeat("c", 64))

	f.Fuzz(func(t *testing.T, data []byte) {
		// Verify never panics, whatever the body.
		_, _ = webhook.Verify(data, h, testSecret, webhook.WithAllowSimple())

		out, err := webhook.Canonicalize(data)
		if err != nil {
			return
		}
		if !json.Valid(out) {
			t.Fatalf("canonical output is not valid JSON: %q", out)
		}
		again, err := webhook.Canonicalize(out)
		if err != nil {
			t.Fatalf("canonical output does not canonicalize: %v", err)
		}
		if !bytes.Equal(out, again) {
			t.Fatalf("not idempotent:\n%q\n%q", out, again)
		}
	})
}
