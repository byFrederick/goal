package domain

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// fieldFollowUps is the output field the follow-ups tests map.
const fieldFollowUps = "followups"

func TestFollowUpsFrom(t *testing.T) {
	t.Parallel()

	triage := func(text, reason string) FollowUp {
		return FollowUp{Text: text, Target: FollowUpTriage, State: FollowUpWaiting, Reason: reason}
	}

	tests := []struct {
		name  string
		field string
		out   string
		want  []FollowUp
	}{
		{name: "no field mapped", field: "", out: `{"followups":["a"]}`},
		{name: "an output that is not an object", field: fieldFollowUps, out: `[1]`},
		{name: "field absent", field: fieldFollowUps, out: `{"other":["a"]}`},
		{name: "not a list", field: fieldFollowUps, out: `{"followups":"a"}`},
		{
			name: "a string is text only, for triage", field: fieldFollowUps,
			out:  `{"followups":["  Add an index on runs  "]}`,
			want: []FollowUp{triage("Add an index on runs", "")},
		},
		{
			name: "for an open item becomes a note", field: fieldFollowUps,
			out: `{"followups":[{"text":"The API needs this field","for":"software#647"}]}`,
			want: []FollowUp{{
				Text: "The API needs this field", Target: FollowUpNote, State: FollowUpWaiting,
				For: &ItemRef{Factory: "software", Number: 647},
			}},
		},
		{
			name: "start with every field", field: fieldFollowUps,
			out: `{"followups":[{"text":"Remove the old column","start":{"in":"7d","after":["software#648"],` +
				`"title":"Drop source","type":"task","lane":"data","repositories":["acme/goal"],"factory":"ops"}}]}`,
			want: []FollowUp{{
				Text: "Remove the old column", Target: FollowUpStart, State: FollowUpWaiting,
				Start: &FollowUpPlan{
					In: "7d", After: []string{"software#648"}, Title: "Drop source", Type: "task", Lane: "data",
					Repositories: []string{"acme/goal"}, Factory: "ops",
				},
			}},
		},
		{
			name: "start wins over for", field: fieldFollowUps,
			out: `{"followups":[{"text":"Ship later","for":"software#1","start":{"at":"2026-12-01","title":"Ship later"}}]}`,
			want: []FollowUp{{
				Text: "Ship later", Target: FollowUpStart, State: FollowUpWaiting, Start: &FollowUpPlan{At: "2026-12-01", Title: "Ship later"},
			}},
		},
		{
			name: "a target that does not parse goes to triage with the reason", field: fieldFollowUps,
			out: `{"followups":[{"text":"a","for":"#12"},{"text":"b","start":{"in":"soon","title":"B"}},` +
				`{"text":"c","start":{"in":"7d"}},{"text":"d","start":{"at":"2026-12-01","in":"7d","title":"D"}},` +
				`{"text":"e","start":{"after":["nope"],"title":"E"}},{"text":"f","start":"tomorrow"},{"text":"g","for":12},` +
				`{"text":"h","for":"` + strings.Repeat("a", maxFollowUpFactory+1) + `#3"}]}`,
			want: []FollowUp{
				triage("a", `for "#12" is not an item ref <factory>#<number>`),
				triage("b", `start: wait "soon" is not a number of hours, days or weeks, as in 36h, 7d or 2w`),
				triage("c", "start: a start needs a title"),
				triage("d", "start: a start has a time or a wait, not both"),
				triage("e", `start: after "nope" is not an item ref <factory>#<number>`),
				triage("f", "start is not an object of at, in, after and title"),
				triage("g", "for is not an item ref <factory>#<number>"),
				triage("h", fmt.Sprintf("for names a factory longer than %d characters", maxFollowUpFactory)),
			},
		},
		{
			name: "an entry without text is dropped with the reason", field: fieldFollowUps,
			out: `{"followups":[3,{"for":"software#1"},{"text":"  "},"",null,["a"]]}`,
			want: []FollowUp{
				{Text: "3", State: FollowUpDropped, Reason: ReasonFollowUpNoText},
				{Text: `{"for":"software#1"}`, State: FollowUpDropped, Reason: ReasonFollowUpNoText},
				{Text: `{"text":"  "}`, State: FollowUpDropped, Reason: ReasonFollowUpNoText},
				{Text: `""`, State: FollowUpDropped, Reason: ReasonFollowUpNoText},
				{Text: "null", State: FollowUpDropped, Reason: ReasonFollowUpNoText},
				{Text: `["a"]`, State: FollowUpDropped, Reason: ReasonFollowUpNoText},
			},
		},
		{
			name: "text that matches after trimming, case and spacing is kept once", field: fieldFollowUps,
			out:  `{"followups":["Add an index", " add  AN\tindex ", {"text":"ADD AN INDEX","for":"software#1"}, "Add an index on runs"]}`,
			want: []FollowUp{triage("Add an index", ""), triage("Add an index on runs", "")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := FollowUpsFrom(json.RawMessage(tt.out), tt.field)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FollowUpsFrom = %s\nwant %s", show(got), show(tt.want))
			}
		})
	}
}

