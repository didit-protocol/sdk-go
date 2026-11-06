// Package option holds the options of the Didit client.
//
// Every option works both for the whole client, passed to didit.NewClient or
// Client.WithOptions, and for a single request, passed as the last arguments
// of any method:
//
//	client := didit.NewClient(option.WithAPIKey("..."), option.WithMaxRetries(4))
//	res, err := client.Sessions.RetrieveDecision(ctx, id, option.WithTimeout(10*time.Second))
//
// Request options are applied after the client options, so they win.
package option

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/didit-protocol/sdk-go/internal/requestconfig"
)

// RequestOption changes the configuration of a client or of one request.
type RequestOption = requestconfig.RequestOption

// WithAPIKey sets the API key sent as the x-api-key header. It defaults to the
// DIDIT_API_KEY environment variable.
func WithAPIKey(key string) RequestOption {
	return func(c *requestconfig.Config) error {
		c.APIKey = strings.TrimSpace(key)
		return nil
	}
}

// WithBaseURL sets the base URL of the verification API. It defaults to
// https://verification.didit.me. The API key is only ever sent to this origin.
func WithBaseURL(baseURL string) RequestOption {
	return func(c *requestconfig.Config) error {
		u, err := requestconfig.ParseBaseURL(baseURL)
		if err != nil {
			return err
		}
		c.BaseURL = u
		return nil
	}
}

// WithAccountBaseURL sets the base URL of the account API (client.Account). It
// defaults to https://apx.didit.me/auth/v2.
func WithAccountBaseURL(baseURL string) RequestOption {
	return func(c *requestconfig.Config) error {
		u, err := requestconfig.ParseBaseURL(baseURL)
		if err != nil {
			return err
		}
		c.AccountBaseURL = u
		return nil
	}
}

// WithOrganizationID sets the organization id used by application-scoped
// operations (paths under /v3/organization/{organization_id}/application/
// {application_id}/). It defaults to the DIDIT_ORGANIZATION_ID environment
// variable.
func WithOrganizationID(id string) RequestOption {
	return func(c *requestconfig.Config) error {
		c.OrganizationID = strings.TrimSpace(id)
		return nil
	}
}

// WithApplicationID sets the application id used by application-scoped
// operations. It defaults to the DIDIT_APPLICATION_ID environment variable.
func WithApplicationID(id string) RequestOption {
	return func(c *requestconfig.Config) error {
		c.ApplicationID = strings.TrimSpace(id)
		return nil
	}
}

// WithAccountToken sets the Bearer token of the account API, as returned by
// client.Account.Login.
func WithAccountToken(token string) RequestOption {
	return func(c *requestconfig.Config) error {
		c.AccountToken = strings.TrimSpace(token)
		return nil
	}
}

// WithTimeout sets the timeout of each attempt, from sending the request to
// reading the whole response. The default is 60 seconds; zero disables it.
// A deadline on the context still bounds the whole call, retries included.
func WithTimeout(d time.Duration) RequestOption {
	return func(c *requestconfig.Config) error {
		if d < 0 {
			return errors.New("the timeout must not be negative")
		}
		c.Timeout = d
		return nil
	}
}

// WithMaxRetries sets how many times a failed request is retried (default 2).
// See the README for which failures are retried.
func WithMaxRetries(n int) RequestOption {
	return func(c *requestconfig.Config) error {
		if n < 0 {
			return errors.New("max retries must not be negative")
		}
		c.MaxRetries = n
		return nil
	}
}

// WithHeader sets a header on every request, replacing any value the SDK
// would send for that name. An empty value removes the header.
func WithHeader(key, value string) RequestOption {
	return func(c *requestconfig.Config) error {
		c.Header.Set(key, value)
		return nil
	}
}

// WithHeaderAdd adds a value to a header.
func WithHeaderAdd(key, value string) RequestOption {
	return func(c *requestconfig.Config) error {
		c.Header.Add(key, value)
		return nil
	}
}

// WithQuery sets a query parameter, replacing any value the SDK would send.
// It is meant for parameters this SDK version does not know yet.
func WithQuery(key, value string) RequestOption {
	return func(c *requestconfig.Config) error {
		c.Query.Set(key, value)
		return nil
	}
}

// WithHTTPClient sets the HTTP client used to send requests. The SDK never
// follows a redirect to another origin, whatever the client's CheckRedirect.
func WithHTTPClient(client *http.Client) RequestOption {
	return func(c *requestconfig.Config) error {
		if client == nil {
			return errors.New("the HTTP client must not be nil")
		}
		c.HTTPClient = client
		return nil
	}
}

// WithResponseInto stores the raw HTTP response of the request in *dst: status,
// headers and the X-Request-Id header. For JSON responses the body has been
// read and is replaced by an in-memory copy. Use it as a request option:
//
//	var raw *http.Response
//	res, err := client.Sessions.RetrieveDecision(ctx, id, option.WithResponseInto(&raw))
func WithResponseInto(dst **http.Response) RequestOption {
	return func(c *requestconfig.Config) error {
		c.ResponseInto = dst
		return nil
	}
}

// WithLogger enables debug logging to logger: one Debug record per attempt
// with the method, path, status, duration and request id. Bodies, headers and
// the API key are never logged. DIDIT_LOG=debug enables the same logging to
// standard error.
func WithLogger(logger *slog.Logger) RequestOption {
	return func(c *requestconfig.Config) error {
		c.Logger = logger
		return nil
	}
}
