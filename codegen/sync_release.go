package main

import (
	"fmt"
	"strings"
)

// The release block of a spec sync pull request (docs/DESIGN.md sections
// 3.1 and 13). release-please reads the BEGIN_COMMIT_OVERRIDE block of a
// merged pull request instead of its commits, so the release does not depend
// on how the pull request is titled or merged: one "feat(spec)" or
// "chore(spec)" line for the sync, then one nested commit of type
// syncReleaseType per change that can break typed code, which
// release-please-config.json lists under syncReleaseHeading. A feat is a
// minor version; nothing here is ever a major one (no "!", no "BREAKING
// CHANGE:" footer). release-please splits an override into several commits
// only at BEGIN_NESTED_COMMIT blocks, so every line gets one.

// syncReleaseType is the commit type of a change that can break typed code.
const syncReleaseType = "api"

// syncReleaseHeading is the release notes section of syncReleaseType.
const syncReleaseHeading = "API changes that can break typed code"

// syncMaxReleaseLines and syncMaxRelease bound the release lines, so that
// the pull request body (65,536 characters at most) keeps room for the
// summary. The rest is counted in one last line.
const (
	syncMaxReleaseLines = 100
	syncMaxRelease      = 20000
)

// syncReleaseBlock returns the "Release notes" section of the pull request
// body for a sync of type typ ("feat" or "chore") on date.
func syncReleaseBlock(typ, date string, breaking []syncBreaking) string {
	var b strings.Builder
	b.WriteString("## Release notes\n\n")
	if typ == "feat" {
		b.WriteString("`feat(spec)`: callers can observe what changed, so this is a minor release, never a major one (docs/DESIGN.md section 13).")
		if len(breaking) > 0 {
			fmt.Fprintf(&b, " Its release notes list each change that can break typed code under %q.", syncReleaseHeading)
		}
	} else {
		b.WriteString("`chore(spec)`: only documentation changed, so this releases nothing on its own; it ships with the next release.")
	}
	b.WriteString("\n\nrelease-please reads the block below instead of the commits of this pull request. " +
		"Keep it when you edit this description, and merge with **Squash and merge**: " +
		"after a merge commit release-please reads the block once per commit and lists every line twice.\n\n")
	fmt.Fprintf(&b, "BEGIN_COMMIT_OVERRIDE\n%s(spec): sync API spec %s\n", typ, date)
	if typ == "feat" && len(breaking) > 0 {
		b.WriteString("\n")
		size := 0
		for i, br := range breaking {
			line := fmt.Sprintf("%s(spec): %s", syncReleaseType, syncReleaseLine(br.text()))
			if i == syncMaxReleaseLines || size+len(line) > syncMaxRelease {
				line = fmt.Sprintf("%s(spec): %d more, listed in the spec sync pull request", syncReleaseType, len(breaking)-i)
				fmt.Fprintf(&b, "BEGIN_NESTED_COMMIT\n%s\nEND_NESTED_COMMIT\n", line)
				break
			}
			size += len(line)
			fmt.Fprintf(&b, "BEGIN_NESTED_COMMIT\n%s\nEND_NESTED_COMMIT\n", line)
		}
	}
	b.WriteString("END_COMMIT_OVERRIDE\n")
	return b.String()
}

// syncReleaseLine keeps a release line on one line, and from ending the
// block early.
func syncReleaseLine(s string) string {
	s = strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
	for _, marker := range []string{"BEGIN_COMMIT_OVERRIDE", "END_COMMIT_OVERRIDE", "BEGIN_NESTED_COMMIT", "END_NESTED_COMMIT"} {
		s = strings.ReplaceAll(s, marker, strings.ToLower(marker))
	}
	return s
}
