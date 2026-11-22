// Command corecover reports the test coverage of the hand-written core:
// every package of the module except the generated *_gen.go files, codegen/
// and internal/tools/. It fails below a minimum.
//
//	go test -coverpkg=./... -coverprofile=coverage.out ./...
//	go run ./internal/tools/corecover -min 90 coverage.out
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var line = regexp.MustCompile(`^(.+?):(\d+\.\d+,\d+\.\d+) (\d+) (\d+)$`)

func main() {
	minimum := flag.Float64("min", 0, "fail when coverage is below this percentage")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: corecover [-min N] coverage.out")
		os.Exit(2)
	}
	pct, report, err := coverage(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "corecover:", err)
		os.Exit(1)
	}
	fmt.Print(report)
	if pct < *minimum {
		fmt.Fprintf(os.Stderr, "core coverage %.1f%% is below the %.0f%% minimum\n", pct, *minimum)
		os.Exit(1)
	}
}

func excluded(path string) bool {
	return strings.HasSuffix(path, "_gen.go") ||
		strings.Contains(path, "/codegen/") ||
		strings.Contains(path, "/internal/tools/") ||
		strings.Contains(path, "/examples/")
}

func coverage(profile string) (float64, string, error) {
	f, err := os.Open(profile)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = f.Close() }()
	type block struct{ stmts, count int }
	blocks := map[[2]string]block{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		m := line.FindStringSubmatch(scanner.Text())
		if m == nil || excluded(m[1]) {
			continue
		}
		stmts, _ := strconv.Atoi(m[3])
		count, _ := strconv.Atoi(m[4])
		key := [2]string{m[1], m[2]}
		// The same block appears once per test binary: keep the highest count.
		if prev, ok := blocks[key]; !ok || count > prev.count {
			blocks[key] = block{stmts, count}
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, "", err
	}
	files := map[string][2]int{}
	for key, b := range blocks {
		v := files[key[0]]
		v[0] += b.stmts
		if b.count > 0 {
			v[1] += b.stmts
		}
		files[key[0]] = v
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	total, covered := 0, 0
	for _, n := range names {
		v := files[n]
		total += v[0]
		covered += v[1]
		short := n
		if i := strings.Index(n, "sdk-go/"); i >= 0 {
			short = n[i+len("sdk-go/"):]
		}
		fmt.Fprintf(&b, "%5.1f%%  %s\n", 100*float64(v[1])/float64(v[0]), short)
	}
	if total == 0 {
		return 0, "", fmt.Errorf("no core statements in %s", profile)
	}
	pct := 100 * float64(covered) / float64(total)
	fmt.Fprintf(&b, "core coverage: %.1f%% of %d statements\n", pct, total)
	return pct, b.String(), nil
}
