package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/didit-protocol/sdk-go/internal/naming"
)

// Doc is one OpenAPI document of openapi/.
type Doc struct {
	// Name is "verification" or "account".
	Name string
	Root *Node
}

// Naming is an x-didit-sdk object (docs/DESIGN.md section 4).
type Naming struct {
	Resource string
	Method   string
	Auth     string
	Server   bool
	Scope    string
}

// Operation is one operation of the spec with its SDK naming.
type Operation struct {
	Doc        *Doc
	Key        string // overlay key: "<METHOD> <path>" or "account <METHOD> <path>"
	HTTPMethod string
	Path       string
	Node       *Node
	PathItem   *Node
	Naming     Naming
	// Deprecated is set for operations kept from openapi/deprecated-operations.json.
	Deprecated *DeprecatedEntry
}

// DeprecatedEntry is one method of openapi/deprecated-operations.json: a
// server method whose operation upstream removed or renamed, which the SDK
// keeps, marked deprecated, until RemoveIn (docs/DESIGN.md section 3.1).
// The spec sync (codegen/sync.go) writes the entries.
type DeprecatedEntry struct {
	// Method is the key of the entry, "resource.method".
	Method string
	// OperationKey is the overlay key of the operation when it was recorded.
	OperationKey string
	// Reason is "removed" or "renamed".
	Reason string
	// Replacement is the resource.method that replaces a renamed method.
	Replacement string
	// Since is the date of the sync that recorded the entry.
	Since string
	// DeprecatedSince is the SDK version of that date; RemoveIn is the
	// version that deletes the method.
	DeprecatedSince string
	RemoveIn        string
	// Note explains the entry in words (the doc comment uses Notice).
	Note string
}

// Notice returns the "Deprecated:" paragraph of the kept method's doc
// comment; a rename links the method that replaces it.
func (e *DeprecatedEntry) Notice() string {
	when := ""
	if e.Since != "" {
		when = " on " + e.Since
	}
	var b strings.Builder
	if e.Reason == "renamed" && e.Replacement != "" {
		fmt.Fprintf(&b, "Deprecated: Use %s instead; the API renamed this operation%s.", deprecatedMethodLink(e.Replacement), when)
	} else {
		fmt.Fprintf(&b, "Deprecated: The operation was removed from the API%s.", when)
	}
	if e.RemoveIn != "" {
		fmt.Fprintf(&b, " This method is removed in version %s.", e.RemoveIn)
	}
	return b.String()
}

// deprecatedMethodLink returns the doc link of a method given as
// resource.method ("billing.get_balance" -> "[BillingService.GetBalance]").
func deprecatedMethodLink(name string) string {
	i := strings.LastIndex(name, ".")
	if i <= 0 {
		return name
	}
	return "[" + naming.Resource(name[:i]) + "Service." + naming.Pascal(name[i+1:]) + "]"
}

// Spec is everything the generator reads.
type Spec struct {
	Verification *Doc
	Account      *Doc
	Overlay      *Node
	Ops          []*Operation
}

var httpMethods = []string{"get", "post", "put", "patch", "delete"}

