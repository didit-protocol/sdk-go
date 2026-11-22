package main

// The spec sync (docs/DESIGN.md section 3.1):
//
//	go run ./codegen sync -summary spec-sync-summary.md -release spec-sync-release.md \
//	  -commit-message spec-sync-commit.txt
//
// downloads https://docs.didit.me/openapi-25.json, openapi-auth.json and
// openapi-authentication.json and, when any of them changed:
//
//   - checks that each download is an OpenAPI 3 document with paths, and
//     writes nothing otherwise;
//   - writes them byte for byte to openapi/verification.json,
//     openapi/account.json and openapi/authentication.json, and records URL,
//     SHA-256 and fetch time in openapi/SOURCE.json;
//   - deletes the openapi/overlay.json entries of operations that upstream
//     now names with x-didit-sdk, identical or not: the upstream value wins
//     (DESIGN section 3). A different name goes through the rename path
//     below, and a different auth or scope is reported as breaking;
//   - never deletes a public method silently: a server method whose
//     operation was removed, or whose naming changed upstream (a rename), is
//     recorded in openapi/deprecated-operations.json with the operation as it
//     was and every component it reaches, so the generator keeps it, with the
//     same Go names, as a deprecated method until the next minor version
//     (remove_in). An operation whose SDK name reappears on another path
//     counts as moved, not removed;
//   - writes a Markdown summary of added, removed, renamed, moved and changed
//     operations and schemas, compared field by field, that opens with
//     "Breaking changes": every change that can break typed code (DESIGN
//     section 3.1), or "None detected";
//   - classifies the sync: feat when a caller can observe what changed, chore
//     when only documentation changed. Never a major version (DESIGN section
//     13);
//   - writes the release block of the pull request and the message of the
//     sync commit (sync_release.go), which give release-please the type and
//     one line per breaking change for the release notes.
//
// authentication.json is never generated from: its operations belong to the
// hand-written authentication module (DESIGN section 10), whose contract test
// fails until it follows the document. The sync compares it like the others
// but never deprecates one of its methods.
//
// It appends changed=true|false, date=YYYY-MM-DD, type=feat|chore and
// breaking=<count> to $GITHUB_OUTPUT when that is set. The generator (go run
// ./codegen), not the sync, regenerates the code. This started as a port of
// codegen/spec_sync.py of sdk-python.

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/didit-protocol/sdk-go/internal/version"
)

// syncUpstream describes one upstream document of openapi/.
type syncUpstream struct {
	file string // name in openapi/
	url  string
	api  string // Doc.Name: "verification", "account" or "authentication"
	// prefix starts the operation keys of the document (overlay.json keys).
	prefix string
}

// syncUpstreams are the upstream documents, in the order the summary lists
// them (docs/DESIGN.md section 3).
var syncUpstreams = []syncUpstream{
	{file: "verification.json", url: "https://docs.didit.me/openapi-25.json", api: "verification"},
	{file: "account.json", url: "https://docs.didit.me/openapi-auth.json", api: "account", prefix: "account "},
	{file: "authentication.json", url: "https://docs.didit.me/openapi-authentication.json", api: "authentication", prefix: "authentication "},
}

// syncHandWritten are the upstream documents whose operations a hand-written
// module implements (DESIGN section 10): the generator never reads them, and
// the sync never deprecates one of their methods.
var syncHandWritten = map[string]bool{"authentication.json": true}

// syncMaxDownload bounds a download (the verification document is about
// 3 MB today).
const syncMaxDownload = 64 << 20

// syncCommand is one run of `go run ./codegen sync`, with its side effects
// injectable for tests.
type syncCommand struct {
	dir     string // repository root
	summary string // file for the Markdown summary, or ""
	release string // file for the release block of the pull request, or ""
	message string // file for the message of the sync commit, or ""
	fromDir string // read the upstream files from here instead of downloading
	version string // the SDK version (internal/version)
	// githubOutput is the $GITHUB_OUTPUT file, or "".
	githubOutput string
	fetch        func(url string) ([]byte, error)
	now          func() time.Time
	stdout       io.Writer
}

// runSync updates openapi/ from the published contract (docs/DESIGN.md
// section 3.1).
func runSync(args []string) error {
	fs := flag.NewFlagSet("codegen sync", flag.ContinueOnError)
	summary := fs.String("summary", "", "write the Markdown summary to this file")
	release := fs.String("release", "", "write the release block of the pull request to this file (empty when nothing changed)")
	message := fs.String("commit-message", "", "write the message of the sync commit to this file (empty when nothing changed)")
	fromDir := fs.String("from-dir", "", "read verification.json, account.json and authentication.json from this directory instead of downloading them")
	dir := fs.String("dir", ".", "repository root")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments %q", fs.Args())
	}
	client := &http.Client{Timeout: 60 * time.Second}
	cmd := syncCommand{
		dir:          *dir,
		summary:      *summary,
		release:      *release,
		message:      *message,
		fromDir:      *fromDir,
		version:      version.Version,
		githubOutput: os.Getenv("GITHUB_OUTPUT"),
		fetch:        func(url string) ([]byte, error) { return syncFetch(client, url) },
		now:          time.Now,
		stdout:       os.Stdout,
	}
	return cmd.run()
}

func (c syncCommand) run() error {
	downloads := map[string][]byte{}
	for _, up := range syncUpstreams {
		var data []byte
		var err error
		if c.fromDir != "" {
			data, err = os.ReadFile(filepath.Join(c.fromDir, up.file))
		} else {
			data, err = c.fetch(up.url)
		}
		if err != nil {
			return fmt.Errorf("%s: %w (nothing was written)", up.file, err)
		}
		downloads[up.file] = data
	}
	now := c.now().UTC()
	date := now.Format("2006-01-02")
	res, err := syncSpecs(filepath.Join(c.dir, "openapi"), downloads, now, c.version)
	if err != nil {
		return err
	}
	if c.summary != "" {
		if err := os.WriteFile(c.summary, []byte(res.summary), 0o644); err != nil {
			return err
		}
	}
	for _, out := range []struct {
		file   string
		render func(typ, date string, breaking []syncBreaking) string
	}{{c.release, syncReleaseBlock}, {c.message, syncCommitMessage}} {
		if out.file == "" {
			continue
		}
		text := ""
		if res.changed {
			text = out.render(res.typ, date, res.breaking)
		}
		if err := os.WriteFile(out.file, []byte(text), 0o644); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprint(c.stdout, res.summary); err != nil {
		return err
	}
	if c.githubOutput != "" {
		f, err := os.OpenFile(c.githubOutput, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(f, "changed=%t\ndate=%s\ntype=%s\nbreaking=%d\n", res.changed, date, res.typ, len(res.breaking))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// syncFetch downloads url. Anything but 200 is an error, so an error page is
// never mistaken for the contract (syncCheckOpenAPI catches the rest).
func syncFetch(client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "didit-sdk-go spec-sync")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, syncMaxDownload+1))
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	if len(data) > syncMaxDownload {
		return nil, fmt.Errorf("GET %s: the download is larger than %d bytes", url, syncMaxDownload)
	}
	return data, nil
}
