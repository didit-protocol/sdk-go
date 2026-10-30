// Package requestconfig holds the settings of a client or of a single request.
// The option package changes them; the didit package reads them.
package requestconfig

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Defaults from docs/DESIGN.md section 6.
const (
	DefaultBaseURL        = "https://verification.didit.me"
	DefaultAccountBaseURL = "https://apx.didit.me/auth/v2"
	DefaultTimeout        = 60 * time.Second
	DefaultMaxRetries     = 2
)

// Config is the effective configuration of one request.
type Config struct {
	APIKey         string
	BaseURL        *url.URL
	AccountBaseURL *url.URL
	OrganizationID string
	ApplicationID  string
	AccountToken   string
	// Timeout applies to each attempt, from sending the request to reading the
	// whole response body. Zero or less means no per-attempt timeout.
	Timeout    time.Duration
	MaxRetries int
	// Header holds extra headers sent with the request. A header set here
	// replaces the SDK's own value for that name; an empty value removes it.
	Header       http.Header
	Query        url.Values
	HTTPClient   *http.Client
	ResponseInto **http.Response
	Logger       *slog.Logger

	// Sleep waits between retries; tests replace it.
	Sleep func(time.Duration) <-chan time.Time
}

// RequestOption changes a Config. An error is reported as a configuration
// error when the request runs.
type RequestOption func(*Config) error

// New returns a Config with the defaults applied.
func New() *Config {
	base, _ := url.Parse(DefaultBaseURL)
	account, _ := url.Parse(DefaultAccountBaseURL)
	return &Config{
		BaseURL:        base,
		AccountBaseURL: account,
		Timeout:        DefaultTimeout,
		MaxRetries:     DefaultMaxRetries,
		Header:         http.Header{},
		Query:          url.Values{},
	}
}

// Apply applies opts in order and returns the first error.
func (c *Config) Apply(opts ...RequestOption) error {
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(c); err != nil {
			return err
		}
	}
	return nil
}

// EnvironmentOptions returns the options derived from the DIDIT_API_KEY,
// DIDIT_ORGANIZATION_ID, DIDIT_APPLICATION_ID and DIDIT_LOG environment
// variables.
func EnvironmentOptions() []RequestOption {
	var opts []RequestOption
	if v := strings.TrimSpace(os.Getenv("DIDIT_API_KEY")); v != "" {
		opts = append(opts, func(c *Config) error { c.APIKey = v; return nil })
	}
	if v := strings.TrimSpace(os.Getenv("DIDIT_ORGANIZATION_ID")); v != "" {
		opts = append(opts, func(c *Config) error { c.OrganizationID = v; return nil })
	}
	if v := strings.TrimSpace(os.Getenv("DIDIT_APPLICATION_ID")); v != "" {
		opts = append(opts, func(c *Config) error { c.ApplicationID = v; return nil })
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("DIDIT_LOG")), "debug") {
		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
		opts = append(opts, func(c *Config) error { c.Logger = logger; return nil })
	}
	return opts
}

// ParseBaseURL validates a base URL: absolute http or https, no query, no
// fragment and no credentials. A trailing slash is removed.
func ParseBaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, errors.New("invalid base URL: the scheme must be https or http")
	}
	if u.Host == "" {
		return nil, errors.New("invalid base URL: missing host")
	}
	if u.User != nil {
		return nil, errors.New("invalid base URL: credentials are not allowed in the URL")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("invalid base URL: query and fragment are not allowed")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u, nil
}