// LoadSpec reads openapi/ under dir.
func LoadSpec(dir string) (*Spec, error) {
	read := func(name string) (*Node, error) {
		data, err := os.ReadFile(filepath.Join(dir, "openapi", name))
		if err != nil {
			return nil, err
		}
		n, err := ParseNode(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		return n, nil
	}
	verification, err := read("verification.json")
	if err != nil {
		return nil, err
	}
	account, err := read("account.json")
	if err != nil {
		return nil, err
	}
	overlay, err := read("overlay.json")
	if err != nil {
		return nil, err
	}
	s := &Spec{
		Verification: &Doc{Name: "verification", Root: verification},
		Account:      &Doc{Name: "account", Root: account},
		Overlay:      overlay.Get("operations"),
	}
	var problems []string
	for _, doc := range []*Doc{s.Verification, s.Account} {
		paths := doc.Root.Get("paths")
		if paths == nil {
			continue
		}
		for _, path := range paths.Keys {
			item := paths.Get(path)
			for _, m := range httpMethods {
				op := item.Get(m)
				if op == nil {
					continue
				}
				key := strings.ToUpper(m) + " " + path
				if doc.Name == "account" {
					key = "account " + key
				}
				naming, ok, err := namingOf(op.Get("x-didit-sdk"))
				if err != nil {
					problems = append(problems, fmt.Sprintf("%s: x-didit-sdk: %v", key, err))
					continue
				}
				if !ok {
					naming, ok, err = namingOf(s.Overlay.Get(key))
					if err != nil {
						problems = append(problems, fmt.Sprintf("%s: overlay.json: %v", key, err))
						continue
					}
				}
				if !ok {
					problems = append(problems, fmt.Sprintf("%s (operationId %q) has no SDK naming: add an x-didit-sdk entry for it to openapi/overlay.json (docs/DESIGN.md section 4)", key, op.Get("operationId").Text()))
					continue
				}
				s.Ops = append(s.Ops, &Operation{
					Doc: doc, Key: key, HTTPMethod: strings.ToUpper(m), Path: path,
					Node: op, PathItem: item, Naming: naming,
				})
			}
		}
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("openapi naming:\n  %s", strings.Join(problems, "\n  "))
	}
	if err := s.loadDeprecated(dir); err != nil {
		return nil, err
	}
	seen := map[string]string{}
	for _, op := range s.ServerOps() {
		id := op.Naming.Resource + "." + op.Naming.Method
		if prev, dup := seen[id]; dup {
			problems = append(problems, fmt.Sprintf("%s and %s both map to %s", prev, op.Key, id))
		}
		seen[id] = op.Key
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("openapi naming:\n  %s", strings.Join(problems, "\n  "))
	}
	return s, nil
}

func namingOf(n *Node) (Naming, bool, error) {
	if n == nil {
		return Naming{}, false, nil
	}
	if n.Kind != Object {
		return Naming{}, false, fmt.Errorf("not an object")
	}
	nm := Naming{
		Resource: n.Get("resource").Text(),
		Method:   n.Get("method").Text(),
		Auth:     n.Get("auth").Text(),
		Server:   n.Get("server").Truthy(),
		Scope:    n.Get("scope").Text(),
	}
	if nm.Resource == "" || nm.Method == "" {
		return nm, false, fmt.Errorf("resource and method are required")
	}
	switch nm.Auth {
	case "api_key", "account_token", "none", "device_token", "public":
	default:
		return nm, false, fmt.Errorf("unknown auth %q", nm.Auth)
	}
	if nm.Scope != "" && nm.Scope != "application" {
		return nm, false, fmt.Errorf("unknown scope %q", nm.Scope)
	}
	return nm, true, nil
}

// ServerOps returns the operations the SDK exposes, sorted by resource and
// method.
func (s *Spec) ServerOps() []*Operation {
	var out []*Operation
	for _, op := range s.Ops {
		if op.Naming.Server {
			out = append(out, op)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Naming, out[j].Naming
		if a.Resource != b.Resource {
			return a.Resource < b.Resource
		}
		return a.Method < b.Method
	})
	return out
}

// Resolve follows a local $ref ("#/components/schemas/X") in doc.
func (d *Doc) Resolve(n *Node) *Node {
	for i := 0; n != nil && n.Has("$ref") && i < 32; i++ {
		n = d.Lookup(n.Get("$ref").Text())
	}
	return n
}

// Lookup returns the node a local reference points to.
func (d *Doc) Lookup(ref string) *Node {
	if !strings.HasPrefix(ref, "#/") {
		return nil
	}
	n := d.Root
	for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		n = n.Get(part)
	}
	return n
}

// Parameters returns the operation's parameters with path-level ones,
// resolved.
func (op *Operation) Parameters() []*Node {
	var out []*Node
	seen := map[string]bool{}
	add := func(list *Node) {
		if list == nil {
			return
		}
		for _, p := range list.Arr {
			p = op.Doc.Resolve(p)
			key := p.Get("in").Text() + ":" + p.Get("name").Text()
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, p)
		}
	}
	add(op.Node.Get("parameters"))
	add(op.PathItem.Get("parameters"))
	return out
}

