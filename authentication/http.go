package authentication

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"

	didit "github.com/didit-protocol/sdk-go"
	"github.com/didit-protocol/sdk-go/internal/version"
)

var (
	userAgent = "didit-go/" + version.Version +
		" (Go " + strings.TrimPrefix(runtime.Version(), "go") + "; " + runtime.GOOS + "/" + runtime.GOARCH + ")"
	clientHeader = "go/" + version.Version
)

// maxResponseBytes bounds every response body the package reads.
const maxResponseBytes = 1 << 20

const (
	contentTypeJSON = "application/json"
	contentTypeForm = "application/x-www-form-urlencoded"
)

type request struct {
	method      string
	url         string
	bearer      string // sent as Authorization: Bearer <bearer>
	contentType string
	body        []byte
	// retry allows one retry after a connection error or a 5xx answer. Only
	// requests that are safe to repeat set it.
	retry bool
}

type response struct {
	header http.Header
	body   []byte
	// http is the response, its body already read into body. Errors carry a
	// redacted copy of it.
	http *http.Response
}

// send performs the request and maps failures to the module's error types:
// *didit.APIConnectionError (or *didit.APITimeoutError) without a response,
// didit.NewAPIStatusError for a status outside 2xx, and
// *didit.UnexpectedResponseError for a 2xx body over the size limit.
func (c *Client) send(ctx context.Context, r request) (*response, error) {
	for attempt := 0; ; attempt++ {
		resp, err := c.sendOnce(ctx, r)
		if err == nil || !r.retry || attempt > 0 || !retryable(ctx, err) {
			return resp, err
		}
		if waitErr := c.backoff(ctx); waitErr != nil {
			return nil, connectionError(r.method, r.url, waitErr)
		}
	}
}

func (c *Client) sendOnce(ctx context.Context, r request) (*response, error) {
	var body io.Reader
	if r.body != nil {
		body = bytes.NewReader(r.body)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, r.url, body)
	if err != nil {
		return nil, connectionError(r.method, r.url, err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-Didit-Client", clientHeader)
	req.Header.Set("Accept", contentTypeJSON)
	if r.contentType != "" {
		req.Header.Set("Content-Type", r.contentType)
	}
	if r.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+r.bearer)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, connectionError(r.method, r.url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, connectionError(r.method, r.url, err)
	}
	ok := resp.StatusCode >= 200 && resp.StatusCode <= 299
	if len(data) > maxResponseBytes {
		if ok {
			return nil, unexpectedResponse(r, resp, "is larger than 1 MiB", nil)
		}
		data = data[:maxResponseBytes]
	}
	if !ok {
		return nil, didit.NewAPIStatusError(&didit.APIStatusError{
			StatusCode: resp.StatusCode,
			Body:       parseBody(data),
			RawBody:    data,
			Header:     resp.Header,
			RequestID:  resp.Header.Get("X-Request-Id"),
			Method:     r.method,
			URL:        redactURL(r.url),
			Response:   redactedResponse(resp),
		})
	}
	return &response{header: resp.Header, body: data, http: resp}, nil
}

// retryable reports whether a failed GET may be sent once more: the
// connection failed (and the caller's context is still live) or the issuer
// answered 5xx.
func retryable(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	var status *didit.APIStatusError
	if errors.As(err, &status) {
		return status.StatusCode >= 500
	}
	var conn *didit.APIConnectionError
	return errors.As(err, &conn)
}

// backoff waits a random delay between half and all of retryDelay.
func (c *Client) backoff(ctx context.Context) error {
	half := c.retryDelay / 2
	timer := time.NewTimer(half + rand.N(half+1))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// decodeJSON decodes a 2xx response body. A body that does not decode is a
// *didit.UnexpectedResponseError.
func decodeJSON(r request, resp *response, v any) error {
	if err := json.Unmarshal(resp.body, v); err != nil {
		return unexpectedResponse(r, resp.http, "is not valid JSON", err)
	}
	return nil
}

// unexpectedResponse reports a 2xx response the package cannot use, with a
// redacted copy of the response.
func unexpectedResponse(r request, resp *http.Response, problem string, cause error) error {
	return &didit.UnexpectedResponseError{
		Message:  fmt.Sprintf("%s %s: the %d response %s", r.method, redactURL(r.url), resp.StatusCode, problem),
		Cause:    cause,
		Response: redactedResponse(resp),
	}
}

func connectionError(method, rawURL string, cause error) error {
	e := &didit.APIConnectionError{Method: method, URL: redactURL(rawURL), Cause: cause}
	if isTimeout(cause) {
		return &didit.APITimeoutError{APIConnectionError: e}
	}
	return e
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// parseBody returns an error body as parsed JSON when possible, otherwise as
// a string, or nil when empty.
func parseBody(data []byte) any {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(data, &v); err == nil {
		return v
	}
	return string(data)
}

// redactURL drops credentials, query and fragment from a URL for errors.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.User, u.RawQuery, u.ForceQuery, u.Fragment, u.RawFragment = nil, "", false, "", ""
	return u.String()
}

// redactedResponse copies a response for an error value, so that the error
// does not carry the access token (request header) or the request body (codes
// and refresh tokens) through resp.Request. After a redirect,
// resp.Request.Response is the redirect response, whose Request is the
// original request; it is dropped too.
func redactedResponse(resp *http.Response) *http.Response {
	copied := *resp
	copied.Body = http.NoBody
	if resp.Request != nil {
		req := resp.Request.WithContext(context.Background())
		req.Header = resp.Request.Header.Clone()
		if req.Header.Get("Authorization") != "" {
			req.Header.Set("Authorization", "Bearer [redacted]")
		}
		req.Body, req.GetBody = http.NoBody, nil
		req.Response = nil
		copied.Request = req
	}
	return &copied
}

// withRedirectPolicy returns a copy of hc (or a default client) that never
// follows a redirect to another origin: the 3xx response is returned instead,
// so an access token, code or refresh token never leaves the issuer's origin.
func withRedirectPolicy(hc *http.Client) *http.Client {
	copied := http.Client{Timeout: DefaultTimeout}
	if hc != nil {
		copied = *hc
	}
	next := copied.CheckRedirect
	copied.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 || !sameOrigin(req.URL, via[0].URL) {
			return http.ErrUseLastResponse
		}
		if next != nil {
			return next(req, via)
		}
		return nil
	}
	return &copied
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) &&
		strings.EqualFold(a.Hostname(), b.Hostname()) &&
		effectivePort(a) == effectivePort(b)
}

func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	return "80"
}

// checkBearer validates an access token argument without echoing it.
func checkBearer(token string) error {
	if token == "" {
		return configError("access token is required")
	}
	for i := 0; i < len(token); i++ {
		if token[i] <= ' ' || token[i] >= 0x7f {
			return configError("access token contains invalid characters")
		}
	}
	return nil
}
