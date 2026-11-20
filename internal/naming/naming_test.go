package naming

import "testing"

func TestPascal(t *testing.T) {
	cases := map[string]string{
		"retrieve_decision":     "RetrieveDecision",
		"id_verification":       "IDVerification",
		"IN_REVIEW":             "InReview",
		"In Review":             "InReview",
		"accountId":             "AccountID",
		"sessionId":             "SessionID",
		"generate_pdf":          "GeneratePDF",
		"ip_analyses":           "IPAnalyses",
		"session_ids":           "SessionIDs",
		"HTTPServer":            "HTTPServer",
		"kyb.registry":          "KYBRegistry",
		"uae-pass":              "UaePass",
		"3d":                    "3d",
		"":                      "",
		"travel_rule":           "TravelRule",
		"poa_languages_allowed": "POALanguagesAllowed",
	}
	for in, want := range cases {
		if got := Pascal(in); got != want {
			t.Errorf("Pascal(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCamel(t *testing.T) {
	cases := map[string]string{
		"sessionId":   "sessionID",
		"vendor_data": "vendorData",
		"org_id":      "orgID",
		"id":          "id",
	}
	for in, want := range cases {
		if got := Camel(in); got != want {
			t.Errorf("Camel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResource(t *testing.T) {
	if got := Resource("sessions.imports"); got != "SessionsImports" {
		t.Fatal(got)
	}
	if got := Resource("transactions.travel_rule"); got != "TransactionsTravelRule" {
		t.Fatal(got)
	}
}
