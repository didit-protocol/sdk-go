package authentication

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/didit-protocol/sdk-go/internal/naming"
)

// The contract test of the hand-written module (docs/DESIGN.md sections 10
// and 11): openapi/authentication.json, synced daily from
// https://docs.didit.me/openapi-authentication.json, names every issuer
// operation with x-didit-sdk. Every server operation has the Client methods
// below calling it with its HTTP method and path (and, for a variant, its
// discriminator), every Client method is accounted for, and an operation
// server SDKs do not expose has no method. A spec sync that adds, removes or
// renames an Authentication operation fails here until the module follows.

// contractMethods maps each x-didit-sdk method of authentication.json (the
// variants of an operation count as methods of their own) to the Client
// methods that call it. Discovery and JWKS have no method of their own: the
// verifiers fetch them lazily and cache them (DESIGN section 10).
var contractMethods = map[string][]string{
	"get_openid_configuration": {"VerifyAccessToken", "VerifyIDToken"},
	"get_jwks":                 {"VerifyAccessToken", "VerifyIDToken"},
	"exchange_code":            {"ExchangeCode"},
	"refresh":                  {"Refresh"},
	"get_session":              {"GetSession"},
	"revoke_session":           {"RevokeSession"},
	"create_step_up":           {"CreateStepUp"},
	"retrieve_step_up":         {"RetrieveStepUp"},
}

// contractLocal are the Client methods that call no issuer operation.
var contractLocal = map[string]bool{"Issuer": true}

// contractOp is one method of authentication.json.
type contractOp struct {
	method, path string // "POST", "/token/"
	// field and value discriminate a variant ("grant_type",
	// "authorization_code"); empty otherwise.
	field, value string
}

// readContract returns the server methods of authentication.json, and the
// methods of operations server SDKs do not expose.
func readContract(t *testing.T) (map[string]contractOp, []string) {
	t.Helper()
	data, err := os.ReadFile("../openapi/authentication.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	lookup := func(v any) map[string]any {
		for i := 0; i < 8; i++ {
			m, _ := v.(map[string]any)
			ref, ok := m["$ref"].(string)
			if !ok {
				return m
			}
			v = doc
			for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
				v = v.(map[string]any)[part]
			}
		}
		return nil
	}
	server := map[string]contractOp{}
	var hidden []string
	for path, item := range doc["paths"].(map[string]any) {
		for verb, raw := range item.(map[string]any) {
			switch verb {
			case "get", "put", "post", "patch", "delete":
			default:
				continue
			}
			op := raw.(map[string]any)
			key := strings.ToUpper(verb) + " " + path
			sdk, ok := op["x-didit-sdk"].(map[string]any)
			if !ok {
				t.Errorf("%s has no x-didit-sdk naming", key)
				continue
			}
			if sdk["resource"] != "authentication" {
				t.Errorf("%s: resource %v, want authentication", key, sdk["resource"])
			}
			method, _ := sdk["method"].(string)
			if s, _ := sdk["server"].(bool); !s {
				hidden = append(hidden, method)
				continue
			}
			base := contractOp{method: strings.ToUpper(verb), path: path}
			variants, _ := sdk["variants"].([]any)
			if len(variants) == 0 {
				server[method] = base
				continue
			}
			// The discriminator property of the request body tells the
			// variants apart.
			var field string
			for _, media := range lookup(op["requestBody"])["content"].(map[string]any) {
				schema := lookup(media.(map[string]any)["schema"])
				if d, ok := schema["discriminator"].(map[string]any); ok {
					field, _ = d["propertyName"].(string)
				}
			}
			if field == "" {
				t.Errorf("%s has variants but its request body has no discriminator", key)
			}
			for _, v := range variants {
				variant := v.(map[string]any)
				op := base
				op.field, op.value = field, variant["discriminator"].(string)
				server[variant["method"].(string)] = op
			}
		}
	}
	return server, hidden
}

