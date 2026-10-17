#!/usr/bin/env python3
"""Coverage of the hand-written core (generated *_gen.go files excluded).

    go test -coverpkg=./... -coverprofile=coverage.out ./...
    python3 scripts/corecover.py coverage.out 90
"""
import re
import sys

blocks = {}
for line in open(sys.argv[1]):
    if line.startswith("mode:"):
        continue
    m = re.match(r"(.+?):(\d+\.\d+,\d+\.\d+) (\d+) (\d+)", line)
    if not m:
        continue
    path, block, stmts, count = m.group(1), m.group(2), int(m.group(3)), int(m.group(4))
    if path.endswith("_gen.go") or "/codegen/" in path or "/examples/" in path:
        continue
    key = (path, block)
    prev = blocks.get(key, (stmts, 0))
    blocks[key] = (stmts, max(prev[1], count))

files = {}
for (path, _), (stmts, count) in blocks.items():
    total, covered = files.get(path, (0, 0))
    files[path] = (total + stmts, covered + (stmts if count > 0 else 0))
total = sum(t for t, _ in files.values())
covered = sum(c for _, c in files.values())
for path in sorted(files):
    t, c = files[path]
    print(f"{100 * c / t:5.1f}%  {path.split('sdk-go/', 1)[-1]}")
pct = 100 * covered / total if total else 100.0
print(f"core coverage: {pct:.1f}% of {total} statements")
if len(sys.argv) > 2 and pct < float(sys.argv[2]):
    print(f"below the {sys.argv[2]}% minimum", file=sys.stderr)
    sys.exit(1)
