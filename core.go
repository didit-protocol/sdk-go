package didit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/didit-protocol/sdk-go/internal/requestconfig"
	"github.com/didit-protocol/sdk-go/internal/version"
	"github.com/didit-protocol/sdk-go/option"
)

// Retry policy constants (docs/DESIGN.md section 7).
const (
	initialRetryDelay = 500 * time.Millisecond
	maxRetryDelay     = 8 * time.Second
	maxRetryAfter     = 60 * time.Second
	maxRedirects      = 10
)

type apiKind uint8

const (
	apiVerification apiKind = iota
	apiAccount
)

type authKind uint8

const (
	authAPIKey authKind = iota
	authAccountToken
	authNone
)

type bodyKind uint8

const (
	bodyNone bodyKind = iota
	bodyJSON
	bodyMultipart
	// bodyJSONOrMultipart is multipart when a file is set, JSON otherwise.
	bodyJSONOrMultipart
)

// apiRequest describes one operation call. The generated methods build it.
type apiRequest struct {
	method string
	// path is the path template, such as "/v3/session/{sessionId}/decision/".
	path string
	// pathParams holds name, value pairs for the template.
	pathParams []string
	api        apiKind
	auth       authKind
	// appScope fills {organization_id} and {application_id} from the options.
	appScope bool
	params   any
	body     bodyKind
	// binary marks a file response: the body is returned unread.
	binary bool
	accept string

	// rawURL, when set, replaces path and query (next page links).
	rawURL *url.URL
}

var userAgent = fmt.Sprintf("didit-go/%s (Go %s; %s/%s)",
	version.Version, strings.TrimPrefix(runtime.Version(), "go"), runtime.GOOS, runtime.GOARCH)

var defaultHTTPClient = &http.Client{Transport: http.DefaultTransport}

func (c *Client) config(opts []option.RequestOption) (*requestconfig.Config, error) {
	cfg := requestconfig.New()
	if err := cfg.Apply(c.options...); err != nil {
		return nil, &ConfigurationError{Message: err.Error()}
	}
	if err := cfg.Apply(opts...); err != nil {
		return nil, &ConfigurationError{Message: err.Error()}
	}
	return cfg, nil
}

// do runs r and decodes a 2xx JSON response into dst (when not nil). It
// returns the final HTTP response; for binary requests its body is unread and
// the caller must close it.
func (c *Client) do(ctx context.Context, r *apiRequest, dst any, opts []option.RequestOption) (*http.Response, error) {
	cfg, err := c.config(opts)
	if err != nil {
		return nil, err
	}
	base := cfg.BaseURL
	if r.api == apiAccount {
		base = cfg.AccountBaseURL
	}
	u, err := buildURL(cfg, base, r)
	if err != nil {
		return nil, err
	}

	header := http.Header{}
	header.Set("User-Agent", userAgent)
	header.Set("X-Didit-Client", "go/"+version.Version)
	accept := r.accept
	if accept == "" {
		accept = "application/json"
	}
	header.Set("Accept", accept)
	switch r.auth {
	case authAPIKey:
		if cfg.APIKey == "" {
			return nil, &ConfigurationError{Message: "missing API key: pass option.WithAPIKey or set DIDIT_API_KEY"}
		}
		header.Set("X-Api-Key", cfg.APIKey)
	case authAccountToken:
		if cfg.AccountToken == "" {
			return nil, &ConfigurationError{Message: "missing account token: pass option.WithAccountToken with the access token from client.Account.Login"}
		}
		header.Set("Authorization", "Bearer "+cfg.AccountToken)
	}

	var body []byte
	if r.params != nil {
		encodeHeaders(r.params, header)
		kind := r.body
		if kind == bodyJSONOrMultipart {
			kind = bodyJSON
			if hasFile(r.params) {
				kind = bodyMultipart
			}
		}
		switch kind {
		case bodyJSON:
			if body, err = encodeJSONBody(r.params); err != nil {
				return nil, &ConfigurationError{Message: "encoding the request body: " + err.Error()}
			}
			header.Set("Content-Type", "application/json")
		case bodyMultipart:
			var contentType string
			if body, contentType, err = encodeMultipart(r.params); err != nil {
				return nil, &ConfigurationError{Message: "encoding the request body: " + err.Error()}
			}
			header.Set("Content-Type", contentType)
		}
	}
	for name, values := range cfg.Header {
		if len(values) == 0 || (len(values) == 1 && values[0] == "") {
			header.Del(name)
			continue
		}
		header[name] = values
	}
	return c.send(ctx, cfg, r, u, header, body, dst)
}

