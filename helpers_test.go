package didit_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/didit-protocol/sdk-go"
	"github.com/didit-protocol/sdk-go/option"
)

// Test credentials. They are not secrets.
const (
	testAPIKey       = "test-api-key"
	testAccountToken = "test-account-token"
	testBaseURL      = "https://verification.test"
	testAccountURL   = "https://account.test/auth/v2"
)

// recordedRequest is one request seen by mockTransport.
type recordedRequest struct {
	Method string
	URL    string
	Path   string
	Query  map[string][]string
	Header http.Header
	Body   []byte
}

// response is a canned response of mockTransport.
type response struct {
	status      int
	contentType string
	body        string
	header      http.Header
	err         error
}

// mockTransport records requests and answers with canned responses, in order;
// the last one repeats. It never opens a socket.
type mockTransport struct {
	mu        sync.Mutex
	responses []response
	requests  []recordedRequest
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
		_ = req.Body.Close()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, recordedRequest{
		Method: req.Method,
		URL:    req.URL.String(),
		Path:   req.URL.EscapedPath(),
		Query:  req.URL.Query(),
		Header: req.Header.Clone(),
		Body:   body,
	})
	r := m.responses[len(m.responses)-1]
	if len(m.requests) <= len(m.responses) {
		r = m.responses[len(m.requests)-1]
	}
	if r.err != nil {
		return nil, r.err
	}
	h := http.Header{}
	for k, v := range r.header {
		h[k] = v
	}
	if r.contentType != "" {
		h.Set("Content-Type", r.contentType)
	}
	return &http.Response{
		StatusCode: r.status,
		Status:     strconv.Itoa(r.status) + " " + http.StatusText(r.status),
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(r.body)),
		Request:    req,
	}, nil
}

func (m *mockTransport) all() []recordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]recordedRequest(nil), m.requests...)
}

// single returns the only request sent.
func (m *mockTransport) single(t *testing.T) recordedRequest {
	t.Helper()
	reqs := m.all()
	if len(reqs) != 1 {
		t.Fatalf("sent %d requests, want 1", len(reqs))
	}
	return reqs[0]
}

// newMockClient returns a client whose requests go to a mockTransport that
// answers with status, content type and body.
func newMockClient(t *testing.T, status int, contentType, body string, opts ...option.RequestOption) (*didit.Client, *mockTransport) {
	t.Helper()
	return newMockClientWith(t, []response{{status: status, contentType: contentType, body: body}}, opts...)
}

func newMockClientWith(t *testing.T, responses []response, opts ...option.RequestOption) (*didit.Client, *mockTransport) {
	t.Helper()
	m := &mockTransport{responses: responses}
	base := []option.RequestOption{
		option.WithAPIKey(testAPIKey),
		option.WithAccountToken(testAccountToken),
		option.WithOrganizationID("org-test"),
		option.WithApplicationID("app-test"),
		option.WithBaseURL(testBaseURL),
		option.WithAccountBaseURL(testAccountURL),
		option.WithHTTPClient(&http.Client{Transport: m}),
		option.WithMaxRetries(0),
	}
	return didit.NewClient(append(base, opts...)...), m
}

func mustUnmarshal(t *testing.T, data string, dst any) {
	t.Helper()
	if err := json.Unmarshal([]byte(data), dst); err != nil {
		t.Fatalf("building params from the example: %v", err)
	}
}

func (r recordedRequest) expect(t *testing.T, method, path string) {
	t.Helper()
	if r.Method != method {
		t.Errorf("method = %s, want %s", r.Method, method)
	}
	if r.Path != path {
		t.Errorf("path = %s, want %s", r.Path, path)
	}
	if got := r.Header.Get("User-Agent"); !strings.HasPrefix(got, "didit-go/") {
		t.Errorf("User-Agent = %q", got)
	}
	if got := r.Header.Get("X-Didit-Client"); !strings.HasPrefix(got, "go/") {
		t.Errorf("X-Didit-Client = %q", got)
	}
	if got := r.Header.Get("Accept"); got == "" {
		t.Error("missing Accept header")
	}
}

