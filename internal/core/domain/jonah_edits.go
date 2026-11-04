package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrEditDoesNotApply refuses an edit that no longer applies to its file: a replacement's old
// text does not occur exactly once, the file is gone, a new file already exists, or a whole file
// is not the one it was made against.
var ErrEditDoesNotApply = errors.New("the edit no longer applies to its file")

// ProposalEdit is one file a proposal changes (ADR 0039 decision 6): Replacements applied in
// order to the file as it is when the proposal is approved or edited, or, with none, the whole
// Content of a new file. BaseSHA256 is set only on a whole file made against an existing one
// (proposals stored before replacements, and converted by 00101): the file must still have it.
type ProposalEdit struct {
	Path         string
	Replacements []Replacement
	Content      string
	BaseSHA256   string
}

// Whole reports whether e gives its file whole instead of as replacements.
func (e ProposalEdit) Whole() bool { return len(e.Replacements) == 0 }

// proposalEditJSON is a ProposalEdit as 00101's jonah_proposals.edits holds it: replacements, or
// content with base_sha256 when it has one.
type proposalEditJSON struct {
	Path         string        `json:"path"`
	Replacements []Replacement `json:"replacements,omitempty"`
	Content      *string       `json:"content,omitempty"`
	BaseSHA256   string        `json:"base_sha256,omitempty"`
}

// MarshalJSON writes e as {path, replacements} or {path, content[, base_sha256]}.
func (e ProposalEdit) MarshalJSON() ([]byte, error) {
	out := proposalEditJSON{Path: e.Path}
	if e.Whole() {
		out.Content, out.BaseSHA256 = &e.Content, e.BaseSHA256
	} else {
		out.Replacements = e.Replacements
	}

	return json.Marshal(out)
}

// UnmarshalJSON reads what MarshalJSON writes.
func (e *ProposalEdit) UnmarshalJSON(raw []byte) error {
	var in proposalEditJSON
	if err := json.Unmarshal(raw, &in); err != nil {
		return fmt.Errorf("proposal edit: %w", err)
	}

	*e = ProposalEdit{Path: in.Path, Replacements: in.Replacements, BaseSHA256: in.BaseSHA256}
	if in.Content != nil {
		e.Content = *in.Content
	}

	return nil
}

// Apply is the file after e, given its content now (present says whether it exists):
// ErrEditDoesNotApply when e no longer applies.
func (e ProposalEdit) Apply(content []byte, present bool) (string, error) {
	switch {
	case !e.Whole() && !present:
		return "", fmt.Errorf("%s is gone: %w", e.Path, ErrEditDoesNotApply)
	case !e.Whole():
		out, err := ApplyReplacements(string(content), e.Replacements)
		if err != nil {
			return "", fmt.Errorf("%s: %w: %w", e.Path, ErrEditDoesNotApply, err)
		}

		return out, nil
	case e.BaseSHA256 == "" && present:
		return "", fmt.Errorf("%s already exists: %w", e.Path, ErrEditDoesNotApply)
	case e.BaseSHA256 != "" && (!present || HashBlob(content) != BlobHash(e.BaseSHA256)):
		return "", fmt.Errorf("%s is not the file it was made against: %w", e.Path, ErrEditDoesNotApply)
	default:
		return e.Content, nil
	}
}

// size is the bytes of text e holds: its replacements' old and new, or its content.
func (e ProposalEdit) size() int {
	n := len(e.Content)
	for _, r := range e.Replacements {
		n += len(r.Old) + len(r.New)
	}

	return n
}

// texts are the strings e holds.
func (e ProposalEdit) texts() []string {
	out := []string{e.Content}
	for _, r := range e.Replacements {
		out = append(out, r.Old, r.New)
	}

	return out
}

// EditsOverlap reports whether two proposals' edits overlap (ADR 0039 decision 6), so the newer
// supersedes the older: both edit one file and either gives it whole, or an old of one spans
// bytes an old of the other spans in files[path], the file the newer was made against. An old
// that does not occur there spans nothing.
func EditsOverlap(a, b []ProposalEdit, files map[string][]byte) bool {
	for _, ea := range a {
		for _, eb := range b {
			if ea.Path != eb.Path {
				continue
			}

			if ea.Whole() || eb.Whole() {
				return true
			}

			content := string(files[ea.Path])
			for _, sa := range spans(content, ea.Replacements) {
				for _, sb := range spans(content, eb.Replacements) {
					if sa[0] < sb[1] && sb[0] < sa[1] {
						return true
					}
				}
			}
		}
	}

	return false
}

// spans are the byte ranges of rs' olds in content, each where it first occurs.
func spans(content string, rs []Replacement) [][2]int {
	out := make([][2]int, 0, len(rs))

	for _, r := range rs {
		if i := strings.Index(content, r.Old); r.Old != "" && i >= 0 {
			out = append(out, [2]int{i, i + len(r.Old)})
		}
	}

	return out
}

// HiddenCharacter is the first invisible or bidirectional character in s, which can hide
// instructions from the person who reads it (ADR 0039 decision 20): the zero-width characters
// and marks U+200B to U+200F, the bidirectional embeddings and overrides U+202A to U+202E, the
// invisible operators U+2060 to U+2064, the isolates U+2066 to U+2069, the byte order mark U+FEFF
// and the tags U+E0000 to U+E007F.
func HiddenCharacter(s string) (rune, bool) {
	for _, r := range s {
		switch {
		case r >= 0x200B && r <= 0x200F, r >= 0x202A && r <= 0x202E, r >= 0x2060 && r <= 0x2064,
			r >= 0x2066 && r <= 0x2069, r == 0xFEFF, r >= 0xE0000 && r <= 0xE007F:
			return r, true
		}
	}

	return 0, false
}

// HiddenCharacter is the first invisible or bidirectional character in n's title, summary or
// edits, and whether it has one.
func (n NewProposal) HiddenCharacter() (rune, bool) {
	texts := []string{n.Title, n.Summary}
	for _, e := range n.Edits {
		texts = append(texts, e.texts()...)
	}

	for _, s := range texts {
		if r, ok := HiddenCharacter(s); ok {
			return r, true
		}
	}

	return 0, false
}