// buildURL expands the path template and adds the query.
func buildURL(cfg *requestconfig.Config, base *url.URL, r *apiRequest) (*url.URL, error) {
	if r.rawURL != nil {
		return r.rawURL, nil
	}
	values := map[string]string{}
	if r.appScope {
		if cfg.OrganizationID == "" || cfg.ApplicationID == "" {
			return nil, &ConfigurationError{Message: "this operation is scoped to an application: pass option.WithOrganizationID and option.WithApplicationID or set DIDIT_ORGANIZATION_ID and DIDIT_APPLICATION_ID"}
		}
		values["organization_id"] = cfg.OrganizationID
		values["application_id"] = cfg.ApplicationID
	}
	for i := 0; i+1 < len(r.pathParams); i += 2 {
		values[r.pathParams[i]] = r.pathParams[i+1]
	}
	path, rawPath, err := expandPath(r.path, values)
	if err != nil {
		return nil, err
	}
	u := *base
	u.Path = base.Path + path
	u.RawPath = base.EscapedPath() + rawPath
	q := url.Values{}
	if r.params != nil {
		encodeQuery(r.params, q)
	}
	for name, vs := range cfg.Query {
		q[name] = vs
	}
	u.RawQuery = q.Encode()
	return &u, nil
}

// expandPath fills the {name} placeholders of template and returns the path
// and its escaped form. Values that are empty or would change the path ("."
// and "..") are refused; "/" and other reserved characters are escaped.
func expandPath(template string, values map[string]string) (path, rawPath string, err error) {
	var p, raw strings.Builder
	rest := template
	for {
		open := strings.IndexByte(rest, '{')
		if open < 0 {
			break
		}
		closing := strings.IndexByte(rest[open:], '}')
		if closing < 0 {
			break
		}
		literal := rest[:open]
		name := rest[open+1 : open+closing]
		rest = rest[open+closing+1:]
		value := values[name]
		if value == "" {
			return "", "", &ConfigurationError{Message: fmt.Sprintf("missing path parameter %s", name)}
		}
		if value == "." || value == ".." {
			return "", "", &ConfigurationError{Message: fmt.Sprintf("invalid path parameter %s: %q", name, value)}
		}
		p.WriteString(literal)
		raw.WriteString(literal)
		p.WriteString(value)
		raw.WriteString(url.PathEscape(value))
	}
	p.WriteString(rest)
	raw.WriteString(rest)
	return p.String(), raw.String(), nil
}

func idempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodOptions:
		return true
	}
	return false
}

// send runs the attempts of one request.
func (c *Client) send(ctx context.Context, cfg *requestconfig.Config, r *apiRequest, u *url.URL, header http.Header, body []byte, dst any) (*http.Response, error) {
	hc := pinnedClient(cfg.HTTPClient)
	sleep := cfg.Sleep
	if sleep == nil {
		sleep = time.After
	}
	for attempt := 0; ; attempt++ {
		resp, retryable, err := c.attempt(ctx, cfg, hc, r, u, header, body, dst, attempt)
		if err == nil {
			if cfg.ResponseInto != nil {
				*cfg.ResponseInto = resp
			}
			return resp, nil
		}
		if attempt >= cfg.MaxRetries || !retryable {
			if cfg.ResponseInto != nil && resp != nil {
				*cfg.ResponseInto = resp
			}
			return resp, err
		}
		delay := retryDelay(attempt, resp)
		select {
		case <-ctx.Done():
			return resp, connectionError(r.method, u, ctx.Err(), true)
		case <-sleep(delay):
		}
	}
}