func (r recordedRequest) expectAPIKey(t *testing.T) {
	t.Helper()
	if got := r.Header.Get("X-Api-Key"); got != testAPIKey {
		t.Errorf("x-api-key = %q, want the test key", got)
	}
	if !strings.HasPrefix(r.URL, testBaseURL+"/") {
		t.Errorf("API key sent to %s", r.URL)
	}
}

func (r recordedRequest) expectAccountToken(t *testing.T) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer "+testAccountToken {
		t.Errorf("Authorization = %q, want the account token", got)
	}
	if got := r.Header.Get("X-Api-Key"); got != "" {
		t.Error("the API key was sent to the account API")
	}
}

func (r recordedRequest) expectNoCredentials(t *testing.T) {
	t.Helper()
	if r.Header.Get("X-Api-Key") != "" || r.Header.Get("Authorization") != "" {
		t.Error("credentials sent to an unauthenticated operation")
	}
}

func (r recordedRequest) expectQuery(t *testing.T, want map[string]string) {
	t.Helper()
	for k, v := range want {
		if got := strings.Join(r.Query[k], ","); got != v {
			t.Errorf("query %s = %q, want %q", k, got, v)
		}
	}
}

func (r recordedRequest) expectHeader(t *testing.T, name, want string) {
	t.Helper()
	if got := r.Header.Get(name); got != want {
		t.Errorf("header %s = %q, want %q", name, got, want)
	}
}

// expectJSON compares the JSON body with want, ignoring key order and number
// formatting.
func (r recordedRequest) expectJSON(t *testing.T, want string) {
	t.Helper()
	if ct := r.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var got, exp any
	if err := json.Unmarshal(r.Body, &got); err != nil {
		t.Fatalf("body is not JSON: %v: %s", err, r.Body)
	}
	if err := json.Unmarshal([]byte(want), &exp); err != nil {
		t.Fatalf("bad expected JSON: %v", err)
	}
	if !reflect.DeepEqual(got, exp) {
		t.Errorf("body =\n%s\nwant\n%s", r.Body, want)
	}
}

// expectMultipart checks a multipart/form-data body: every field and every
// file in the lists is present.
func (r recordedRequest) expectMultipart(t *testing.T, fields, files []string) {
	t.Helper()
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		t.Fatalf("Content-Type = %q, want multipart/form-data", r.Header.Get("Content-Type"))
	}
	reader := multipart.NewReader(bytes.NewReader(r.Body), params["boundary"])
	gotFields := map[string]bool{}
	gotFiles := map[string]bool{}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("reading multipart body: %v", err)
		}
		data, _ := io.ReadAll(part)
		if part.FileName() != "" {
			gotFiles[part.FormName()] = true
			if string(data) != "file-content" {
				t.Errorf("file %s content = %q", part.FormName(), data)
			}
			if part.Header.Get("Content-Type") != "image/png" {
				t.Errorf("file %s Content-Type = %q", part.FormName(), part.Header.Get("Content-Type"))
			}
		} else {
			gotFields[part.FormName()] = true
		}
	}
	for _, f := range fields {
		if !gotFields[f] {
			t.Errorf("multipart field %s missing (got %v)", f, keys(gotFields))
		}
	}
	for _, f := range files {
		if !gotFiles[f] {
			t.Errorf("multipart file %s missing (got %v)", f, keys(gotFiles))
		}
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// expectFields checks that each key was decoded into its Go field.
func expectFields(t *testing.T, j didit.ResponseJSON, fields []string) {
	t.Helper()
	for _, f := range fields {
		if !j.Present(f) {
			t.Errorf("response field %s not present", f)
		}
		if j.Invalid(f) {
			raw, _ := j.Field(f)
			t.Errorf("response field %s did not decode: %s", f, raw)
		}
	}
}
