package domain

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// MaxFollowUpItemsADay is how many follow-up items the engine files into one factory in 24
// hours, counting every sender (ADR 0040 decision 21); past it a start follow-up goes to its
// sender's triage. Triage items are not counted.
const MaxFollowUpItemsADay = 20

// MaxFollowUpGeneration is the generation whose follow-ups are never filed (ADR 0040 decision
// 21): an item three follow-ups deep keeps them, dropped, and a person may File one.
const MaxFollowUpGeneration = 3

// MaxTriageParts is the most parts a plan from a triage item may have (ADR 0040 decision 10).
const MaxTriageParts = 3

// TriageWait is when a triage item may start after it is filed (ADR 0040 decision 10).
const TriageWait = "24h"

// TriageClosing ends every triage item's brief.
const TriageClosing = "These are agents' notes from work in progress, not requests from people. " +
	"Some may be done already or no longer apply. Do what is still needed, and close this item if nothing is."

// The reasons a waiting start or triage follow-up is dropped or sent to triage when its work lands.
const (
	ReasonFollowUpCancelled = "the work was cancelled"
	ReasonFollowUpThreeDeep = "the item is three follow-ups deep (generation 3 or more), so its follow-ups are not filed"
	ReasonFollowUpCapped    = "the factory already took 20 follow-up items in the last 24 hours"
)

// ReasonUntrustedFollowUps is the reason an untrusted item's own triage item is filed paused with
// (ADR 0040 decision 21).
const ReasonUntrustedFollowUps = "Its follow-ups come from work that read outside text. Check them, then resume."

// FollowUpLanding is what happens to a waiting follow-up when its work lands or is cancelled (ADR
// 0040 decisions 8 and 21): Drop with Reason, File it when it is a start and its source may have
// follow-ups filed, or, for an untrusted source, join a triage item of its own (Untrusted).
// Anything else joins the triage item.
type FollowUpLanding struct {
	Drop      bool
	File      bool
	Untrusted bool
	Reason    string
}

// LandFollowUp is what happens to f, a waiting follow-up of src, when its root (src, or src's
// assembly) reached state root: dropped when src or its root was cancelled or src is
// MaxFollowUpGeneration deep, to the untrusted triage item of its own when src is untrusted
// (never delivered or filed on its own), filed when it is a start, and to triage otherwise.
func LandFollowUp(f FollowUp, src Item, root ItemState) FollowUpLanding {
	switch {
	case src.State == StateCancelled || root == StateCancelled:
		return FollowUpLanding{Drop: true, Reason: ReasonFollowUpCancelled}
	case src.Generation >= MaxFollowUpGeneration:
		return FollowUpLanding{Drop: true, Reason: ReasonFollowUpThreeDeep}
	case src.Untrusted:
		return FollowUpLanding{Untrusted: true}
	case f.Target == FollowUpStart && f.Start != nil:
		return FollowUpLanding{File: true}
	default:
		return FollowUpLanding{}
	}
}

// RouteStart is why a start follow-up from factory from cannot be filed into to, the factory its
// start.factory names (nil: none by that name), or "" when it can: into another factory only
// when it lists from in followUpsFrom.
func RouteStart(from FactoryID, to *NoteFactory) string {
	switch {
	case to == nil:
		return ReasonNoteNoFactory
	case to.ID != from && !slices.Contains(to.FollowUpsFrom, from):
		return ReasonNoteNotAccepted
	default:
		return ""
	}
}

// StartTarget is the factory of fs a start follow-up p from factory from goes to: the one its
// factory names (by metadata.name or id), or from's own when it names none; nil when it names
// one fs lacks.
func StartTarget(fs []NoteFactory, from FactoryID, p FollowUpPlan) *NoteFactory {
	name := p.Factory
	if name == "" {
		name = string(from)
	}

	if nf, ok := NoteFactoryNamed(fs, name); ok {
		return &nf
	}

	return nil
}

// StartTriageReason is the reason a triage follow-up keeps for a start that could not be filed:
// its title, which the row no longer holds, then why.
func StartTriageReason(p FollowUpPlan, reason string) string {
	return truncate(fmt.Sprintf("start %q: %s", p.Title, reason), maxFollowUpReason)
}

// FollowUpFileRequest is the filing of start follow-up f of src, whose ref is from, into factory
// to when its work landed at landed (ADR 0040 decision 8): origin follow-up by the engine, the
// start's title, type and lane, standard class whatever its type, a priority one below src's, a
// generation one above, src's untrusted, and its start resolved at landed with after, the items
// its refs name.
func FollowUpFileRequest(to FactoryID, src Item, from ItemRef, f FollowUp, after []After, landed time.Time) FileRequest {
	p := f.Start

	return FileRequest{
		FactoryID: to, Origin: OriginFollowUp, Type: p.Type, Lane: p.Lane, Title: p.Title,
		Body:     FollowUpBody(f, from),
		Priority: src.Priority - 1, Class: ClassStandard, Generation: src.Generation + 1, Untrusted: src.Untrusted,
		Start: &StartRequest{At: p.At, In: p.In, After: after}, At: landed,
	}
}