func (c *Client) attempt(ctx context.Context, cfg *requestconfig.Config, hc *http.Client, r *apiRequest, u *url.URL, header http.Header, body []byte, dst any, attempt int) (*http.Response, bool, error) {
	actx, cancel := ctx, context.CancelFunc(func() {})
	if cfg.Timeout > 0 {
		actx, cancel = context.WithTimeout(ctx, cfg.Timeout)
	}
	var wroteHeaders atomic.Bool
	actx = httptrace.WithClientTrace(actx, &httptrace.ClientTrace{
		WroteHeaders: func() { wroteHeaders.Store(true) },
	})
	req, err := http.NewRequestWithContext(actx, r.method, u.String(), nil)
	if err != nil {
		cancel()
		return nil, false, &ConfigurationError{Message: "building the request: " + err.Error()}
	}
	req.Header = header.Clone()
	if body != nil {
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))
		req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	}
	start := time.Now()
	resp, err := hc.Do(req)
	if err != nil {
		cancel()
		callerDone := ctx.Err() != nil
		retryable := !callerDone && (idempotent(r.method) || !wroteHeaders.Load())
		cerr := connectionError(r.method, u, err, callerDone && errors.Is(ctx.Err(), context.DeadlineExceeded))
		logAttempt(ctx, cfg, r.method, u, 0, "", time.Since(start), attempt, cerr)
		return nil, retryable, cerr
	}
	if resp.Request == nil {
		resp.Request = req
	}
	requestID := requestIDOf(resp.Header)
	if r.binary && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		resp.Body = &cancelOnClose{ReadCloser: resp.Body, cancel: cancel}
		logAttempt(ctx, cfg, r.method, u, resp.StatusCode, requestID, time.Since(start), attempt, nil)
		return resp, false, nil
	}
	data, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	cancel()
	if err != nil {
		// The server answered, so the request reached it.
		callerDone := ctx.Err() != nil
		cerr := connectionError(r.method, u, err, callerDone && errors.Is(ctx.Err(), context.DeadlineExceeded))
		logAttempt(ctx, cfg, r.method, u, resp.StatusCode, requestID, time.Since(start), attempt, cerr)
		return nil, !callerDone && idempotent(r.method), cerr
	}
	resp.Body = io.NopCloser(bytes.NewReader(data))
	logAttempt(ctx, cfg, r.method, u, resp.StatusCode, requestID, time.Since(start), attempt, nil)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if dst != nil && len(bytes.TrimSpace(data)) > 0 {
			if err := json.Unmarshal(data, dst); err != nil {
				return resp, false, &UnexpectedResponseError{
					Message:  fmt.Sprintf("%s %s: the %d response is not valid JSON", r.method, redactURL(u), resp.StatusCode),
					Cause:    err,
					Response: errorResponse(resp, redactKey(cfg, data)),
				}
			}
		}
		return resp, false, nil
	}
	data = redactKey(cfg, data)
	statusErr := &APIStatusError{
		StatusCode: resp.StatusCode,
		Body:       parseErrorBody(data),
		RawBody:    data,
		Header:     resp.Header,
		RequestID:  requestID,
		Method:     r.method,
		URL:        redactURL(u),
		Response:   errorResponse(resp, data),
	}
	status := resp.StatusCode
	retryable := status == http.StatusTooManyRequests
	if idempotent(r.method) {
		retryable = retryable || status == http.StatusRequestTimeout || (status >= 500 && status <= 599)
	}
	return resp, retryable, NewAPIStatusError(statusErr)
}

// redactKey replaces the API key in a response body that echoes it.
func redactKey(cfg *requestconfig.Config, data []byte) []byte {
	if cfg.APIKey == "" || !bytes.Contains(data, []byte(cfg.APIKey)) {
		return data
	}
	return bytes.ReplaceAll(data, []byte(cfg.APIKey), []byte("[redacted]"))
}

// errorResponse copies resp for an error value. Errors get logged, and the
// API key never appears in one (docs/DESIGN.md section 6): the copy's body
// reads data, its request's X-Api-Key is redacted, and the response of a
// redirect, which leads back to the original request, is dropped.
func errorResponse(resp *http.Response, data []byte) *http.Response {
	copied := *resp
	copied.Body = io.NopCloser(bytes.NewReader(data))
	if resp.Request != nil {
		req := resp.Request.Clone(context.Background())
		if req.Header.Get("X-Api-Key") != "" {
			req.Header.Set("X-Api-Key", "[redacted]")
		}
		req.Response = nil
		copied.Request = req
	}
	return &copied
}

// requestIDHeaders are the response headers that may carry the request id,
// in order (the same list as the other Didit SDKs).
var requestIDHeaders = []string{"X-Request-Id", "Request-Id", "X-Amzn-Requestid", "X-Correlation-Id"}