// Only the first MaxFollowUps entries are read, and text is cut to MaxFollowUpText characters.
func TestFollowUpsFrom_Bounds(t *testing.T) {
	t.Parallel()

	entries := make([]string, MaxFollowUps+5)
	for i := range entries {
		entries[i] = fmt.Sprintf("%q", fmt.Sprintf("follow-up %d", i))
	}

	got := FollowUpsFrom(json.RawMessage(`{"f":[`+strings.Join(entries, ",")+`]}`), "f")
	if len(got) != MaxFollowUps || got[MaxFollowUps-1].Text != fmt.Sprintf("follow-up %d", MaxFollowUps-1) {
		t.Errorf("read %d follow-ups, the last %q; want the first %d", len(got), got[len(got)-1].Text, MaxFollowUps)
	}

	long := FollowUpsFrom(json.RawMessage(`{"f":["`+strings.Repeat("é", MaxFollowUpText+10)+`"]}`), "f")
	if n := len([]rune(long[0].Text)); n != MaxFollowUpText {
		t.Errorf("text of %d characters; want it cut to %d", n, MaxFollowUpText)
	}
}

func TestFollowUpKey(t *testing.T) {
	t.Parallel()

	if a, b := FollowUpKey("  Add AN\n index "), FollowUpKey("add an index"); a != b || a != "add an index" {
		t.Errorf("keys %q and %q; want both %q", a, b, "add an index")
	}
}

func show(fs []FollowUp) string {
	b, _ := json.Marshal(fs)
	return string(b)
}

// The factories and names of the note routing tests.
const (
	noteFrom FactoryID = "f-from"
	noteInto FactoryID = "f-into"
	nameOps            = "ops"
)

func TestRouteNote(t *testing.T) {
	t.Parallel()

	open := &NoteFactory{ID: noteInto, Name: nameOps, FollowUpsFrom: []FactoryID{noteFrom}}
	closed := &NoteFactory{ID: noteInto, Name: nameOps}
	own := &NoteFactory{ID: noteFrom, Name: "dev"}

	cases := []struct {
		name  string
		to    *NoteFactory
		state ItemState
		today int
		want  string
	}{
		{"an open item of a factory that accepts the sender", open, StateQueued, MaxNotesADay - 1, ""},
		{"no factory by the name", nil, StateQueued, 0, ReasonNoteNoFactory},
		{"a factory that does not accept the sender", closed, StateQueued, 0, ReasonNoteNotAccepted},
		{"no item with the number", open, "", 0, ReasonNoteNoItem},
		{"a done item", open, StateDone, 0, ReasonNoteItemClosed},
		{"a cancelled item", open, StateCancelled, 0, ReasonNoteItemClosed},
		{"past the cap", open, StateBlocked, MaxNotesADay, ReasonNoteCapped},
		{"the sender's own factory needs no followUpsFrom", own, StateInProgress, 0, ""},
		{"the sender's own factory has no cap", own, StateQueued, MaxNotesADay * 3, ""},
		{"a closed item of the sender's own factory", own, StateDone, 0, ReasonNoteItemClosed},
	}

	for _, c := range cases {
		if got := RouteNote(noteFrom, c.to, c.state, c.today); got != c.want {
			t.Errorf("%s: RouteNote = %q; want %q", c.name, got, c.want)
		}
	}
}

func TestNoteFactoryNamed(t *testing.T) {
	t.Parallel()

	fs := []NoteFactory{{ID: noteFrom, Name: "dev"}, {ID: noteInto, Name: nameOps}}

	for name, want := range map[string]FactoryID{nameOps: noteInto, string(noteFrom): noteFrom, "unknown": ""} {
		got, ok := NoteFactoryNamed(fs, name)
		if ok != (want != "") || got.ID != want {
			t.Errorf("NoteFactoryNamed(%q) = %+v, %v; want %q", name, got, ok, want)
		}
	}
}

func TestFollowUpNoteData(t *testing.T) {
	t.Parallel()

	f := FollowUp{ID: "fu-1", FactoryID: noteFrom, ItemID: "i-1", Station: stationBuild, Agent: testAgent, Text: "Mind the index"}

	got := NoteData(f, ItemRef{Factory: "dev", Number: 4})
	want := map[string]any{
		"text": "Mind the index", "follow_up_id": "fu-1", "from_item": "dev#4", "from_item_id": "i-1",
		"from_factory_id": string(noteFrom), "station": stationBuild, "agent": testAgent, "actor": "system:engine",
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("NoteData = %v; want %v", got, want)
	}

	if r := NoteTriageReason(ItemRef{Factory: nameOps, Number: 9}, ReasonNoteNoItem); r != "for ops#9: "+ReasonNoteNoItem {
		t.Errorf("NoteTriageReason = %q", r)
	}
}
