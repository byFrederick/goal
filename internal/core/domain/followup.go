package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxFollowUps is how many entries of an output's follow-ups field goald reads (ADR 0040
// decision 5); the rest are ignored.
const MaxFollowUps = 10

// MaxFollowUpText is a follow-up's most characters; longer text is cut.
const MaxFollowUpText = 2000

// maxFollowUpFactory is the most characters of a for's factory the store keeps (the for_factory
// column's bound); a longer one sends the entry to triage.
const maxFollowUpFactory = 100

// maxFollowUpReason is a follow-up's reason's most characters; it may quote the entry.
const maxFollowUpReason = 500

// ReasonFollowUpNoText is why an entry that is neither a string nor an object with text is
// dropped.
const ReasonFollowUpNoText = "the entry is not a string or an object with text"

// FollowUpID names one kept follow-up.
type FollowUpID string

// FollowUpTarget is where a waiting follow-up goes (ADR 0040 decision 5).
type FollowUpTarget string

const (
	// FollowUpNote is a note on an open item (For).
	FollowUpNote FollowUpTarget = "note"
	// FollowUpStart is an item of its own, filed when the work lands (Start).
	FollowUpStart FollowUpTarget = "start"
	// FollowUpTriage goes to the factory's triage item, for the architect.
	FollowUpTriage FollowUpTarget = "triage"
)

// FollowUpState is a kept follow-up's state (ADR 0040 decision 6).
type FollowUpState string

const (
	// FollowUpWaiting has a Target it will go to.
	FollowUpWaiting FollowUpState = "waiting"
	// FollowUpDelivered was delivered as a note.
	FollowUpDelivered FollowUpState = "delivered"
	// FollowUpFiled became an item (FiledItem).
	FollowUpFiled FollowUpState = "filed"
	// FollowUpDropped goes nowhere, for Reason.
	FollowUpDropped FollowUpState = "dropped"
)

// FollowUpPlan is a follow-up's start: when the item it becomes may start ({at, in, after}, as
// StartRequest), its title and optionally its type, lane, repositories and the factory to file it
// in (a metadata.name, "" for the source's). It is checked, not resolved: In counts from when the
// work lands.
type FollowUpPlan struct {
	At           string   `json:"at,omitempty"`
	In           string   `json:"in,omitempty"`
	After        []string `json:"after,omitempty"`
	Title        string   `json:"title"`
	Type         string   `json:"type,omitempty"`
	Lane         string   `json:"lane,omitempty"`
	Repositories []string `json:"repositories,omitempty"`
	Factory      string   `json:"factory,omitempty"`
}

// FollowUp is one follow-up an accepted output wrote, kept on its source item (ADR 0040 decision
// 6). FollowUpsFrom sets what the output says; the engine sets its source run, station and
// agent, and the store the rest.
type FollowUp struct {
	ID        FollowUpID
	FactoryID FactoryID
	// ItemID is the source item; RunID, Station and Agent the run that wrote it.
	ItemID  ItemID
	RunID   RunID
	Station string
	Agent   string
	Text    string
	// Target is where a waiting follow-up goes; "" when it is dropped. For is set for a note and
	// Start for a start.
	Target FollowUpTarget
	For    *ItemRef
	Start  *FollowUpPlan
	State  FollowUpState
	// Reason is why a dropped follow-up goes nowhere, or why one for triage could not go where
	// it said; "" otherwise.
	Reason string
	// FiledItem is the item a filed follow-up became. FiledFactory and FiledNumber are its
	// factory and number, of any factory of the organization, as reads of an item's follow-ups
	// resolve them; "" and 0 when it is not filed, that item is gone, or the read does not.
	FiledItem    ItemID
	FiledFactory FactoryID
	FiledNumber  int64
	CreatedAt    time.Time
}

