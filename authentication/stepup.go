package authentication

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"
)

// StepUpAction is the sensitive action a step-up protects. It is an open set:
// unknown values are sent as is.
type StepUpAction string

// Step-up actions the issuer knows.
const (
	// StepUpActionPayout is a payout; Amount is required, Currency optional.
	StepUpActionPayout StepUpAction = "payout"
	// StepUpActionBank is adding or changing a bank account.
	StepUpActionBank StepUpAction = "bank"
	// StepUpActionContact is changing contact details.
	StepUpActionContact StepUpAction = "contact"
)

// StepUpStatus is the status of a step-up. It is an open set: unknown values
// never fail decoding. Act only on [StepUpStatusApproved].
type StepUpStatus string

// Step-up statuses.
const (
	StepUpStatusPending    StepUpStatus = "pending"
	StepUpStatusReview     StepUpStatus = "review"
	StepUpStatusApproved   StepUpStatus = "approved"
	StepUpStatusDeclined   StepUpStatus = "declined"
	StepUpStatusExpired    StepUpStatus = "expired"
	StepUpStatusSuperseded StepUpStatus = "superseded"
)

// StepUpParams are the parameters of [Client.CreateStepUp]. Empty optional
// fields are not sent.
type StepUpParams struct {
	// Action is required.
	Action StepUpAction
	// Amount is a decimal string such as "250.00" with at most two decimals.
	// The issuer requires it for StepUpActionPayout.
	Amount string
	// Currency is an ISO 4217 code such as "USD". A payout in another currency
	// than the payout rule's is always checked.
	Currency string
	// ReturnTo is where the person returns after verifying, with
	// step_up=<id>&status=<status>. It must be one of the application's
	// registered redirect URIs. The issuer currently requires it and answers
	// 400 without it, so set it.
	ReturnTo string
}

// StepUp is a step-up: a verification the person must pass before a
// sensitive action.
type StepUp struct {
	// Required is false when no rule applies to the action: then the issuer
	// answered 200 {"required": false}, every other field is empty and the
	// action may go ahead. It is true for a step-up record.
	Required bool `json:"required"`
	// ID is the step-up id; keep it to call [Client.RetrieveStepUp].
	ID string `json:"id"`
	// Rule is the rule that applied: "payout", "bank", "contact" or "device".
	Rule string `json:"rule"`
	// Method is the verification asked for: "liveness", "selfie" or
	// "full_id".
	Method string `json:"method"`
	// Status is the step-up status.
	Status StepUpStatus `json:"status"`
	// Level is the verification level reached; empty until verified.
	Level string `json:"level"`
	// Amount is the payout amount as a decimal string; empty for other
	// actions.
	Amount string `json:"amount"`
	// Currency is the payout currency; empty for other actions.
	Currency string `json:"currency"`
	// VerificationURL is the hosted page where the person verifies. Only
	// [Client.CreateStepUp] returns it.
	VerificationURL string `json:"verification_url"`
	// ExpiresIn is the number of seconds the person has to verify. Only
	// [Client.CreateStepUp] returns it.
	ExpiresIn int64 `json:"expires_in"`
	// CreatedAt is when the step-up was created.
	CreatedAt time.Time `json:"created_at"`
	// CompletedAt is when the step-up was decided; zero until then.
	CompletedAt time.Time `json:"completed_at"`
	// ExpiresAt is when the step-up expires if the person does not verify.
	ExpiresAt time.Time `json:"expires_at"`
	// Raw is the response body as received, with fields this version does
	// not know.
	Raw json.RawMessage `json:"-"`
}

type stepUpBody struct {
	Action   StepUpAction `json:"action"`
	Amount   string       `json:"amount,omitempty"`
	Currency string       `json:"currency,omitempty"`
	ReturnTo string       `json:"return_to,omitempty"`
}

// CreateStepUp asks whether the person behind an access token must verify
// before an action (POST step-up/). When no rule applies it returns a StepUp
// with Required false. Otherwise the issuer creates a step-up (201): send the
// person to VerificationURL, and when they come back to ReturnTo, call
// [Client.RetrieveStepUp] and act only when its Status is
// [StepUpStatusApproved]. The status in the ReturnTo query is not proof.
//
// The request is not retried. Invalid parameters, including a missing or
// unregistered ReturnTo, are a *didit.BadRequestError whose body names the
// field.
func (c *Client) CreateStepUp(ctx context.Context, accessToken string, params StepUpParams) (*StepUp, error) {
	if err := checkBearer(accessToken); err != nil {
		return nil, err
	}
	if params.Action == "" {
		return nil, configError("step-up action is required")
	}
	body, err := json.Marshal(stepUpBody(params))
	if err != nil {
		return nil, configError("encoding step-up parameters: " + err.Error())
	}
	r := request{
		method:      http.MethodPost,
		url:         c.endpoint("step-up/"),
		bearer:      accessToken,
		contentType: contentTypeJSON,
		body:        body,
	}
	return c.stepUp(ctx, r, false)
}

// RetrieveStepUp returns a step-up of the session behind the access token
// (GET step-up/{id}/). Act on the protected action only when Status is
// [StepUpStatusApproved]; every other status means no. A step-up of another
// session is a *didit.NotFoundError.
func (c *Client) RetrieveStepUp(ctx context.Context, accessToken, id string) (*StepUp, error) {
	if err := checkBearer(accessToken); err != nil {
		return nil, err
	}
	if id == "" {
		return nil, configError("step-up id is required")
	}
	r := request{
		method: http.MethodGet,
		url:    c.endpoint("step-up/" + url.PathEscape(id) + "/"),
		bearer: accessToken,
		retry:  true,
	}
	return c.stepUp(ctx, r, true)
}

func (c *Client) stepUp(ctx context.Context, r request, record bool) (*StepUp, error) {
	resp, err := c.send(ctx, r)
	if err != nil {
		return nil, err
	}
	var out StepUp
	if err := decodeJSON(r, resp.body, &out); err != nil {
		return nil, err
	}
	if record {
		// GET step-up/{id}/ returns the record without "required".
		out.Required = true
	}
	out.Raw = resp.body
	return &out, nil
}
