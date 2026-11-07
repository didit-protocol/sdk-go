package main

import (
	"regexp"
	"strings"
)

// Generated tests copy the spec's examples. Values that look like credentials
// (tokens, keys, secrets, JWTs) are replaced with obvious placeholders, so the
// full-history secret scan stays clean without allowlisting test files.
var (
	sensitiveKey = regexp.MustCompile(`(?i)(token|secret|api_?key|password|passwd|private_?key|credential|signature)`)
	jwtLike      = regexp.MustCompile(`^eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`)
	keyLike      = regexp.MustCompile(`^(sk|pk|rk|whsec)_(live|test)_`)
	timestamp    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$`)
	nonAlnum     = regexp.MustCompile(`[^A-Za-z0-9]+`)
)

// sanitizeExample returns a copy of n with credential-like strings replaced.
func sanitizeExample(n *Node) *Node {
	return sanitize(n, "")
}

func sanitize(n *Node, key string) *Node {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case String:
		if (key != "" && sensitiveKey.MatchString(key) && n.Str != "" && !timestamp.MatchString(n.Str)) || jwtLike.MatchString(n.Str) || keyLike.MatchString(n.Str) {
			name := strings.ToLower(strings.Trim(nonAlnum.ReplaceAllString(key, "-"), "-"))
			if name == "" {
				name = "value"
			}
			return &Node{Kind: String, Str: "sample-" + name}
		}
		return n
	case Array:
		out := &Node{Kind: Array}
		for _, item := range n.Arr {
			out.Arr = append(out.Arr, sanitize(item, key))
		}
		return out
	case Object:
		out := &Node{Kind: Object}
		for _, k := range n.Keys {
			out.Set(k, sanitize(n.Get(k), k))
		}
		return out
	}
	return n
}
