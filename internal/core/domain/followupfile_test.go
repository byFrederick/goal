package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestFollowUpFiling_Landing(t *testing.T) {
	t.Parallel()

	start := FollowUp{Target: FollowUpStart, Start: &FollowUpPlan{Title: "Add an index"}, State: FollowUpWaiting}
	triage := FollowUp{Target: FollowUpTriage, State: FollowUpWaiting}

	tests := []struct {
		name string
		f    FollowUp
		src  Item
		root ItemState
		want FollowUpLanding
	}{
		{"a start of landed work is filed", start, Item{State: StateDone}, StateDone, FollowUpLanding{File: true}},
		{"a triage follow-up of landed work joins triage", triage, Item{State: StateDone}, StateDone, FollowUpLanding{}},
		{"a cancelled source drops it", start, Item{State: StateCancelled}, StateDone, FollowUpLanding{Drop: true, Reason: ReasonFollowUpCancelled}},
		{"a cancelled root drops it", triage, Item{State: StateDone}, StateCancelled, FollowUpLanding{Drop: true, Reason: ReasonFollowUpCancelled}},
		{"three deep drops it", start, Item{State: StateDone, Generation: 3}, StateDone, FollowUpLanding{Drop: true, Reason: ReasonFollowUpThreeDeep}},
		{"two deep still files", start, Item{State: StateDone, Generation: 2}, StateDone, FollowUpLanding{File: true}},
		{"an untrusted start waits for its own triage", start, Item{State: StateDone, Untrusted: true}, StateDone, FollowUpLanding{Untrusted: true}},
		{
			"an untrusted note waits for its own triage",
			FollowUp{Target: FollowUpNote, State: FollowUpWaiting},
			Item{State: StateDone, Untrusted: true},
			StateDone,
			FollowUpLanding{Untrusted: true},
		},
		{
			"untrusted cancelled work drops it", start,
			Item{State: StateCancelled, Untrusted: true},
			StateCancelled,
			FollowUpLanding{Drop: true, Reason: ReasonFollowUpCancelled},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := LandFollowUp(tt.f, tt.src, tt.root); got != tt.want {
				t.Errorf("LandFollowUp = %+v; want %+v", got, tt.want)
			}
		})
	}
}

func TestFollowUpFiling_RouteStart(t *testing.T) {
	t.Parallel()

	other := NoteFactory{ID: "b", Name: "ops"}
	accepting := NoteFactory{ID: "b", Name: "ops", FollowUpsFrom: []FactoryID{"a"}}

	for _, tt := range []struct {
		name string
		to   *NoteFactory
		want string
	}{
		{"its own factory", &NoteFactory{ID: "a"}, ""},
		{"no such factory", nil, ReasonNoteNoFactory},
		{"another that does not list it", &other, ReasonNoteNotAccepted},
		{"another that lists it", &accepting, ""},
	} {
		if got := RouteStart("a", tt.to); got != tt.want {
			t.Errorf("%s: RouteStart = %q; want %q", tt.name, got, tt.want)
		}
	}
}

func TestFollowUpFiling_FileRequest(t *testing.T) {
	t.Parallel()

	landed := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	src := Item{FactoryID: "a", Priority: 2, Generation: 1, Untrusted: true}
	f := FollowUp{
		Text: "The runs table needs an index", Station: stationBuild, Agent: testAgent,
		Start: &FollowUpPlan{In: "2d", Title: "Index runs", Type: fileStory, Lane: fileLaneAPI},
	}

	r := FollowUpFileRequest("a", src, ItemRef{Factory: "software", Number: 513}, f, nil, landed)

	l := fileLine()
	l.FactoryID = "a"

	n, err := File(l, r)
	if err != nil {
		t.Fatalf("File = %v", err)
	}

	if n.Origin != OriginFollowUp || n.CreatedBy != (Actor{}) || n.Class != ClassStandard || n.Priority != 1 ||
		n.Generation != 2 || !n.Untrusted || n.Title != "Index runs" || !n.StartAt.Equal(landed.Add(48*time.Hour)) {
		t.Errorf("filed %+v; want a follow-up by the engine, standard, priority 1, generation 2, untrusted, starting 2 days after landing", n)
	}

	if !strings.HasPrefix(n.Body, f.Text) || !strings.Contains(n.Body, "software#513") {
		t.Errorf("body %q; want the text and the source", n.Body)
	}
}

