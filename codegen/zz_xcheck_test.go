package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// Temporary cross-check harness; never committed.
func TestZZCrossCheck(t *testing.T) {
	pairs, base, out := os.Getenv("XCHECK_PAIRS"), os.Getenv("XCHECK_BASE"), os.Getenv("XCHECK_OUT")
	if pairs == "" {
		t.Skip()
	}
	entries, _ := os.ReadDir(pairs)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		d := filepath.Join(pairs, e.Name())
		dir := t.TempDir()
		for _, name := range []string{"overlay.json", "SOURCE.json", "account.json", "authentication.json"} {
			data, _ := os.ReadFile(filepath.Join("..", "openapi", name))
			_ = os.WriteFile(filepath.Join(dir, name), data, 0o644)
		}
		old := filepath.Join(d, "old.json")
		if _, err := os.Stat(old); err != nil {
			old = base
		}
		oldData, _ := os.ReadFile(old)
		_ = os.WriteFile(filepath.Join(dir, "verification.json"), oldData, 0o644)
		newData, _ := os.ReadFile(filepath.Join(d, "new.json"))
		acc, _ := os.ReadFile(filepath.Join("..", "openapi", "account.json"))
		auth, _ := os.ReadFile(filepath.Join("..", "openapi", "authentication.json"))
		res, err := syncSpecs(dir, map[string][]byte{"verification.json": newData, "account.json": acc, "authentication.json": auth},
			time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC), "1.1.0")
		line := map[string]any{"pair": e.Name()}
		if err != nil {
			line["error"] = err.Error()
		} else {
			var b []string
			for _, x := range res.breaking {
				b = append(b, x.text())
			}
			if b == nil {
				b = []string{}
			}
			line["type"] = res.typ
			line["breaking"] = b
		}
		data, _ := json.Marshal(line)
		fmt.Fprintln(f, string(data))
	}
}
