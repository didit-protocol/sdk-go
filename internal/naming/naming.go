// Package naming converts API names (snake_case, camelCase, enum values) to Go
// identifiers. The generator and the contract test share it.
package naming

import (
	"strings"
	"unicode"
)

// initialisms are written in upper case, following Go conventions.
var initialisms = map[string]bool{
	"acl": true, "aml": true, "api": true, "ascii": true, "bic": true, "cpu": true,
	"css": true, "csv": true, "dns": true, "dob": true, "eof": true, "guid": true,
	"html": true, "http": true, "https": true, "iban": true, "id": true, "ip": true,
	"json": true, "jwt": true, "jwks": true, "kyb": true, "kyc": true, "kyt": true,
	"mfa": true, "mrz": true, "nfc": true, "ocr": true, "oidc": true, "otp": true,
	"pdf": true, "pep": true, "pkce": true, "poa": true, "rpc": true, "sdk": true,
	"sla": true, "sms": true, "sql": true, "ssh": true, "ssn": true, "sso": true,
	"tcp": true, "tls": true, "ttl": true, "ubo": true, "udp": true, "ui": true,
	"uid": true, "uri": true, "url": true, "utf8": true, "uuid": true, "vasp": true,
	"vat": true, "xml": true, "xss": true,
}

// Words splits s into words at non-alphanumeric characters and at camelCase
// boundaries ("accountId" -> account, Id; "HTTPServer" -> HTTP, Server).
func Words(s string) []string {
	var words []string
	var cur []rune
	runes := []rune(s)
	flush := func() {
		if len(cur) > 0 {
			words = append(words, string(cur))
			cur = nil
		}
	}
	for i, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			continue
		}
		if len(cur) > 0 {
			prev := cur[len(cur)-1]
			switch {
			case unicode.IsUpper(r) && unicode.IsLower(prev):
				flush()
			case unicode.IsUpper(r) && unicode.IsUpper(prev) && i+1 < len(runes) && unicode.IsLower(runes[i+1]):
				flush()
			}
		}
		cur = append(cur, r)
	}
	flush()
	return words
}

// word formats one word for an exported identifier.
func word(w string) string {
	lower := strings.ToLower(w)
	if initialisms[lower] {
		return strings.ToUpper(lower)
	}
	if strings.HasSuffix(lower, "s") && initialisms[strings.TrimSuffix(lower, "s")] {
		return strings.ToUpper(strings.TrimSuffix(lower, "s")) + "s"
	}
	runes := []rune(lower)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// Pascal converts s to an exported Go identifier ("retrieve_decision" ->
// RetrieveDecision, "id_verification" -> IDVerification, "IN_REVIEW" ->
// InReview). It returns "" when s has no letters or digits.
func Pascal(s string) string {
	var b strings.Builder
	for _, w := range Words(s) {
		b.WriteString(word(w))
	}
	return b.String()
}

// Camel converts s to an unexported Go identifier ("sessionId" -> sessionID).
func Camel(s string) string {
	words := Words(s)
	if len(words) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(strings.ToLower(words[0]))
	for _, w := range words[1:] {
		b.WriteString(word(w))
	}
	return b.String()
}

// Resource converts a dot path of resource names to the Go prefix of its type
// names ("sessions.imports" -> SessionsImports).
func Resource(path string) string {
	var b strings.Builder
	for _, seg := range strings.Split(path, ".") {
		b.WriteString(Pascal(seg))
	}
	return b.String()
}