func TestFollowUpFiling_ClassOnlyForTriggersAndFollowUps(t *testing.T) {
	t.Parallel()

	_, err := File(fileLine(), FileRequest{Origin: OriginPerson, Title: "t", Class: ClassStandard})
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("a person's class: err %v; want ErrInvalid", err)
	}
}

func triageEntries() []TriageEntry {
	e := func(n int64, text string, prio int) TriageEntry {
		return TriageEntry{
			FollowUp: FollowUp{Text: text, Station: stationBuild, Agent: testAgent},
			Source:   ItemRef{Factory: "software", Number: n}, Title: "Source", Type: fileStory, Priority: prio, Generation: int(n % 3),
		}
	}

	return []TriageEntry{e(513, "First", 2), e(514, "Second\nline", 0), e(513, "Third", 2), e(515, "x", 1), e(516, "y", 1), e(517, "z", 1)}
}

func TestTriage_Title(t *testing.T) {
	t.Parallel()

	es := triageEntries()
	if got := TriageTitle(es); got != "Follow-ups from software#513 and 4 more" {
		t.Errorf("TriageTitle = %q", got)
	}

	if got := TriageTitle(es[:1]); got != "Follow-ups from software#513" {
		t.Errorf("TriageTitle of one = %q", got)
	}
}

func TestTriage_PriorityGenerationAndRequest(t *testing.T) {
	t.Parallel()

	es := triageEntries()
	if TriagePriority(es) != -1 || TriageGeneration(es) != 3 {
		t.Errorf("priority %d, generation %d; want -1 and 3", TriagePriority(es), TriageGeneration(es))
	}

	at := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)

	n, err := File(fileLine(), TriageFileRequest(fileLine().FactoryID, es, at))
	if err != nil {
		t.Fatalf("File = %v", err)
	}

	if n.Origin != OriginFollowUp || n.Class != ClassStandard || n.Type != fileStory || !n.StartAt.Equal(at.Add(24*time.Hour)) {
		t.Errorf("triage item %+v; want a standard follow-up of the first source's type starting in 24h", n)
	}
}

func TestTriage_Brief(t *testing.T) {
	t.Parallel()

	es := triageEntries()
	es[0].FollowUp.Reason = "start \"x\": no factory by that name in the organization"

	b := TriageBrief(es)
	if !strings.HasSuffix(b, TriageClosing+"\n") {
		t.Errorf("brief does not end with the closing: %q", b)
	}

	// Grouped by source: the third follow-up, of software#513, comes before software#514's.
	if i, j := strings.Index(b, "> Third"), strings.Index(b, "## software#514"); i < 0 || j < 0 || i > j {
		t.Errorf("brief not grouped by source:\n%s", b)
	}

	for _, want := range []string{"## software#513: Source", "From the backend agent at build:", "> Second\n> line", "It could not go where it said"} {
		if !strings.Contains(b, want) {
			t.Errorf("brief lacks %q:\n%s", want, b)
		}
	}
}

func TestTriage_PlanAtMostThreeParts(t *testing.T) {
	t.Parallel()

	parts := make([]NewPart, 4)
	if err := CheckTriagePlan(Item{Triage: true}, parts); !errors.Is(err, ErrBadPlan) {
		t.Errorf("4 parts: err %v; want ErrBadPlan", err)
	}

	if err := CheckTriagePlan(Item{Triage: true}, parts[:3]); err != nil {
		t.Errorf("3 parts: err %v", err)
	}

	if err := CheckTriagePlan(Item{}, parts); err != nil {
		t.Errorf("not triage: err %v", err)
	}
}

func TestFollowUpFiling_Fileable(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		f      FollowUp
		state  ItemState
		triage bool
		want   bool
	}{
		{"dropped", FollowUp{State: FollowUpDropped}, "", false, true},
		{"waiting", FollowUp{State: FollowUpWaiting}, "", false, false},
		{"in an open triage item", FollowUp{State: FollowUpFiled, Target: FollowUpTriage}, StateQueued, true, false},
		{"in a done triage item", FollowUp{State: FollowUpFiled, Target: FollowUpTriage}, StateDone, true, true},
		{"filed as its own item", FollowUp{State: FollowUpFiled, Target: FollowUpStart}, StateDone, false, false},
	} {
		if got := Fileable(tt.f, tt.state, tt.triage); got != tt.want {
			t.Errorf("%s: Fileable = %t; want %t", tt.name, got, tt.want)
		}
	}
}
