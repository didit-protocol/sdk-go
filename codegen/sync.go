package main

// The spec sync (docs/DESIGN.md section 3.1):
//
//	go run ./codegen sync -summary spec-sync-summary.md
//
// downloads https://docs.didit.me/openapi-25.json and openapi-auth.json and,
// when either changed:
//
//   - checks that each download is an OpenAPI 3 document with paths, and
//     writes nothing otherwise;
//   - writes them byte for byte to openapi/verification.json and
//     openapi/account.json, and records URL, SHA-256 and fetch time in
//     openapi/SOURCE.json;
//   - drops the openapi/overlay.json entries that upstream now carries
//     identically as x-didit-sdk (an entry that differs is kept, and the
//     summary says so; upstream wins in the generator either way);
//   - never deletes a public method silently: a server method whose
//     operation was removed, or whose naming changed upstream (a rename), is
//     recorded in openapi/deprecated-operations.json with the operation as it
//     was and every component it reaches, so the generator keeps it, with the
//     same Go names, as a deprecated method until the next minor version
//     (remove_in). An operation whose SDK name reappears on another path
//     counts as moved, not removed;
//   - writes a Markdown summary of added, removed, renamed, moved and changed
//     operations and schemas, compared field by field, with every change that
//     can break code written against the SDK listed under "Breaking changes".
//
// It appends changed=true|false and date=YYYY-MM-DD to $GITHUB_OUTPUT when
// that is set. The generator (go run ./codegen), not the sync, regenerates
// the code. This is a port of codegen/spec_sync.py of sdk-python.

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
	api  string // Doc.Name: "verification" or "account"
	// prefix starts the operation keys of the document (overlay.json keys).
	prefix string
}

// syncUpstreams are the upstream documents, in the order the summary lists
// them (docs/DESIGN.md section 3).
var syncUpstreams = []syncUpstream{
	{file: "verification.json", url: "https://docs.didit.me/openapi-25.json", api: "verification"},
	{file: "account.json", url: "https://docs.didit.me/openapi-auth.json", api: "account", prefix: "account "},
}

// syncMaxDownload bounds a download (the verification document is about
// 3 MB today).
const syncMaxDownload = 64 << 20

// syncCommand is one run of `go run ./codegen sync`, with its side effects
// injectable for tests.
type syncCommand struct {
	dir     string // repository root
	summary string // file for the Markdown summary, or ""
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
	fromDir := fs.String("from-dir", "", "read verification.json and account.json from this directory instead of downloading them")
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
	res, err := syncSpecs(filepath.Join(c.dir, "openapi"), downloads, now, c.version)
	if err != nil {
		return err
	}
	if c.summary != "" {
		if err := os.WriteFile(c.summary, []byte(res.summary), 0o644); err != nil {
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
		_, err = fmt.Fprintf(f, "changed=%t\ndate=%s\n", res.changed, now.Format("2006-01-02"))
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