func requestIDOf(h http.Header) string {
	for _, name := range requestIDHeaders {
		if v := h.Get(name); v != "" {
			return v
		}
	}
	return ""
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

func parseErrorBody(data []byte) any {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(trimmed, &v); err == nil {
		return v
	}
	return string(data)
}

func connectionError(method string, u *url.URL, err error, deadline bool) error {
	cerr := &APIConnectionError{Method: method, URL: redactURL(u), Cause: err}
	var netErr net.Error
	if deadline || errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return &APITimeoutError{APIConnectionError: cerr}
	}
	return cerr
}

// redactURL returns the URL without credentials and query string.
func redactURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	clean := url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path, RawPath: u.RawPath}
	return clean.String()
}

func logAttempt(ctx context.Context, cfg *requestconfig.Config, method string, u *url.URL, status int, requestID string, d time.Duration, attempt int, err error) {
	if cfg.Logger == nil {
		return
	}
	attrs := []slog.Attr{
		slog.String("method", method),
		slog.String("path", u.EscapedPath()),
		slog.Duration("duration", d),
		slog.Int("attempt", attempt+1),
	}
	if status != 0 {
		attrs = append(attrs, slog.Int("status", status))
	}
	if requestID != "" {
		attrs = append(attrs, slog.String("request_id", requestID))
	}
	if err != nil {
		attrs = append(attrs, slog.String("error", err.Error()))
	}
	cfg.Logger.LogAttrs(ctx, slog.LevelDebug, "didit request", attrs...)
}

// retryDelay returns how long to wait before retry number attempt+1:
// Retry-After when the response has a usable one (capped at 60 s), otherwise
// exponential backoff from 0.5 s, capped at 8 s, with full jitter.
func retryDelay(attempt int, resp *http.Response) time.Duration {
	if resp != nil {
		if d, ok := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()); ok {
			return d
		}
	}
	backoff := maxRetryDelay
	if attempt < 5 {
		backoff = min(initialRetryDelay<<attempt, maxRetryDelay)
	}
	return time.Duration(rand.Int64N(int64(backoff) + 1))
}

// parseRetryAfter reads a Retry-After value in seconds or as an HTTP date.
func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	var d time.Duration
	if secs, err := strconv.ParseFloat(value, 64); err == nil {
		if secs < 0 || secs != secs {
			return 0, false
		}
		if secs > maxRetryAfter.Seconds() {
			return maxRetryAfter, true
		}
		d = time.Duration(secs * float64(time.Second))
	} else if t, err := http.ParseTime(value); err == nil {
		d = t.Sub(now)
		if d < 0 {
			d = 0
		}
	} else {
		return 0, false
	}
	return min(d, maxRetryAfter), true
}

// pinnedClient returns a copy of hc that never follows a redirect to another
// origin, so the API key cannot leak to it. The redirect response is returned
// as is and surfaces as an *APIStatusError.
func pinnedClient(hc *http.Client) *http.Client {
	if hc == nil {
		hc = defaultHTTPClient
	}
	pinned := *hc
	userCheck := hc.CheckRedirect
	pinned.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 0 && !sameOrigin(req.URL, via[0].URL) {
			return http.ErrUseLastResponse
		}
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		if userCheck != nil {
			return userCheck(req, via)
		}
		return nil
	}
	return &pinned
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(canonicalHost(a), canonicalHost(b))
}

// canonicalHost returns host:port with the default port made explicit.
func canonicalHost(u *url.URL) string {
	port := u.Port()
	if port == "" {
		switch strings.ToLower(u.Scheme) {
		case "https":
			port = "443"
		case "http":
			port = "80"
		}
	}
	return net.JoinHostPort(strings.ToLower(u.Hostname()), port)
}

// UnexpectedResponseError reports a response the SDK cannot use: a 2xx
// response that is not valid JSON, or a next page link to another origin or
// back to a page already fetched.
type UnexpectedResponseError struct {
	Message string
	Cause   error
	// Response is the HTTP response, when there is one. Its body reads the
	// body as received (with an echoed API key redacted), and its request's
	// X-Api-Key header is redacted.
	Response *http.Response
}

func (e *UnexpectedResponseError) Error() string {
	msg := "didit: unexpected response: " + e.Message
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

func (e *UnexpectedResponseError) Unwrap() error { return e.Cause }
func (*UnexpectedResponseError) diditError()     {}