// FollowUpKey is the text follow-ups of an item are kept once by: trimmed, case folded and its
// whitespace collapsed to single spaces.
func FollowUpKey(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

// FollowUpsFrom reads the follow-ups of an accepted output from its field (board.flow.outputs.
// followUps), leniently: it never fails, and "" or an output whose field is not a list has none.
// It reads the first MaxFollowUps entries. A string is text for triage. An object with text goes
// by its start ({at, in, after} with a title) when it has one, else by its for (<factory>#<number>),
// else to triage; a start or for that does not parse sends it to triage with the reason. Any other
// entry is dropped, its JSON kept as the text. Entries whose FollowUpKey matches an earlier one's
// are left out.
func FollowUpsFrom(output json.RawMessage, field string) []FollowUp {
	if field == "" {
		return nil
	}

	var v map[string]json.RawMessage
	if json.Unmarshal(output, &v) != nil {
		return nil
	}

	var entries []json.RawMessage
	if json.Unmarshal(v[field], &entries) != nil {
		return nil
	}

	if len(entries) > MaxFollowUps {
		entries = entries[:MaxFollowUps]
	}

	var (
		out  []FollowUp
		seen = map[string]bool{}
	)

	for _, raw := range entries {
		f := followUpFrom(raw)
		if key := FollowUpKey(f.Text); !seen[key] {
			seen[key] = true

			out = append(out, f)
		}
	}

	return out
}

// followUpFrom reads one entry of a follow-ups field.
func followUpFrom(raw json.RawMessage) FollowUp {
	var s string
	if json.Unmarshal(raw, &s) == nil && strings.TrimSpace(s) != "" {
		return FollowUp{Text: followUpText(s), Target: FollowUpTriage, State: FollowUpWaiting}
	}

	var o struct {
		Text  string          `json:"text"`
		For   json.RawMessage `json:"for"`
		Start json.RawMessage `json:"start"`
	}

	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) && json.Unmarshal(raw, &o) == nil && strings.TrimSpace(o.Text) != "" {
		f := FollowUp{Text: followUpText(o.Text), State: FollowUpWaiting}
		f.Target, f.For, f.Start, f.Reason = followUpTarget(o.For, o.Start)
		f.Reason = truncate(f.Reason, maxFollowUpReason)

		return f
	}

	var compact bytes.Buffer
	if json.Compact(&compact, raw) != nil {
		compact.Reset()
		compact.Write(raw)
	}

	return FollowUp{Text: truncate(compact.String(), MaxFollowUpText), State: FollowUpDropped, Reason: ReasonFollowUpNoText}
}

// followUpTarget reads where an object entry goes: its start, else its for, else triage; a start or
// for that does not parse is triage with the reason.
func followUpTarget(forRaw, startRaw json.RawMessage) (FollowUpTarget, *ItemRef, *FollowUpPlan, string) {
	switch {
	case present(startRaw):
		var p FollowUpPlan
		if !bytes.HasPrefix(bytes.TrimSpace(startRaw), []byte("{")) || json.Unmarshal(startRaw, &p) != nil {
			return FollowUpTriage, nil, nil, "start is not an object of at, in, after and title"
		}

		if err := p.check(); err != nil {
			return FollowUpTriage, nil, nil, "start: " + ReasonOf(err)
		}

		return FollowUpStart, nil, &p, ""
	case present(forRaw):
		var s string
		if json.Unmarshal(forRaw, &s) != nil {
			return FollowUpTriage, nil, nil, "for is not an item ref <factory>#<number>"
		}

		ref, ok := ParseItemRef(s)
		if !ok {
			return FollowUpTriage, nil, nil, fmt.Sprintf("for %q is not an item ref <factory>#<number>", s)
		}

		if utf8.RuneCountInString(ref.Factory) > maxFollowUpFactory {
			return FollowUpTriage, nil, nil, fmt.Sprintf("for names a factory longer than %d characters", maxFollowUpFactory)
		}

		return FollowUpNote, &ref, nil, ""
	default:
		return FollowUpTriage, nil, nil, ""
	}
}

// check refuses a start without a title, with both a time and a wait, or with a time, a wait or
// an after ref that does not parse. Every refusal is ErrInvalid.
func (p *FollowUpPlan) check() error {
	p.Title = strings.TrimSpace(p.Title)

	switch {
	case p.Title == "":
		return fmt.Errorf("a start needs a title: %w", ErrInvalid)
	case p.At != "" && p.In != "":
		return fmt.Errorf("a start has a time or a wait, not both: %w", ErrInvalid)
	case p.At != "":
		if _, err := ParseStartTime(p.At, nil); err != nil {
			return err
		}
	case p.In != "":
		if _, err := ParseWait(p.In); err != nil {
			return err
		}
	}

	for _, a := range p.After {
		if _, ok := ParseItemRef(a); !ok {
			return fmt.Errorf("after %q is not an item ref <factory>#<number>: %w", a, ErrInvalid)
		}
	}

	return nil
}

