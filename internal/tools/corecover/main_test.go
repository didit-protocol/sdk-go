package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoverage(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "c.out")
	data := strings.Join([]string{
		"mode: atomic",
		"github.com/didit-protocol/sdk-go/core.go:1.1,2.2 3 1",
		"github.com/didit-protocol/sdk-go/core.go:1.1,2.2 3 0",
		"github.com/didit-protocol/sdk-go/core.go:3.1,4.2 1 0",
		"github.com/didit-protocol/sdk-go/sessions_gen.go:1.1,2.2 50 0",
		"github.com/didit-protocol/sdk-go/codegen/main.go:1.1,2.2 50 0",
		"garbage",
	}, "\n")
	if err := os.WriteFile(profile, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	pct, report, err := coverage(profile)
	if err != nil {
		t.Fatal(err)
	}
	if pct != 75 || !strings.Contains(report, "core.go") || strings.Contains(report, "sessions_gen.go") {
		t.Fatalf("pct = %v, report:\n%s", pct, report)
	}
	if _, _, err := coverage(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing file accepted")
	}
	empty := filepath.Join(t.TempDir(), "e.out")
	_ = os.WriteFile(empty, []byte("mode: set\n"), 0o600)
	if _, _, err := coverage(empty); err == nil {
		t.Fatal("empty profile accepted")
	}
}
