package domain

import (
	"fmt"
	"strings"
)

// MaxChangedPaths bounds the paths one Changed since your plan entry lists.
const MaxChangedPaths = 20

// shortSHA is how many characters of a commit's SHA a section shows.
const shortSHA = 12

// ChangedSinceEntry is one commit on the default branch since an item's base commit that changed
// files its paths match, as a Changed since your plan section lists it.
type ChangedSinceEntry struct {
	// Ref (<factory>#<number>) and Title name the item whose merged event has the commit's SHA;
	// both are empty when no item of the factory merged it.
	Ref, Title string
	// SHA and Subject are the commit's.
	SHA, Subject string
	// Paths are the changed paths that overlap the item's paths.
	Paths []string
}

// ChangedSinceSection is the `## Changed since your plan` section of a run's feedback (ADR 0015
// decision 5): the entries, oldest first, each by its merged item's ref and title or else by its
// commit, with at most MaxChangedPaths of its paths, and a line when the comparison from base was
// truncated. It states what it lists and nothing else; it is empty without entries.
func ChangedSinceSection(base string, entries []ChangedSinceEntry, truncated bool) string {
	if len(entries) == 0 {
		return ""
	}

	var b strings.Builder

	fmt.Fprintf(&b, "## Changed since your plan\n\nThe commits on the default branch since %s that changed files this item's paths match, oldest first, with those files:\n", short(base))

	for _, e := range entries {
		by := fmt.Sprintf("commit %s %q", short(e.SHA), e.Subject)
		if e.Ref != "" {
			by = fmt.Sprintf("%s %q", e.Ref, e.Title)
		}

		paths := strings.Join(e.Paths[:min(len(e.Paths), MaxChangedPaths)], ", ")
		if more := len(e.Paths) - MaxChangedPaths; more > 0 {
			paths += fmt.Sprintf(" and %d more", more)
		}

		fmt.Fprintf(&b, "\n- %s: %s", by, paths)
	}

	if truncated {
		fmt.Fprintf(&b, "\n\nThe comparison was truncated: the default branch has more commits since %s than the code host returned, and the later ones are not listed.", short(base))
	}

	return b.String()
}

// short is the first shortSHA characters of sha.
func short(sha string) string { return sha[:min(len(sha), shortSHA)] }