// loadDeprecated adds the methods of openapi/deprecated-operations.json
// (written by the spec sync): operations that upstream removed, and the old
// names of renamed ones. Each entry carries the operation as it was, with
// its $refs, and the components it reaches; components the documents no
// longer have are added back, so the method and its types are generated
// with the same Go names as before.
func (s *Spec) loadDeprecated(dir string) error {
	data, err := os.ReadFile(filepath.Join(dir, "openapi", "deprecated-operations.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	root, err := ParseNode(data)
	if err != nil {
		return fmt.Errorf("openapi/deprecated-operations.json: %w", err)
	}
	methods := root.Get("methods")
	if methods == nil {
		return nil
	}
	if methods.Kind != Object {
		return fmt.Errorf("openapi/deprecated-operations.json: methods is not an object")
	}
	live := map[string]string{}
	for _, op := range s.Ops {
		if op.Naming.Server {
			live[op.Naming.Resource+"."+op.Naming.Method] = op.Key
		}
	}
	var problems []string
	for _, name := range methods.Keys {
		op, err := s.deprecatedOperation(name, methods.Get(name), live)
		if err != nil {
			problems = append(problems, fmt.Sprintf("openapi/deprecated-operations.json %s: %v", name, err))
			continue
		}
		s.Ops = append(s.Ops, op)
	}
	if len(problems) > 0 {
		return fmt.Errorf("deprecated methods:\n  %s", strings.Join(problems, "\n  "))
	}
	return nil
}

// deprecatedOperation builds the operation of one deprecated method.
func (s *Spec) deprecatedOperation(name string, e *Node, live map[string]string) (*Operation, error) {
	if e == nil || e.Kind != Object {
		return nil, fmt.Errorf("not an object")
	}
	nm, ok, err := namingOf(e.Get("x-didit-sdk"))
	if err != nil {
		return nil, fmt.Errorf("x-didit-sdk: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("x-didit-sdk is missing")
	}
	if id := nm.Resource + "." + nm.Method; id != name {
		return nil, fmt.Errorf("the entry name must equal its x-didit-sdk resource.method (%s)", id)
	}
	if !nm.Server {
		return nil, fmt.Errorf("x-didit-sdk server is not true: only server methods are kept")
	}
	if provider, ok := live[name]; ok {
		return nil, fmt.Errorf("%s provides %s again, so it is no longer deprecated; delete this entry from openapi/deprecated-operations.json", provider, name)
	}
	entry := &DeprecatedEntry{
		Method:          name,
		Reason:          e.Get("reason").Text(),
		Replacement:     e.Get("replacement").Text(),
		Since:           e.Get("since").Text(),
		DeprecatedSince: e.Get("deprecated_since").Text(),
		RemoveIn:        e.Get("remove_in").Text(),
		Note:            e.Get("note").Text(),
	}
	switch entry.Reason {
	case "removed":
	case "renamed":
		if entry.Replacement == "" {
			return nil, fmt.Errorf("reason renamed needs a replacement")
		}
	default:
		return nil, fmt.Errorf("reason %q is not removed or renamed", entry.Reason)
	}
	api, method, path, err := deprecatedTarget(e)
	if err != nil {
		return nil, err
	}
	doc := s.Verification
	entry.OperationKey = method + " " + path
	if api == "account" {
		doc = s.Account
		entry.OperationKey = "account " + entry.OperationKey
	}
	opNode := e.Get("operation")
	if opNode == nil || opNode.Kind != Object {
		return nil, fmt.Errorf("operation is not an object")
	}
	if comps := e.Get("components"); comps != nil {
		if err := addMissingComponents(doc, comps); err != nil {
			return nil, err
		}
	}
	item := &Node{Kind: Object}
	item.Set(strings.ToLower(method), opNode)
	return &Operation{
		Doc: doc, Key: entry.OperationKey + " (deprecated)", HTTPMethod: method, Path: path,
		Node: opNode, PathItem: item, Naming: nm, Deprecated: entry,
	}, nil
}

// deprecatedTarget returns the document ("verification" or "account"), HTTP
// method and path of an entry, from api, http_method and path, or from
// operation_key ("[account ]METHOD /path"); when both are given they must
// agree.
func deprecatedTarget(e *Node) (api, method, path string, err error) {
	api, method, path = e.Get("api").Text(), strings.ToUpper(e.Get("http_method").Text()), e.Get("path").Text()
	if key := e.Get("operation_key").Text(); key != "" {
		kAPI, rest := "verification", key
		if r, ok := strings.CutPrefix(key, "account "); ok {
			kAPI, rest = "account", r
		}
		kMethod, kPath, ok := strings.Cut(rest, " ")
		if !ok {
			return "", "", "", fmt.Errorf("operation_key %q is not \"[account ]METHOD /path\"", key)
		}
		for _, f := range []struct{ field, fromKey *string }{{&api, &kAPI}, {&method, &kMethod}, {&path, &kPath}} {
			switch {
			case *f.field == "":
				*f.field = *f.fromKey
			case *f.field != *f.fromKey:
				return "", "", "", fmt.Errorf("operation_key %q does not match api %q, http_method %q and path %q", key, api, method, path)
			}
		}
	}
	if api != "verification" && api != "account" {
		return "", "", "", fmt.Errorf("api %q is not verification or account", api)
	}
	known := false
	for _, m := range httpMethods {
		known = known || strings.ToLower(method) == m
	}
	if !known || !strings.HasPrefix(path, "/") {
		return "", "", "", fmt.Errorf("%q %q is not an HTTP method and a path", method, path)
	}
	return api, method, path, nil
}

// addMissingComponents adds to doc the components it does not have
// (components removed upstream with the operation). A component the
// document still has is used as it is now: Go has one type per name.
func addMissingComponents(doc *Doc, comps *Node) error {
	if comps.Kind != Object {
		return fmt.Errorf("components is not an object")
	}
	for _, section := range comps.Keys {
		entries := comps.Get(section)
		if entries.Kind != Object {
			return fmt.Errorf("components.%s is not an object", section)
		}
		have := doc.Root.Get("components")
		if have == nil {
			have = &Node{Kind: Object}
			doc.Root.Set("components", have)
		}
		target := have.Get(section)
		if target == nil {
			target = &Node{Kind: Object}
			have.Set(section, target)
		}
		if have.Kind != Object || target.Kind != Object {
			return fmt.Errorf("%s: components.%s is not an object", doc.Name, section)
		}
		for _, name := range entries.Keys {
			if !target.Has(name) {
				target.Set(name, entries.Get(name))
			}
		}
	}
	return nil
}
