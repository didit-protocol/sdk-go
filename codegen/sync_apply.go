package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// syncDeprecatedDescription heads a new openapi/deprecated-operations.json.
const syncDeprecatedDescription = "Public SDK methods kept for one more minor version after the API contract dropped them (docs/DESIGN.md section 3.1). Written by `go run ./codegen sync`, keyed by resource.method. reason is removed (the operation left the contract) or renamed (its x-didit-sdk naming changed; replacement names the new method). Each entry holds the operation as it was, with its $refs intact, and under components every component it reaches, so the generator keeps the method and its types under the same Go names. codegen/deprecated_test.go fails once internal/version.Version reaches remove_in: delete the entry and run `go run ./codegen` in that release."

// syncResult is the outcome of one spec sync.
type syncResult struct {
	changed bool
	summary string
}

// syncRefusal reports a download the SDK cannot be generated from (an error
// page, a maintenance JSON, ...). Nothing is written.
type syncRefusal struct{ msg string }

func (e *syncRefusal) Error() string {
	return "spec sync refused the download, nothing was written: " + e.msg
}

// syncCheckOpenAPI parses data and refuses anything that is not an OpenAPI
// 3 document with paths, so that an error page never replaces the contract.
func syncCheckOpenAPI(name string, data []byte) (*Node, error) {
	if !utf8.Valid(data) {
		return nil, &syncRefusal{name + ": the download is not JSON (it is not valid UTF-8)."}
	}
	doc, err := ParseNode(data)
	if err != nil {
		return nil, &syncRefusal{fmt.Sprintf("%s: the download is not JSON (%v).", name, err)}
	}
	if doc.Kind != Object {
		return nil, &syncRefusal{name + ": the download is not a JSON object."}
	}
	v := doc.Get("openapi")
	if v == nil || v.Kind != String || !strings.HasPrefix(v.Str, "3.") {
		shown := "None"
		if v != nil {
			shown = syncEncode(v, syncInlineStyle)
		}
		return nil, &syncRefusal{fmt.Sprintf("%s: the download is not an OpenAPI 3 document (openapi=%s).", name, shown)}
	}
	if paths := doc.Get("paths"); !syncIsObject(paths) || len(paths.Keys) == 0 {
		return nil, &syncRefusal{name + ": the download has no paths."}
	}
	return doc, nil
}

// syncNaming is an x-didit-sdk object as the sync reads it.
type syncNaming struct {
	node   *Node
	name   string // "resource.method"
	auth   string
	server bool
}

// syncNamingOf reads a naming object; nil and empty values are no naming
// (Python's `x or y` falls through them).
func syncNamingOf(n *Node) *syncNaming {
	if !syncIsObject(n) || len(n.Keys) == 0 {
		return nil
	}
	return &syncNaming{
		node:   n,
		name:   n.Get("resource").Text() + "." + n.Get("method").Text(),
		auth:   n.Get("auth").Text(),
		server: n.Get("server").Truthy(),
	}
}

// syncNextMinor returns the version in which a method deprecated in version
// is deleted.
func syncNextMinor(version string) string {
	m := regexp.MustCompile(`^(\d+)\.(\d+)`).FindStringSubmatch(version)
	if m == nil {
		return "0.1.0"
	}
	minor, err := strconv.Atoi(m[2])
	if err != nil {
		return "0.1.0"
	}
	return fmt.Sprintf("%s.%d.0", m[1], minor+1)
}

