package didit

import (
	"github.com/didit-protocol/sdk-go/internal/requestconfig"
	"github.com/didit-protocol/sdk-go/option"
)

// NewClient returns a client for the Didit API. It is safe for concurrent use;
// create one per process and reuse it.
//
// Without options it reads the API key from DIDIT_API_KEY and the organization
// and application ids from DIDIT_ORGANIZATION_ID and DIDIT_APPLICATION_ID;
// DIDIT_LOG=debug enables debug logging. Options passed here override the
// environment, and options passed to a method override both.
//
//	client := didit.NewClient(option.WithAPIKey(os.Getenv("DIDIT_API_KEY")))
func NewClient(opts ...option.RequestOption) *Client {
	c := &Client{options: append(requestconfig.EnvironmentOptions(), opts...)}
	c.init()
	return c
}

// WithOptions returns a copy of the client with more options applied. The
// receiver is not changed.
func (c *Client) WithOptions(opts ...option.RequestOption) *Client {
	merged := make([]option.RequestOption, 0, len(c.options)+len(opts))
	merged = append(merged, c.options...)
	merged = append(merged, opts...)
	n := &Client{options: merged}
	n.init()
	return n
}