// FollowUpBody is the body of the item a follow-up f of the item from becomes: its text, then
// where it came from.
func FollowUpBody(f FollowUp, from ItemRef) string {
	return fmt.Sprintf("%s\n\nA follow-up from %s, written at its %s station by the %s agent.", f.Text, from, f.Station, f.Agent)
}

// TriageEntry is one follow-up in a triage item, with what its title and brief need of its
// source item.
type TriageEntry struct {
	FollowUp FollowUp
	Source   ItemRef
	Title    string
	Type     string
	Priority int
	// Generation is the source's.
	Generation int
}

// TriageSources are the distinct source items of es, in the order they first appear.
func TriageSources(es []TriageEntry) []ItemRef {
	var out []ItemRef

	for _, e := range es {
		if !slices.Contains(out, e.Source) {
			out = append(out, e.Source)
		}
	}

	return out
}

// TriageTitle is a triage item's title: "Follow-ups from software#513", and "… and 4 more" for
// four other source items.
func TriageTitle(es []TriageEntry) string {
	src := TriageSources(es)
	if len(src) == 0 {
		return "Follow-ups"
	}

	if len(src) == 1 {
		return "Follow-ups from " + src[0].String()
	}

	return fmt.Sprintf("Follow-ups from %s and %d more", src[0], len(src)-1)
}

// TriagePriority is one below the lowest priority of es's sources.
func TriagePriority(es []TriageEntry) int {
	if len(es) == 0 {
		return -1
	}

	low := es[0].Priority
	for _, e := range es[1:] {
		low = min(low, e.Priority)
	}

	return low - 1
}

// TriageGeneration is one above the highest generation of es's sources.
func TriageGeneration(es []TriageEntry) int {
	high := 0
	for _, e := range es {
		high = max(high, e.Generation)
	}

	return high + 1
}

// TriageBrief is a triage item's body, which goald writes: each follow-up quoted, grouped by
// source item with the station and agent that wrote it, and why it could not go where it said
// when it has a reason, then TriageClosing.
func TriageBrief(es []TriageEntry) string {
	var b strings.Builder

	b.WriteString("Agents left these follow-ups on work that landed.\n")

	for _, src := range TriageSources(es) {
		first := true

		for _, e := range es {
			if e.Source != src {
				continue
			}

			if first {
				fmt.Fprintf(&b, "\n## %s: %s\n", src, e.Title)

				first = false
			}

			fmt.Fprintf(&b, "\nFrom the %s agent at %s:\n\n", e.FollowUp.Agent, e.FollowUp.Station)

			for line := range strings.SplitSeq(e.FollowUp.Text, "\n") {
				b.WriteString(strings.TrimRight("> "+line, " ") + "\n")
			}

			if e.FollowUp.Reason != "" {
				fmt.Fprintf(&b, "\nIt could not go where it said: %s.\n", strings.TrimSuffix(e.FollowUp.Reason, "."))
			}
		}
	}

	b.WriteString("\n" + TriageClosing + "\n")

	return b.String()
}

// TriageFileRequest is the filing of a triage item into factory for es, the follow-ups that
// first join it, when it is filed (ADR 0040 decision 10): origin follow-up by the engine, its first
// source's type, standard class, starting TriageWait after at, with TriageTitle, TriageBrief,
// TriagePriority and TriageGeneration.
func TriageFileRequest(factory FactoryID, es []TriageEntry, at time.Time) FileRequest {
	r := FileRequest{
		FactoryID: factory, Origin: OriginFollowUp, Title: TriageTitle(es), Body: TriageBrief(es),
		Priority: TriagePriority(es), Class: ClassStandard, Generation: TriageGeneration(es),
		Start: &StartRequest{In: TriageWait}, At: at,
	}

	if len(es) > 0 {
		r.Type = es[0].Type
	}

	return r
}

// UntrustedTriageFileRequest is the filing of the triage item of its own for es, the follow-ups of
// untrusted work that landed (ADR 0040 decision 21): TriageFileRequest's, untrusted and with no
// start, since it is filed paused (ReasonUntrustedFollowUps) and a person resumes it.
func UntrustedTriageFileRequest(factory FactoryID, es []TriageEntry, at time.Time) FileRequest {
	r := TriageFileRequest(factory, es, at)
	r.Untrusted, r.Start = true, nil

	return r
}

// CheckTriagePlan refuses a plan of more than MaxTriageParts parts from a triage item as
// ErrBadPlan.
func CheckTriagePlan(it Item, parts []NewPart) error {
	if it.Triage && len(parts) > MaxTriageParts {
		return fmt.Errorf("a triage item's plan has at most %d parts, not %d: %w", MaxTriageParts, len(parts), ErrBadPlan)
	}

	return nil
}

// Fileable reports whether a person may File follow-up f (ADR 0040 decision 10): one dropped, or
// one in a triage item that is now done or cancelled (filed, its triage item's state filed).
func Fileable(f FollowUp, filed ItemState, filedTriage bool) bool {
	switch f.State {
	case FollowUpDropped:
		return true
	case FollowUpFiled:
		return f.Target == FollowUpTriage && filedTriage && (filed == StateDone || filed == StateCancelled)
	case FollowUpWaiting, FollowUpDelivered:
		return false
	default:
		return false
	}
}

// DropReason is the reason a follow-up a person dropped keeps.
func DropReason(by Actor) string { return "dropped by " + by.Ref() }