// syncReadJSON reads and parses a JSON file of openapi/.
func syncReadJSON(path string) (*Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	n, err := ParseNode(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return n, nil
}

// syncSpecs applies downloads (file name in openapi/ -> bytes) to
// openapiDir and returns whether anything changed and the Markdown summary.
// sdkVersion is the current SDK version (it decides remove_in). Nothing is
// written when the result is unchanged or an error is returned.
func syncSpecs(openapiDir string, downloads map[string][]byte, now time.Time, sdkVersion string) (syncResult, error) {
	now = now.UTC()
	today := now.Format("2006-01-02")
	known := map[string]bool{}
	for _, up := range syncUpstreams {
		known[up.file] = true
	}
	for name := range downloads {
		if !known[name] {
			return syncResult{}, fmt.Errorf("%s is not an upstream document of openapi/", name)
		}
	}

	// Check every download before anything else.
	var ups []syncUpstream
	oldDocs, newDocs := map[string]*Doc{}, map[string]*Doc{}
	var changedFiles []string
	for _, up := range syncUpstreams {
		data, ok := downloads[up.file]
		if !ok {
			continue
		}
		ups = append(ups, up)
		root, err := syncCheckOpenAPI(up.file, data)
		if err != nil {
			return syncResult{}, err
		}
		newDocs[up.file] = &Doc{Name: up.api, Root: root}
		oldBytes, err := os.ReadFile(filepath.Join(openapiDir, up.file))
		if errors.Is(err, fs.ErrNotExist) {
			oldBytes = []byte("{}")
		} else if err != nil {
			return syncResult{}, err
		}
		oldRoot, err := ParseNode(oldBytes)
		if err != nil {
			return syncResult{}, fmt.Errorf("openapi/%s: %w", up.file, err)
		}
		oldDocs[up.file] = &Doc{Name: up.api, Root: oldRoot}
		if !bytes.Equal(oldBytes, data) {
			changedFiles = append(changedFiles, up.file)
		}
	}
	if len(changedFiles) == 0 {
		return syncResult{summary: "No upstream change.\n"}, nil
	}

	removeIn := syncNextMinor(sdkVersion)
	overlayPath := filepath.Join(openapiDir, "overlay.json")
	overlay, err := syncReadJSON(overlayPath)
	if err != nil {
		return syncResult{}, err
	}
	overlayOps := overlay.Get("operations")
	if !syncIsObject(overlayOps) {
		return syncResult{}, fmt.Errorf("%s: operations is not an object", overlayPath)
	}
	oldOverlay := syncCopyObject(overlayOps)
	deprecatedPath := filepath.Join(openapiDir, "deprecated-operations.json")
	deprecated, err := syncReadJSON(deprecatedPath)
	if errors.Is(err, fs.ErrNotExist) {
		deprecated = syncObject()
		deprecated.Set("description", syncString(syncDeprecatedDescription))
	} else if err != nil {
		return syncResult{}, err
	}
	if !syncIsObject(deprecated) {
		return syncResult{}, fmt.Errorf("%s: not a JSON object", deprecatedPath)
	}
	if deprecated.Get("methods") == nil {
		deprecated.Set("methods", syncObject())
	}
	methods := deprecated.Get("methods")
	if !syncIsObject(methods) {
		return syncResult{}, fmt.Errorf("%s: methods is not an object", deprecatedPath)
	}

	var oldOps, newOps syncOpSet
	for _, up := range ups {
		syncCollectOps(&oldOps, up.file, oldDocs[up.file], up.prefix)
		syncCollectOps(&newOps, up.file, newDocs[up.file], up.prefix)
	}
	oldNaming := func(key string) *syncNaming {
		if n := syncNamingOf(oldOps.byKey[key].raw.Get("x-didit-sdk")); n != nil {
			return n
		}
		return syncNamingOf(oldOverlay.Get(key))
	}
	newNaming := func(key string) *syncNaming {
		if n := syncNamingOf(newOps.byKey[key].raw.Get("x-didit-sdk")); n != nil {
			return n
		}
		return syncNamingOf(oldOverlay.Get(key))
	}

	var added, removed, common []string
	for _, key := range newOps.keys {
		if oldOps.byKey[key] == nil {
			added = append(added, key)
		}
	}
	for _, key := range oldOps.keys {
		if newOps.byKey[key] == nil {
			removed = append(removed, key)
		} else {
			common = append(common, key)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(common)
	// Server method name -> the operation that provides it, after and before.
	newServerNames, oldServerNames := map[string]string{}, map[string]string{}
	for _, key := range newOps.keys {
		if n := newNaming(key); n != nil && n.server {
			newServerNames[n.name] = key
		}
	}
	for _, key := range oldOps.keys {
		if n := oldNaming(key); n != nil && n.server {
			oldServerNames[n.name] = key
		}
	}

	// One comparer per pair of documents, so shared components are compared
	// once.
	differs := map[[2]string]*syncDiffer{}
	differFor := func(oldOp, newOp *syncOp) *syncDiffer {
		k := [2]string{oldOp.file, newOp.file}
		if differs[k] == nil {
			differs[k] = newSyncDiffer(oldOp.doc, newOp.doc)
		}
		return differs[k]
	}

	lines := []string{
		fmt.Sprintf("Spec sync %s. Upstream files changed: %s.", today, strings.Join(changedFiles, ", ")),
		"",
	}
	var breaking []string
	kept := map[string]bool{}

	// keep records a method that would disappear as deprecated until
	// removeIn.
	keep := func(n *syncNaming, key, reason, replacement string) {
		op := oldOps.byKey[key]
		note := fmt.Sprintf("Removed from the API contract on %s; this method is removed in version %s.", today, removeIn)
		if replacement != "" {
			note = fmt.Sprintf("Renamed to %s on %s; this name is removed in version %s.", replacement, today, removeIn)
		}
		operation, components := syncStoreOperation(op)
		e := syncObject()
		e.Set("reason", syncString(reason))
		if replacement != "" {
			e.Set("replacement", syncString(replacement))
		}
		e.Set("note", syncString(note))
		e.Set("since", syncString(today))
		e.Set("deprecated_since", syncString(sdkVersion))
		e.Set("remove_in", syncString(removeIn))
		e.Set("operation_key", syncString(key))
		e.Set("api", syncString(op.doc.Name))
		e.Set("http_method", syncString(op.method))
		e.Set("path", syncString(op.path))
		e.Set("x-didit-sdk", n.node)
		e.Set("operation", operation)
		e.Set("components", components)
		methods.Set(n.name, e)
		kept[n.name] = true
	}

	lines = append(lines, fmt.Sprintf("## Added operations (%d)", len(added)))
	for _, key := range added {
		switch n := newNaming(key); {
		case n == nil:
			lines = append(lines, fmt.Sprintf("- `%s`: **no SDK naming**; add an entry to `openapi/overlay.json` (DESIGN section 4)", key))
		case n.server:
			lines = append(lines, fmt.Sprintf("- `%s` -> `%s`", key, n.name))
		default:
			lines = append(lines, fmt.Sprintf("- `%s` (not exposed by server SDKs: %s)", key, n.auth))
		}
	}
	lines = append(lines, "")

	lines = append(lines, fmt.Sprintf("## Removed operations (%d)", len(removed)))
	for _, key := range removed {
		n := oldNaming(key)
		switch {
		case n == nil:
			lines = append(lines, fmt.Sprintf("- `%s`", key))
		case !n.server:
			lines = append(lines, fmt.Sprintf("- `%s` (not exposed by server SDKs)", key))
		case newServerNames[n.name] != "":
			lines = append(lines, fmt.Sprintf("- `%s` moved to `%s`; `%s` keeps its name", key, newServerNames[n.name], n.name))
		default:
			breaking = append(breaking, fmt.Sprintf("`%s` was removed upstream (%s); kept as a deprecated method until version %s", n.name, key, removeIn))
			lines = append(lines, fmt.Sprintf("- `%s` (`%s`): kept as a deprecated method until version %s, recorded in `openapi/deprecated-operations.json`", key, n.name, removeIn))
			keep(n, key, "removed", "")
		}
		syncDeleteKey(overlayOps, key)
	}
	lines = append(lines, "")

	var renames []string
	for _, key := range common {
		before, after := oldNaming(key), newNaming(key)
		if before == nil || !before.server {
			continue
		}
		if after != nil && after.server && after.name == before.name {
			continue
		}
		if newServerNames[before.name] != "" {
			continue // another operation now provides the name (swapped or moved)
		}
		switch {
		case after == nil:
			// The generator fails until the operation is named again, so this
			// cannot ship silently; the overlay entry decides whether the name
			// survives.
			renames = append(renames, fmt.Sprintf("- `%s` lost its SDK naming: add `%s` back to `openapi/overlay.json` (the generator fails until it is named)", key, before.name))
			breaking = append(breaking, fmt.Sprintf("`%s` (%s) has no SDK naming any more", before.name, key))
		case !after.server:
			// Server SDKs never expose device or end-user token operations
			// (DESIGN 4), and an API key could not call it any more, so the
			// method goes now.
			renames = append(renames, fmt.Sprintf("- `%s` (`%s`) is no longer a server operation (auth %s); the method is removed", key, before.name, after.auth))
			breaking = append(breaking, fmt.Sprintf("`%s` (%s) is removed: the operation now needs %s and server SDKs do not expose it", before.name, key, after.auth))
		default:
			renames = append(renames, fmt.Sprintf("- `%s`: `%s` renamed to `%s`; the old name stays as a deprecated alias until version %s", key, before.name, after.name, removeIn))
			breaking = append(breaking, fmt.Sprintf("`%s` was renamed upstream to `%s` (%s); the old name is a deprecated alias until version %s", before.name, after.name, key, removeIn))
			keep(before, key, "renamed", after.name)
		}
	}
	lines = append(lines, fmt.Sprintf("## Renamed methods (%d)", len(renames)))
	lines = append(lines, renames...)
	lines = append(lines, "")

	// A name that stays but now belongs to another operation: moved to a new
	// path, or reassigned (swapped, or reused by another operation). Compare
	// what the method sends and receives before and after.
	var moves []string
	var stayed []string
	for name := range oldServerNames {
		if newServerNames[name] != "" {
			stayed = append(stayed, name)
		}
	}
	sort.Strings(stayed)
	for _, method := range stayed {
		oldKey, newKey := oldServerNames[method], newServerNames[method]
		if oldKey == newKey {
			continue
		}
		o, n := oldOps.byKey[oldKey], newOps.byKey[newKey]
		notes, isBreaking := differFor(o, n).describeChange(o.node, n.node)
		if len(notes) == 1 && notes[0] == syncUnchangedNote {
			notes = []string{"same parameters, body and response"}
		}
		detail := strings.Join(notes, "; ")
		if newOps.byKey[oldKey] != nil {
			moves = append(moves, fmt.Sprintf("- `%s` **(breaking)** now calls `%s` instead of `%s` (which is still in the API): %s", method, newKey, oldKey, detail))
			breaking = append(breaking, fmt.Sprintf("`%s` now calls `%s` instead of `%s`, a different operation", method, newKey, oldKey))
			continue
		}
		marker := ""
		if isBreaking {
			marker = " **(breaking)**"
		}
		moves = append(moves, fmt.Sprintf("- `%s`%s moved from `%s` to `%s`: %s", method, marker, oldKey, newKey, detail))
		if isBreaking {
			breaking = append(breaking, fmt.Sprintf("`%s` moved from `%s` to `%s`: %s", method, oldKey, newKey, strings.Join(syncBreakingNotes(notes), "; ")))
		}
	}
	lines = append(lines, fmt.Sprintf("## Moved or reassigned methods (%d)", len(moves)))
	lines = append(lines, moves...)
	lines = append(lines, "")

	for _, method := range syncSortedKeys(methods) {
		if key := newServerNames[method]; key != "" && !kept[method] {
			syncDeleteKey(methods, method)
			lines = append(lines, fmt.Sprintf("- `%s` is provided again by `%s`; its deprecation entry was dropped.", method, key))
		}
	}

	componentsSame := map[string]bool{}
	for _, up := range ups {
		componentsSame[up.file] = syncEqual(oldDocs[up.file].Root.Get("components"), newDocs[up.file].Root.Get("components"))
	}
	var changes []string
	for _, key := range common {
		o, n := oldOps.byKey[key], newOps.byKey[key]
		same := syncEqual(o.node, n.node)
		if same && componentsSame[o.file] {
			continue
		}
		notes, isBreaking := differFor(o, n).describeChange(o.node, n.node)
		if same && len(notes) == 1 && notes[0] == syncUnchangedNote {
			continue
		}
		marker := ""
		if isBreaking {
			marker = " **(breaking)**"
		}
		changes = append(changes, fmt.Sprintf("- `%s`%s: %s", key, marker, strings.Join(notes, "; ")))
		if isBreaking {
			breaking = append(breaking, fmt.Sprintf("`%s`: %s", key, strings.Join(syncBreakingNotes(notes), "; ")))
		}
	}
	lines = append(lines, fmt.Sprintf("## Changed operations (%d)", len(changes)))
	lines = append(lines, changes...)
	lines = append(lines, "")

	lines = append(lines, "## Schemas")
	for _, up := range ups {
		oldSchemas := oldDocs[up.file].Root.Path("components", "schemas")
		newSchemas := newDocs[up.file].Root.Path("components", "schemas")
		var addedS, goneS, keptS, changedS []string
		for _, name := range syncSortedKeys(syncOrEmpty(newSchemas)) {
			if oldSchemas.Get(name) == nil {
				addedS = append(addedS, name)
			}
		}
		for _, name := range syncSortedKeys(syncOrEmpty(oldSchemas)) {
			nv := newSchemas.Get(name)
			switch {
			case nv == nil:
				goneS = append(goneS, name)
				if syncKeptByDeprecated(methods, up.api, name) {
					keptS = append(keptS, name)
				} else {
					breaking = append(breaking, fmt.Sprintf("%s: schema `%s` removed (its type leaves package didit)", up.file, name))
				}
			case !syncEqual(oldSchemas.Get(name), nv):
				changedS = append(changedS, name)
			}
		}
		for _, group := range []struct {
			label string
			names []string
		}{
			{"added", addedS}, {"removed", goneS}, {"changed", changedS},
			{"removed upstream but kept for the deprecated methods that use them", keptS},
		} {
			if len(group.names) > 0 {
				lines = append(lines, fmt.Sprintf("- %s %s: %s", up.file, group.label, syncCodeList(group.names)))
			}
		}
	}
	lines = append(lines, "")

	// Overlay entries that upstream now carries: an identical one is dropped;
	// a different one is kept, so that someone looks at it, and has no effect
	// (upstream x-didit-sdk wins in the generator).
	var overlayLines []string
	dropped := 0
	for _, key := range append([]string(nil), overlayOps.Keys...) {
		op := newOps.byKey[key]
		if op == nil {
			continue
		}
		upstream := op.raw.Get("x-didit-sdk")
		if upstream == nil || upstream.Kind == Null {
			continue
		}
		if syncEqual(upstream, overlayOps.Get(key)) {
			syncDeleteKey(overlayOps, key)
			dropped++
			continue
		}
		overlayLines = append(overlayLines, fmt.Sprintf(
			"- `%s`: upstream x-didit-sdk %s differs from the overlay entry %s; upstream wins, and the overlay entry is kept for review: delete it, or ask for the upstream value to be fixed.",
			key, syncEncode(upstream, syncSortedInlineStyle), syncEncode(overlayOps.Get(key), syncSortedInlineStyle)))
	}
	if dropped > 0 {
		overlayLines = append(overlayLines, fmt.Sprintf("Dropped %d overlay entries that upstream now carries.", dropped))
	}
	if len(overlayLines) > 0 {
		lines = append(lines, "## Overlay")
		lines = append(lines, overlayLines...)
		lines = append(lines, "")
	}

	breakingText := "None detected."
	if len(breaking) > 0 {
		breakingText = "- " + strings.Join(breaking, "\n- ")
	}
	lines = append(lines[:2:2], append([]string{"## Breaking changes", breakingText, ""}, lines[2:]...)...)

	// Everything is computed; write the files.
	source, err := syncReadJSON(filepath.Join(openapiDir, "SOURCE.json"))
	if errors.Is(err, fs.ErrNotExist) {
		source = syncObject()
	} else if err != nil {
		return syncResult{}, err
	}
	if !syncIsObject(source) {
		return syncResult{}, fmt.Errorf("openapi/SOURCE.json: not a JSON object")
	}
	type output struct {
		name string
		data []byte
	}
	var outputs []output
	for _, up := range ups {
		data := downloads[up.file]
		sum := sha256.Sum256(data)
		entry := syncObject()
		entry.Set("url", syncString(up.url))
		entry.Set("sha256", syncString(hex.EncodeToString(sum[:])))
		entry.Set("fetched_at", syncString(now.Format("2006-01-02T15:04:05Z")))
		source.Set(up.file, entry)
		outputs = append(outputs, output{up.file, data})
	}
	// Same layout as the seeded files (one-space indent, no trailing
	// newline), so diffs stay small.
	outputs = append(outputs,
		output{"SOURCE.json", []byte(syncEncode(source, syncFileStyle))},
		output{"overlay.json", []byte(syncEncode(overlay, syncFileStyle))},
		output{"deprecated-operations.json", []byte(syncEncode(deprecated, syncFileStyle) + "\n")},
	)
	for _, o := range outputs {
		if err := os.WriteFile(filepath.Join(openapiDir, o.name), o.data, 0o644); err != nil {
			return syncResult{}, err
		}
	}
	return syncResult{changed: true, summary: strings.TrimRight(strings.Join(lines, "\n"), " \t\r\n") + "\n"}, nil
}

// syncBreakingNotes returns the notes flagged as breaking.
func syncBreakingNotes(notes []string) []string {
	var out []string
	for _, n := range notes {
		if strings.HasSuffix(n, "(breaking)") {
			out = append(out, n)
		}
	}
	return out
}

func syncCodeList(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = "`" + n + "`"
	}
	return strings.Join(quoted, ", ")
}

// syncKeptByDeprecated reports whether a deprecated method of api keeps the
// component schema name alive (the generator adds it from the entry).
func syncKeptByDeprecated(methods *Node, api, name string) bool {
	for _, m := range methods.ObjKeys() {
		e := methods.Get(m)
		entryAPI := e.Get("api").Text()
		if entryAPI == "" { // an entry with operation_key only
			entryAPI = "verification"
			if strings.HasPrefix(e.Get("operation_key").Text(), "account ") {
				entryAPI = "account"
			}
		}
		if entryAPI == api && e.Path("components", "schemas", name) != nil {
			return true
		}
	}
	return false
}

// syncStoreOperation returns what openapi/deprecated-operations.json keeps
// of an operation so that the generator rebuilds its method exactly, without
// the document: the operation with the path-level parameters merged in the
// order the generator reads them, references to components left intact,
// other local references inlined (the generator treats them as inline
// schemas), and an OpenAPI components object holding every component the
// operation reaches, transitively.
func syncStoreOperation(op *syncOp) (*Node, *Node) {
	st := &syncStore{doc: op.doc, comps: map[string]map[string]*Node{}, seen: map[string]bool{}}
	node := op.raw
	if params := syncMergedParams(op); params != nil {
		node = syncCopyObject(op.raw)
		node.Set("parameters", params)
	}
	node = st.rewrite(node, 0)
	for len(st.queue) > 0 {
		c := st.queue[0]
		st.queue = st.queue[1:]
		if target := op.doc.Root.Path("components", c[0], c[1]); target != nil {
			st.comps[c[0]][c[1]] = st.rewrite(target, 0)
		}
	}
	comps := syncObject()
	var sections []string
	for s := range st.comps {
		sections = append(sections, s)
	}
	sort.Strings(sections)
	for _, s := range sections {
		var names []string
		for n, v := range st.comps[s] {
			if v != nil {
				names = append(names, n)
			}
		}
		if len(names) == 0 {
			continue
		}
		sort.Strings(names)
		sec := syncObject()
		for _, n := range names {
			sec.Set(n, st.comps[s][n])
		}
		comps.Set(s, sec)
	}
	return node, comps
}

// syncMergedParams returns the operation's parameters followed by the
// path-level ones it does not override (the order and precedence of
// Operation.Parameters), or nil when the path item has none.
func syncMergedParams(op *syncOp) *Node {
	shared := op.item.Get("parameters")
	if shared == nil || len(shared.Arr) == 0 {
		return nil
	}
	seen := map[string]bool{}
	key := func(p *Node) string {
		p = op.doc.Resolve(p)
		return p.Get("in").Text() + ":" + p.Get("name").Text()
	}
	var out []*Node
	for _, p := range op.raw.Get("parameters").Elems() {
		seen[key(p)] = true
		out = append(out, p)
	}
	for _, p := range shared.Arr {
		if k := key(p); !seen[k] {
			seen[k] = true
			out = append(out, p)
		}
	}
	return syncArray(out)
}

// syncStore copies an operation and the components it reaches.
type syncStore struct {
	doc   *Doc
	comps map[string]map[string]*Node // section -> name -> stored copy
	seen  map[string]bool             // "section/name" already queued
	queue [][2]string
}

// syncComponentOf returns the section and name of a reference into
// #/components/<section>/<name> (possibly deeper inside it).
func syncComponentOf(ref string) (string, string, bool) {
	rest, ok := strings.CutPrefix(ref, "#/components/")
	if !ok {
		return "", "", false
	}
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	unescape := func(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "~1", "/"), "~0", "~") }
	return unescape(parts[0]), unescape(parts[1]), true
}

