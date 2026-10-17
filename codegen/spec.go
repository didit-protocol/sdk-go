package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

// DeprecatedEntry records an operation removed upstream that the SDK keeps
// for one minor version (docs/DESIGN.md section 3.1).
type DeprecatedEntry struct {
	Key             string
	DeprecatedSince string
	Reason          string
	ReplacedBy      string
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
