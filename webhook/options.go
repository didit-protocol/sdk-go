package webhook

import (
	"fmt"
	"slices"
	"time"

	didit "github.com/didit-protocol/sdk-go"
)

// DefaultTolerance is how far the signed timestamp may be from the current
// time unless WithTolerance says otherwise.
const DefaultTolerance = 300 * time.Second

// Option configures Verify and VerifyParsed.
type Option func(*config)

type config struct {
	secrets     []string // primary first
	extra       []string
	tolerance   time.Duration
	allowSimple bool
	now         func() time.Time
}

// WithSecrets adds secrets to accept besides the primary secret, for a secret
// rotation: a delivery is accepted when a signature matches any of them. The
// primary secret is tried first.
func WithSecrets(secrets ...string) Option {
	secrets = slices.Clone(secrets)
	return func(c *config) { c.extra = append(c.extra, secrets...) }
}

// WithTolerance sets how far the signed timestamp may be from the current time
// in either direction. The default is DefaultTolerance (300 seconds). A
// negative tolerance is a configuration error.
func WithTolerance(d time.Duration) Option {
	return func(c *config) { c.tolerance = d }
}

// WithAllowSimple also accepts the deprecated X-Signature-Simple and
// X-Signature-Simple-Previous headers. They sign only the timestamp,
// session_id, status and webhook_type, not the body, so an event accepted
// through them reports Authenticated() == false: do not trust its other fields
// and re-fetch the record from the API instead.
func WithAllowSimple() Option {
	return func(c *config) { c.allowSimple = true }
}

// WithClock replaces time.Now as the source of the current time, for tests.
// A nil now restores time.Now.
func WithClock(now func() time.Time) Option {
	return func(c *config) { c.now = now }
}

func newConfig(secret string, opts []Option) (*config, error) {
	c := &config{tolerance: DefaultTolerance}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	if secret == "" {
		return nil, &didit.ConfigurationError{Message: "webhook secret is empty"}
	}
	for i, s := range c.extra {
		if s == "" {
			return nil, &didit.ConfigurationError{Message: fmt.Sprintf("webhook secret %d passed to WithSecrets is empty", i+1)}
		}
	}
	if c.tolerance < 0 {
		return nil, &didit.ConfigurationError{Message: "webhook timestamp tolerance is negative"}
	}
	if c.now == nil {
		c.now = time.Now
	}
	c.secrets = append([]string{secret}, c.extra...)
	return c, nil
}