func TestContractAuthenticationDocument(t *testing.T) {
	server, hidden := readContract(t)

	// The document and the map name the same methods.
	var missing, extra []string
	for m := range server {
		if contractMethods[m] == nil {
			missing = append(missing, m)
		}
	}
	for m := range contractMethods {
		if _, ok := server[m]; !ok {
			extra = append(extra, m)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("authentication.json has server methods the module does not implement: %v (add them to the authentication module and to contractMethods)", missing)
	}
	if len(extra) > 0 {
		t.Errorf("contractMethods names methods authentication.json no longer has: %v (remove or rename them in the authentication module)", extra)
	}

	// Every exported Client method calls a document operation or is local,
	// and an operation server SDKs do not expose has no method.
	callers := map[string]bool{}
	for _, list := range contractMethods {
		for _, m := range list {
			callers[m] = true
		}
	}
	typ := reflect.TypeOf(&Client{})
	have := map[string]bool{}
	for i := 0; i < typ.NumMethod(); i++ {
		name := typ.Method(i).Name
		have[name] = true
		if !callers[name] && !contractLocal[name] {
			t.Errorf("Client.%s calls no operation of authentication.json; map it in contractMethods or contractLocal", name)
		}
	}
	for name := range callers {
		if !have[name] {
			t.Errorf("Client has no method %s", name)
		}
	}
	for _, m := range hidden {
		if contractMethods[m] != nil || have[naming.Pascal(m)] {
			t.Errorf("%s is not exposed by server SDKs (x-didit-sdk server false), but the module has it", m)
		}
	}
	if len(hidden) == 0 {
		t.Log("authentication.json has no operation hidden from server SDKs")
	}
}

func TestContractAuthenticationCalls(t *testing.T) {
	server, _ := readContract(t)
	key := newTestKey(t, "k_contract")
	const stepUpID = "4c1d2e3f-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
	calls := map[string]func(ctx context.Context, c *Client) error{
		"VerifyAccessToken": func(ctx context.Context, c *Client) error {
			_, err := c.VerifyAccessToken(ctx, key.mint(t, nil, accessClaims()))
			return err
		},
		"VerifyIDToken": func(ctx context.Context, c *Client) error {
			_, err := c.VerifyIDToken(ctx, key.mint(t, map[string]any{"typ": "JWT"}, idClaims()), "nonce-1", testAudience)
			return err
		},
		"ExchangeCode": func(ctx context.Context, c *Client) error {
			_, err := c.ExchangeCode(ctx, testCode, "verifier", "https://app.example/callback")
			return err
		},
		"Refresh": func(ctx context.Context, c *Client) error {
			_, err := c.Refresh(ctx, testRefreshToken)
			return err
		},
		"GetSession": func(ctx context.Context, c *Client) error {
			_, err := c.GetSession(ctx, testAccessToken)
			return err
		},
		"RevokeSession": func(ctx context.Context, c *Client) error {
			return c.RevokeSession(ctx, testAccessToken)
		},
		"CreateStepUp": func(ctx context.Context, c *Client) error {
			_, err := c.CreateStepUp(ctx, testAccessToken, StepUpParams{Action: "payout"})
			return err
		},
		"RetrieveStepUp": func(ctx context.Context, c *Client) error {
			_, err := c.RetrieveStepUp(ctx, testAccessToken, stepUpID)
			return err
		},
	}
	var methods []string
	for m := range server {
		methods = append(methods, m)
	}
	sort.Strings(methods)
	for _, m := range methods {
		op := server[m]
		// "/step-up/{step_up_id}/" matches "/step-up/<id>/".
		pattern := regexp.MustCompile("^" + regexp.MustCompile(`\\\{[^}]+\\\}`).ReplaceAllString(regexp.QuoteMeta(op.path), "[^/]+") + "$")
		for _, caller := range contractMethods[m] {
			t.Run(m+"/"+caller, func(t *testing.T) {
				call := calls[caller]
				if call == nil {
					t.Fatalf("no call for Client.%s", caller)
				}
				stub := newStub(t, key)
				stub.handle("/session/", func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodDelete {
						w.WriteHeader(http.StatusNoContent)
						return
					}
					writeJSON(w, http.StatusOK, sessionBody())
				})
				stub.handle("/step-up/", func(w http.ResponseWriter, r *http.Request) {
					writeJSON(w, http.StatusOK, map[string]any{"required": false})
				})
				stub.handle("/step-up/"+stepUpID+"/", func(w http.ResponseWriter, r *http.Request) {
					writeJSON(w, http.StatusOK, stepUpRecord())
				})
				c := newTestClient(t, stub, newClock(), WithClientID(testAudience))
				if err := call(context.Background(), c); err != nil {
					t.Fatalf("Client.%s: %v", caller, err)
				}
				for _, r := range stub.all() {
					if r.method != op.method || !pattern.MatchString(r.path) {
						continue
					}
					if op.field == "" {
						return
					}
					form, err := url.ParseQuery(r.body)
					if err != nil {
						t.Fatalf("%s %s body %q: %v", r.method, r.path, r.body, err)
					}
					if got := form.Get(op.field); got != op.value {
						t.Fatalf("Client.%s sends %s=%q, want %q (the %s variant)", caller, op.field, got, op.value, m)
					}
					return
				}
				t.Fatalf("Client.%s never calls %s %s (%s)", caller, op.method, op.path, m)
			})
		}
	}
}
