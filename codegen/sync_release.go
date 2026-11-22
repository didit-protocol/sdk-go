package main

import (
	"fmt"
	"strings"
)

// The release notes of a spec sync (docs/DESIGN.md sections 3.1 and 13).
// release-please reads the BEGIN_COMMIT_OVERRIDE block of a merged pull
// request instead of its commits, so the release does not depend on how the
// pull request is titled: one "feat(spec)" or "chore(spec)" line for the
// sync, then one nested commit of type syncReleaseType per change that can
// break typed code, every one of them, which release-please-config.json
// lists under syncReleaseHeading. A feat is a minor version; nothing here is
// ever a major one (no "!", no "BREAKING CHANGE:" footer). release-please
// splits an override into several commits only at BEGIN_NESTED_COMMIT
// blocks, so every line gets one.
//
// The sync commit's message holds the same lines (syncCommitMessage). When
// the block does not fit the pull request description, the description
// leaves it out and asks to squash with that message, which release-please
// then reads.

// syncReleaseType is the commit type of a change that can break typed code.
const syncReleaseType = "api"

// syncReleaseHeading is the release notes section of syncReleaseType.
const syncReleaseHeading = "API changes that can break typed code"

// syncMaxRelease bounds the release section in bytes, so that the pull
// request description (65,536 characters at most) keeps room for the start
// of the summary.
const syncMaxRelease = 60000

// syncCommitMessage returns the message of the sync commit for a sync of
// type typ ("feat" or "chore") on date: the sync line, then one nested
// commit per breaking change.
func syncCommitMessage(typ, date string, breaking []syncBreaking) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s(spec): sync API spec %s\n", typ, date)
	if len(breaking) > 0 {
		b.WriteString("\n")
		for _, br := range breaking {
			fmt.Fprintf(&b, "BEGIN_NESTED_COMMIT\n%s(spec): %s\nEND_NESTED_COMMIT\n", syncReleaseType, syncReleaseLine(br.text()))
		}
	}
	return b.String()
}

// syncReleaseBlock returns the "Release notes" section of the pull request
// description for a sync of type typ on date. Its prose never names the
// override marker: release-please takes any occurrence as the start of a
// block.
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
	message := syncCommitMessage(typ, date, breaking)
	block := "BEGIN_COMMIT_OVERRIDE\n" + message + "END_COMMIT_OVERRIDE\n"
	if b.Len()+len(block) > syncMaxRelease {
		fmt.Fprintf(&b, "\n\nThe %d lines for %q do not fit this description, so it has no override block and release-please reads the merged commit instead. "+
			"Merge with **Squash and merge** and use the message of this branch's sync commit as the commit message: it holds every line, each as its own nested commit.\n",
			len(breaking), syncReleaseHeading)
		return b.String()
	}
	b.WriteString("\n\nrelease-please reads the block below instead of the commits of this pull request. " +
		"Keep it when you edit this description, and merge with **Squash and merge**: " +
		"after a merge commit release-please reads the block once per commit and lists every line twice.\n\n")
	b.WriteString(block)
	return b.String()
}

// syncReleaseLine keeps a release line on one line, and from starting or
// ending a block.
func syncReleaseLine(s string) string {
	s = strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
	for _, marker := range []string{"BEGIN_COMMIT_OVERRIDE", "END_COMMIT_OVERRIDE", "BEGIN_NESTED_COMMIT", "END_NESTED_COMMIT"} {
		s = strings.ReplaceAll(s, marker, strings.ToLower(marker))
	}
	return s
}
