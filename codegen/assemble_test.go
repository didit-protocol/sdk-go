package main

import (
	"strings"
	"testing"
)

// TestAssembleImportsOnlyUsedPackages: a doc comment or string that mentions
// "time." is not a use of the time package.
func TestAssembleImportsOnlyUsedPackages(t *testing.T) {
	body := "// Counts verifications up to the same local time.\n" +
		"func f(ctx context.Context) string { return \"time.\" + http.MethodGet }\n"
	out, err := assemble([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	src := string(out)
	for _, want := range []string{`"context"`, `"net/http"`} {
		if !strings.Contains(src, want) {
			t.Errorf("missing import %s in:\n%s", want, src)
		}
	}
	if strings.Contains(src, `"time"`) {
		t.Errorf("imported time although the body only mentions it in text:\n%s", src)
	}
	used := "func g(d time.Duration) {}\n"
	out, err = assemble([]byte(used))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"time"`) {
		t.Errorf("time not imported although used:\n%s", out)
	}
}
