package didit

import (
	"bytes"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"", 0, false},
		{"2", 2 * time.Second, true},
		{"0.5", 500 * time.Millisecond, true},
		{"0", 0, true},
		{"-1", 0, false},
		{"NaN", 0, false},
		{"3600", 60 * time.Second, true},
		{now.Add(10 * time.Second).Format(http.TimeFormat), 10 * time.Second, true},
		{now.Add(-10 * time.Second).Format(http.TimeFormat), 0, true},
		{now.Add(time.Hour).Format(http.TimeFormat), 60 * time.Second, true},
		{"soon", 0, false},
	}
	for _, c := range cases {
		got, ok := parseRetryAfter(c.in, now)
		if got != c.want || ok != c.ok {
			t.Errorf("parseRetryAfter(%q) = %v, %v; want %v, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestRetryDelayBounds(t *testing.T) {
	for attempt := 0; attempt < 10; attempt++ {
		limit := maxRetryDelay
		if attempt < 5 {
			limit = min(initialRetryDelay<<attempt, maxRetryDelay)
		}
		for i := 0; i < 200; i++ {
			if d := retryDelay(attempt, nil); d < 0 || d > limit {
				t.Fatalf("attempt %d: delay %v outside [0, %v]", attempt, d, limit)
			}
		}
	}
	resp := &http.Response{Header: http.Header{"Retry-After": {"3"}}}
	if d := retryDelay(0, resp); d != 3*time.Second {
		t.Fatalf("Retry-After ignored: %v", d)
	}
}

func TestSameOrigin(t *testing.T) {
	parse := func(s string) *url.URL { u, _ := url.Parse(s); return u }
	same := [][2]string{
		{"https://a.test/x", "https://A.TEST:443/y"},
		{"http://a.test", "http://a.test:80/"},
	}
	for _, p := range same {
		if !sameOrigin(parse(p[0]), parse(p[1])) {
			t.Errorf("%s and %s should be the same origin", p[0], p[1])
		}
	}
	different := [][2]string{
		{"https://a.test", "http://a.test"},
		{"https://a.test", "https://b.test"},
		{"https://a.test", "https://a.test:8443"},
		{"https://a.test", "https://sub.a.test"},
	}
	for _, p := range different {
		if sameOrigin(parse(p[0]), parse(p[1])) {
			t.Errorf("%s and %s should differ", p[0], p[1])
		}
	}
	if canonicalHost(parse("ftp://a.test")) != "a.test:" {
		t.Error("unknown scheme port")
	}
}

func TestRedactURL(t *testing.T) {
	u, _ := url.Parse("https://user:pw@a.test/p%2Fq?key=secret#f")
	if got := redactURL(u); got != "https://a.test/p%2Fq" {
		t.Fatalf("got %s", got)
	}
	if redactURL(nil) != "" {
		t.Fatal("nil")
	}
}

type formParams struct {
	S    string            `json:"s"`
	N    Opt[int64]        `json:"n"`
	U    uint16            `json:"u"`
	F    float64           `json:"f"`
	T    Opt[time.Time]    `json:"t"`
	L    []string          `json:"l"`
	Objs []map[string]any  `json:"objs"`
	M    map[string]string `json:"m"`
	Nil  map[string]string `json:"nil"`
	Any  any               `json:"any"`
	Omit Opt[string]       `json:"omit"`
	Doc  Opt[File]         `json:"-" form:"doc"`
	None Opt[File]         `json:"-" form:"none"`
	Q    Opt[[]string]     `json:"-" query:"q"`
	QT   time.Time         `json:"-" query:"qt"`
	QB   bool              `json:"-" query:"qb"`
	QX   Opt[any]          `json:"-" query:"qx"`
	H    Opt[string]       `json:"-" header:"X-H"`
	HE   string            `json:"-" header:"X-Empty"`
}

func TestEncodeMultipartKinds(t *testing.T) {
	p := formParams{
		S: "s", N: Int(3), U: 7, F: 2.5, T: Time(time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)),
		L: []string{"a", "b"}, Objs: []map[string]any{{"k": 1}}, M: map[string]string{"x": "y"},
		Doc: Some(FileFromBytes(`we"ird.png`, []byte("img"))),
	}
	body, ct, err := encodeMultipart(p)
	if err != nil {
		t.Fatal(err)
	}
	_, params, _ := mime.ParseMediaType(ct)
	r := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	got := map[string][]string{}
	for {
		part, err := r.NextPart()
		if err != nil {
			break
		}
		var b bytes.Buffer
		_, _ = b.ReadFrom(part)
		got[part.FormName()] = append(got[part.FormName()], b.String())
	}
	want := map[string][]string{
		"s": {"s"}, "n": {"3"}, "u": {"7"}, "f": {"2.5"}, "t": {"2025-01-02T03:04:05Z"},
		"l": {"a", "b"}, "objs": {`{"k":1}`}, "m": {`{"x":"y"}`}, "doc": {"img"},
	}
	for k, v := range want {
		if strings.Join(got[k], "|") != strings.Join(v, "|") {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	for _, k := range []string{"nil", "any", "omit", "none"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s should not be sent", k)
		}
	}
	if !strings.Contains(string(body), `filename="we\"ird.png"`) {
		t.Error("file name not escaped")
	}
	if !hasFile(p) || hasFile(formParams{}) || hasFile(nil) {
		t.Error("hasFile")
	}
	if _, _, err := encodeMultipart(formParams{Doc: Some(FileFromPath("/does/not/exist"))}); err == nil {
		t.Error("missing file accepted")
	}
	if _, _, err := encodeMultipart(formParams{Doc: Some(File{name: "empty"})}); err != nil {
		t.Errorf("a zero File should be skipped: %v", err)
	}
}

func TestEncodeQueryAndHeaders(t *testing.T) {
	p := &formParams{
		Q: Some([]string{"a", "b"}), QT: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC), QB: true,
		QX: Some[any](map[string]any{"no": "objects"}), H: String("h"),
	}
	q := url.Values{}
	encodeQuery(p, q)
	if strings.Join(q["q"], ",") != "a,b" || q.Get("qt") != "2025-01-02T00:00:00Z" || q.Get("qb") != "true" {
		t.Errorf("query %v", q)
	}
	if _, ok := q["qx"]; ok {
		t.Error("objects are not query values")
	}
	h := http.Header{}
	encodeHeaders(p, h)
	if h.Get("X-H") != "h" || h.Get("X-Empty") != "" {
		t.Errorf("headers %v", h)
	}
	encodeQuery(nil, q)
	encodeHeaders((*formParams)(nil), h)
	encodeQuery(42, q)
}

func TestFileLoadErrors(t *testing.T) {
	if _, _, err := (File{name: "x"}).load(); err == nil {
		t.Error("empty file loaded")
	}
	if _, _, err := FileFromReader(errReader{}, "x").load(); err == nil {
		t.Error("reader error ignored")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, http.ErrBodyReadAfterClose }

func TestRequestIDHeaders(t *testing.T) {
	if got := requestIDOf(http.Header{"X-Correlation-Id": {"c1"}}); got != "c1" {
		t.Errorf("got %q", got)
	}
	if got := requestIDOf(http.Header{"Request-Id": {"r1"}, "X-Request-Id": {"x1"}}); got != "x1" {
		t.Errorf("X-Request-Id should win, got %q", got)
	}
	if requestIDOf(http.Header{}) != "" {
		t.Error("empty")
	}
}