// rewrite returns n with the components it references queued and other
// local references inlined. It copies only what changes. inlined counts
// nested inlining, to stop reference cycles.
func (st *syncStore) rewrite(n *Node, inlined int) *Node {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case Array:
		var out []*Node
		for i, v := range n.Arr {
			w := st.rewrite(v, inlined)
			if w != v && out == nil {
				out = append(make([]*Node, 0, len(n.Arr)), n.Arr[:i]...)
			}
			if out != nil {
				out = append(out, w)
			}
		}
		if out == nil {
			return n
		}
		return syncArray(out)
	case Object:
		if ref := n.Get("$ref"); ref != nil && ref.Kind == String {
			if section, name, ok := syncComponentOf(ref.Str); ok {
				if k := section + "/" + name; !st.seen[k] {
					st.seen[k] = true
					if st.comps[section] == nil {
						st.comps[section] = map[string]*Node{}
					}
					st.comps[section][name] = nil
					st.queue = append(st.queue, [2]string{section, name})
				}
			} else if target := st.doc.Lookup(ref.Str); target != nil && inlined < 32 {
				// A reference elsewhere in the document (for example into
				// another operation's response), which may not survive: inline
				// it like the Python reference does.
				in := st.rewrite(target, inlined+1)
				if len(n.Keys) == 1 || !syncIsObject(in) {
					return in
				}
				out := syncCopyObject(in)
				for _, k := range n.Keys {
					if k != "$ref" {
						out.Set(k, st.rewrite(n.Get(k), inlined))
					}
				}
				return out
			}
		}
		var out *Node
		for _, k := range n.Keys {
			v := n.Get(k)
			if w := st.rewrite(v, inlined); w != v {
				if out == nil {
					out = syncCopyObject(n)
				}
				out.Set(k, w)
			}
		}
		if out == nil {
			return n
		}
		return out
	}
	return n
}