// present reports whether a field was given a value other than null.
func present(raw json.RawMessage) bool {
	return len(raw) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// ReasonOf is err's message without the ErrInvalid it wraps.
func ReasonOf(err error) string {
	if errors.Is(err, ErrInvalid) {
		return strings.TrimSuffix(err.Error(), ": "+ErrInvalid.Error())
	}

	return err.Error()
}

func followUpText(s string) string { return truncate(strings.TrimSpace(s), MaxFollowUpText) }

// MaxNotesADay is how many notes from other factories a factory takes in 24 hours (ADR 0040
// decision 5); a note past it goes to triage. Notes within a factory are not counted.
const MaxNotesADay = 50

// The reasons a note goes to triage instead of the item its for names (RouteNote).
const (
	ReasonNoteNoFactory   = "no factory by that name in the organization"
	ReasonNoteNotAccepted = "the factory does not accept follow-ups from this one (board.flow.followUpsFrom)"
	ReasonNoteNoItem      = "no item with that number"
	ReasonNoteItemClosed  = "the item is done or cancelled"
	ReasonNoteCapped      = "the factory already took 50 notes from other factories in the last 24 hours"
)

// The data keys of an item_note event (NoteData).
const (
	KeyNoteFollowUp    = "follow_up_id"
	KeyNoteFromItem    = "from_item"
	KeyNoteFromItemID  = "from_item_id"
	KeyNoteFromFactory = "from_factory_id"
	KeyNoteStation     = "station"
	KeyNoteAgent       = "agent"
)

// NoteFactory is what delivering a note needs of a factory of the organization: its id, its
// metadata.name and the factories it accepts follow-ups from.
type NoteFactory struct {
	ID            FactoryID
	Name          string
	FollowUpsFrom []FactoryID
}

// NoteFactoryOf is l as a note's target.
func NoteFactoryOf(l Line) NoteFactory {
	return NoteFactory{ID: l.FactoryID, Name: l.Name, FollowUpsFrom: l.Flow.FollowUpsFrom}
}

// NoteFactoryNamed is the factory of fs a for's factory names, by metadata.name or id, as an
// after ref is matched.
func NoteFactoryNamed(fs []NoteFactory, name string) (NoteFactory, bool) {
	for _, f := range fs {
		if f.Name == name || string(f.ID) == name {
			return f, true
		}
	}

	return NoteFactory{}, false
}

// RouteNote is why a note from factory from cannot go to the item its for names, or "" when it
// can (ADR 0040 decisions 5 and 7). to is the factory the for names (nil: none by that name),
// state the item's ("" when the factory has no item with that number) and today how many notes
// from other factories the target factory took in the last 24 hours. A note into another
// factory needs that factory to list from in followUpsFrom and stays under MaxNotesADay; one
// within from needs neither.
func RouteNote(from FactoryID, to *NoteFactory, state ItemState, today int) string {
	switch {
	case to == nil:
		return ReasonNoteNoFactory
	case to.ID != from && !slices.Contains(to.FollowUpsFrom, from):
		return ReasonNoteNotAccepted
	case state == "":
		return ReasonNoteNoItem
	case state == StateDone || state == StateCancelled:
		return ReasonNoteItemClosed
	case to.ID != from && today >= MaxNotesADay:
		return ReasonNoteCapped
	default:
		return ""
	}
}

// NoteTriageReason is the reason a triage follow-up keeps for a note to ref that RouteNote
// refused: the ref, which the row no longer holds, then why.
func NoteTriageReason(ref ItemRef, reason string) string {
	return truncate("for "+ref.String()+": "+reason, maxFollowUpReason)
}

// NoteData is the data of the item_note event a note f from the source item from becomes:
// its text, the follow-up, the source item (ref and id), its factory, and the station and agent
// that wrote it. The engine writes it (actor system:engine), so it is never a person's touch.
func NoteData(f FollowUp, from ItemRef) map[string]any {
	return map[string]any{
		"text": f.Text, KeyNoteFollowUp: string(f.ID), KeyNoteFromItem: from.String(),
		KeyNoteFromItemID: string(f.ItemID), KeyNoteFromFactory: string(f.FactoryID),
		KeyNoteStation: f.Station, KeyNoteAgent: f.Agent, "actor": EngineActor.Ref(),
	}
}
