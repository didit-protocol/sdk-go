package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

// The shared breaking-change vectors (docs/DESIGN.md section 3.1).
// openapi/breaking-vectors.json is generated in sdk-python
// (codegen/breaking_vectors.py) and copied here byte for byte; every server
// SDK runs each case through its own spec sync and must give the same commit
// type and the same counted changes. A case that looks wrong is fixed in
// sdk-python first, never here.

// syncVectorsSHA256 is the SHA-256 of the copy of openapi/breaking-vectors.json
// (sdk-python 3c361f6e873673ed8a55ad7ea000176e6880f8d3). A new copy updates it.
const syncVectorsSHA256 = "5c9a66000f9743f9b8fa67562a390903bc78860cb5355f8e3421901fa5798026"

// syncVectors is openapi/breaking-vectors.json.
type syncVectors struct {
	Version   int    `json:"version"`
	Rules     string `json:"rules"`
	Generator string `json:"generator"`
	Cases     []struct {
		Name   string          `json:"name"`
		Rule   string          `json:"rule"`
		Before json.RawMessage `json:"before"`
		After  json.RawMessage `json:"after"`
		Expect struct {
			Type     string   `json:"type"`
			Breaking []string `json:"breaking"`
		} `json:"expect"`
	} `json:"cases"`
}

func TestSyncBreakingVectors(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "openapi", "breaking-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != syncVectorsSHA256 {
		t.Fatalf("openapi/breaking-vectors.json has SHA-256 %s, want %s: copy it byte for byte from sdk-python and update syncVectorsSHA256", got, syncVectorsSHA256)
	}
	var vectors syncVectors
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatal(err)
	}
	if vectors.Version != 1 || vectors.Rules != "docs/DESIGN.md section 3.1" || len(vectors.Cases) == 0 {
		t.Fatalf("version %d, rules %q, %d cases", vectors.Version, vectors.Rules, len(vectors.Cases))
	}
	seen := map[string]bool{}
	for _, c := range vectors.Cases {
		if seen[c.Name] {
			t.Fatalf("case %q appears twice", c.Name)
		}
		seen[c.Name] = true
		t.Run(c.Name, func(t *testing.T) {
			typ, breaking := syncClassify(t, c.Before, c.After)
			want := c.Expect.Breaking
			if want == nil {
				want = []string{}
			}
			if typ != c.Expect.Type || !reflect.DeepEqual(breaking, want) {
				t.Errorf("rule %s: got %s with %d breaking changes:\n%s\nwant %s with %d:\n%s",
					c.Rule, typ, len(breaking), syncLines(breaking), c.Expect.Type, len(want), syncLines(want))
			}
		})
	}
}

// syncClassify is what the spec sync makes of verification.json changing
// from before to after (every operation named by its own x-didit-sdk): the
// commit type, "feat(spec)" or "chore(spec)", and the counted changes as
// the vectors spell them, distinct and sorted.
func syncClassify(t *testing.T, before, after []byte) (string, []string) {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string][]byte{"verification.json": before, "overlay.json": []byte(`{"operations": {}}`)} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res, err := syncSpecs(dir, map[string][]byte{"verification.json": after}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	distinct := map[string]bool{}
	for _, b := range res.breaking {
		distinct[b.normalized()] = true
	}
	lines := make([]string, 0, len(distinct))
	for line := range distinct {
		lines = append(lines, line)
	}
	sort.Strings(lines)
	return res.typ + "(spec)", lines
}

func syncLines(lines []string) string {
	out := ""
	for _, l := range lines {
		out += "  " + l + "\n"
	}
	return out
}