// syncSemver is a parsed semantic version.
type syncSemver struct {
	core [3]int
	pre  []string
}

var syncSemverRe = regexp.MustCompile(`^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$`)

func syncParseSemver(s string) (syncSemver, error) {
	m := syncSemverRe.FindStringSubmatch(s)
	if m == nil {
		return syncSemver{}, fmt.Errorf("%q is not a semantic version (MAJOR.MINOR.PATCH)", s)
	}
	var v syncSemver
	for i := 0; i < 3; i++ {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return syncSemver{}, fmt.Errorf("%q: %w", s, err)
		}
		v.core[i] = n
	}
	if m[4] != "" {
		v.pre = strings.Split(m[4], ".")
	}
	return v, nil
}

// syncCompareVersions compares two semantic versions by precedence (build
// metadata ignored, a pre-release before its release): -1, 0 or 1.
func syncCompareVersions(a, b string) (int, error) {
	va, err := syncParseSemver(a)
	if err != nil {
		return 0, err
	}
	vb, err := syncParseSemver(b)
	if err != nil {
		return 0, err
	}
	for i := 0; i < 3; i++ {
		if va.core[i] != vb.core[i] {
			return syncSign(va.core[i] - vb.core[i]), nil
		}
	}
	switch {
	case len(va.pre) == 0 && len(vb.pre) == 0:
		return 0, nil
	case len(va.pre) == 0:
		return 1, nil
	case len(vb.pre) == 0:
		return -1, nil
	}
	for i := 0; i < len(va.pre) && i < len(vb.pre); i++ {
		x, y := va.pre[i], vb.pre[i]
		nx, errx := strconv.Atoi(x)
		ny, erry := strconv.Atoi(y)
		switch {
		case errx == nil && erry == nil:
			if nx != ny {
				return syncSign(nx - ny), nil
			}
		case errx == nil:
			return -1, nil // numeric identifiers come first
		case erry == nil:
			return 1, nil
		case x != y:
			if x < y {
				return -1, nil
			}
			return 1, nil
		}
	}
	return syncSign(len(va.pre) - len(vb.pre)), nil
}

func syncSign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}
