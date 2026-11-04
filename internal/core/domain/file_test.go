package domain

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

var fileAt = time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)

// The file tests' line: work types, stations and lanes.
const (
	fileStory, fileOutage                 = "story", "outage"
	fileIntake, fileMake, fileSignOff     = "intake", "make", "sign-off"
	fileLaneAPI, fileLaneUI, fileLaneNone = "api", "ui", "infra"
	filePlanner                           = "planner"
)

// fileLine is a line with work types, an expedite type, an inbox and a station routed by lane.
func fileLine() Line {
	return Line{
		FactoryID: "f", WorkTypes: []string{fileStory, fileOutage}, ExpediteTypes: []string{fileOutage},
		Stations: []Station{
			{ID: fileIntake, Kind: KindQueue},
			{ID: fileMake, Kind: KindAgent, Route: map[string]string{fileLaneAPI: "go", fileLaneUI: "ts"}},
			{ID: fileSignOff, Kind: KindHuman},
		},
	}
}

var ada = Actor{Kind: ActorUser, ID: "ada", Name: "Ada"}

// A person's item gets the first work type when it names none, enters at the first station and
// records its origin, via and filer.
func TestFile_PersonEntersAtTheFirstStation(t *testing.T) {
	t.Parallel()

	n, err := File(fileLine(), FileRequest{
		Origin: OriginPerson, Via: ViaCLI, By: ada, Title: "  Add a flag  ", Lane: fileLaneAPI, Priority: 2, At: fileAt,
		Source: ItemFromCLI,
	})
	if err != nil {
		t.Fatal(err)
	}

	want := NewItem{
		FactoryID: "f", Type: fileStory, Lane: fileLaneAPI, Title: "Add a flag", Priority: 2, Station: fileIntake,
		State: StateQueued, Class: ClassStandard, CreatedAt: fileAt, Source: ItemFromCLI,
		Origin: OriginPerson, Via: ViaCLI, CreatedBy: ada,
	}
	if !reflect.DeepEqual(n, want) {
		t.Errorf("File = %+v\nwant %+v", n, want)
	}
}

// An expedite type enters with class expedite; a line without work types takes any type.
func TestFile_TypeAndClassComeFromTheLine(t *testing.T) {
	t.Parallel()

	n, err := File(fileLine(), FileRequest{Origin: OriginPerson, Type: fileOutage, Title: "Down"})
	if err != nil || n.Class != ClassExpedite {
		t.Errorf("incident: class %q, err %v; want expedite", n.Class, err)
	}

	l := fileLine()
	l.WorkTypes = nil

	if n, err := File(l, FileRequest{Origin: OriginFollowUp, Type: "chore", Title: "Tidy", Generation: 1}); err != nil || n.Type != "chore" {
		t.Errorf("no work types: type %q, err %v; want chore", n.Type, err)
	}

	if n, err := File(l, FileRequest{Origin: OriginPerson, Title: "Tidy"}); err != nil || n.Type != "" {
		t.Errorf("no work types, no type: type %q, err %v; want none", n.Type, err)
	}
}

// A trigger places its item at a station of the line, in the state it says.
func TestFile_TriggerPlacesItsItem(t *testing.T) {
	t.Parallel()

	n, err := File(fileLine(), FileRequest{
		Origin: OriginTrigger, By: EngineActor, Title: "Nightly", Station: fileMake, State: StateReady, Trigger: "nightly",
		Source: ItemFromSchedule,
	})
	if err != nil {
		t.Fatal(err)
	}

	if n.Station != fileMake || n.State != StateReady || n.Trigger != "nightly" || n.Origin != OriginTrigger || n.CreatedBy != EngineActor {
		t.Errorf("File = %+v, want at build/ready by the engine", n)
	}

	if n, err := File(fileLine(), FileRequest{Origin: OriginTrigger, Title: "Nightly", Station: fileSignOff}); err != nil || n.State != StateWaitingHuman {
		t.Errorf("at a human station: state %q, err %v; want waiting_human", n.State, err)
	}
}

// A part takes its assembly's type, priority, class, station, generation and untrusted.
func TestFile_PartTakesItsAssemblys(t *testing.T) {
	t.Parallel()

	asm := &Item{ID: "a", Type: fileStory, Priority: 3, Class: ClassStandard, Station: fileMake, Generation: 1, Untrusted: true}
	by := Actor{Kind: ActorAgent, ID: filePlanner, Name: filePlanner}

	n, err := File(fileLine(), FileRequest{
		Origin: OriginPlan, By: by, Assembly: asm, Title: "Part", Lane: fileLaneUI, Body: "Brief", Generation: 1, Untrusted: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if n.ParentID != "a" || n.Type != fileStory || n.Priority != 3 || n.Station != fileMake || n.State != StateQueued ||
		n.Generation != 1 || !n.Untrusted || n.Origin != OriginPlan || n.CreatedBy != by {
		t.Errorf("File = %+v, want a part of a", n)
	}
}

// Every refusal is ErrInvalid.
func TestFile_Refuses(t *testing.T) {
	t.Parallel()

	asm := &Item{ID: "a", Type: fileStory, Station: fileMake}

	tests := []struct {
		name string
		r    FileRequest
	}{
		{"an empty title", FileRequest{Origin: OriginPerson, Title: "  "}},
		{"a type not among the line's", FileRequest{Origin: OriginPerson, Title: "t", Type: "chore"}},
		{"a lane no station routes", FileRequest{Origin: OriginPerson, Title: "t", Lane: fileLaneNone}},
		{"a trigger's station not on the line", FileRequest{Origin: OriginTrigger, Title: "t", Station: "nowhere"}},
		{"an unknown origin", FileRequest{Origin: "fax", Title: "t"}},
		{"no origin", FileRequest{Title: "t"}},
		{"via on a trigger", FileRequest{Origin: OriginTrigger, Via: ViaAPI, Title: "t"}},
		{"an unknown via", FileRequest{Origin: OriginPerson, Via: "pigeon", Title: "t"}},
		{"a negative generation", FileRequest{Origin: OriginFollowUp, Title: "t", Generation: -1}},
		{"a person's item of a later generation", FileRequest{Origin: OriginPerson, Title: "t", Generation: 1}},
		{"an untrusted person's item", FileRequest{Origin: OriginPerson, Title: "t", Untrusted: true}},
		{"a station a person picks", FileRequest{Origin: OriginPerson, Title: "t", Station: fileMake}},
		{"a part without an assembly", FileRequest{Origin: OriginPlan, Title: "t"}},
		{"an assembly on a person's item", FileRequest{Origin: OriginPerson, Title: "t", Assembly: asm}},
		{"a part of another generation", FileRequest{Origin: OriginPlan, Title: "t", Assembly: asm, Generation: 1}},
		{"an untrusted part of a trusted assembly", FileRequest{Origin: OriginPlan, Title: "t", Assembly: asm, Untrusted: true}},
		{"a part of another type", FileRequest{Origin: OriginPlan, Title: "t", Assembly: asm, Type: fileOutage}},
		{"a part of a part", FileRequest{Origin: OriginPlan, Title: "t", Assembly: &Item{ID: "p", ParentID: "a"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := File(fileLine(), tt.r); !errors.Is(err, ErrInvalid) {
				t.Errorf("File = %v, want ErrInvalid", err)
			}
		})
	}
}

// A lane is free on a line whose stations route by none.
func TestFile_AnyLaneWhenNothingRoutes(t *testing.T) {
	t.Parallel()

	l := fileLine()
	l.Stations[1].Route = nil

	if n, err := File(l, FileRequest{Origin: OriginPerson, Title: "t", Lane: fileLaneNone}); err != nil || n.Lane != fileLaneNone {
		t.Errorf("lane %q, err %v; want infra", n.Lane, err)
	}
}

// A wait becomes a time from filing, a time without a zone is read in the person's zone, a done
// after item is met and left out, and an open one is kept once.
func TestFile_StartResolvesAtFiling(t *testing.T) {
	t.Parallel()

	open := &Item{ID: "open", FactoryID: "f", State: StateQueued}
	done := &Item{ID: "done", FactoryID: "f", State: StateDone}

	n, err := File(fileLine(), FileRequest{
		Origin: OriginPerson, By: ada, Title: "Later", At: fileAt,
		Start: &StartRequest{In: "36h", After: []After{{Ref: "f#1", Item: open}, {Ref: "f#2", Item: done}, {Ref: "open", Item: open}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if want := fileAt.Add(36 * time.Hour); !n.StartAt.Equal(want) || !reflect.DeepEqual(n.After, []ItemID{"open"}) {
		t.Errorf("start = %v after %v, want %v after [open]", n.StartAt, n.After, want)
	}

	if !n.StartedAt.IsZero() {
		t.Errorf("StartedAt = %v at the first station, want zero", n.StartedAt)
	}

	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("no tzdata:", err)
	}

	n, err = File(fileLine(), FileRequest{
		Origin: OriginPerson, Title: "At nine", At: fileAt, Start: &StartRequest{At: "2026-11-05T09:00", Zone: berlin},
	})
	if want := time.Date(2026, 11, 5, 8, 0, 0, 0, time.UTC); err != nil || !n.StartAt.Equal(want) {
		t.Errorf("start = %v, %v; want %v (09:00 in Berlin)", n.StartAt, err, want)
	}

	n, err = File(fileLine(), FileRequest{Origin: OriginPerson, Title: "Zoned", At: fileAt, Start: &StartRequest{At: "2026-11-05T09:00:00Z", Zone: berlin}})
	if want := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC); err != nil || !n.StartAt.Equal(want) {
		t.Errorf("start = %v, %v; want %v (its own zone)", n.StartAt, err, want)
	}
}

// Waits take hours, days and weeks.
func TestFile_StartWaits(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]time.Duration{"36h": 36 * time.Hour, "7d": 7 * 24 * time.Hour, "2w": 14 * 24 * time.Hour} {
		if got, err := ParseWait(in); err != nil || got != want {
			t.Errorf("ParseWait(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
}

// A start more than 365 days ahead, with both a time and a wait, a bad wait or time, or an after
// item that is cancelled, unknown or of another factory is refused.
func TestFile_StartRefuses(t *testing.T) {
	t.Parallel()

	for name, s := range map[string]StartRequest{
		"too far":         {In: "53w"},
		"too far at":      {At: "2027-11-05T09:00:00Z"},
		"both":            {At: "2026-11-05", In: "1d"},
		"bad wait":        {In: "3 days"},
		"zero wait":       {In: "0d"},
		"minutes":         {In: "30m"},
		"bad time":        {At: "tomorrow"},
		"cancelled after": {After: []After{{Ref: "f#3", Item: &Item{ID: "c", FactoryID: "f", State: StateCancelled}}}},
		"unknown after":   {After: []After{{Ref: "f#9"}}},
		"other factory":   {After: []After{{Ref: "g#1", Item: &Item{ID: "g", FactoryID: "g", State: StateQueued}}}},
	} {
		_, err := File(fileLine(), FileRequest{Origin: OriginPerson, Title: "Later", At: fileAt, Start: &s})
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: File = %v, want ErrInvalid", name, err)
		}
	}

	if _, err := File(fileLine(), FileRequest{Origin: OriginPerson, Title: "Year", At: fileAt, Start: &StartRequest{In: "52w"}}); err != nil {
		t.Errorf("52 weeks ahead: %v, want accepted", err)
	}
}

// An item a trigger places past the first station has started when it is filed.
func TestFile_StartedAtALaterStation(t *testing.T) {
	t.Parallel()

	n, err := File(fileLine(), FileRequest{Origin: OriginTrigger, Title: "Nightly", Station: fileMake, At: fileAt})
	if err != nil || !n.StartedAt.Equal(fileAt) {
		t.Errorf("StartedAt = %v, %v; want %v", n.StartedAt, err, fileAt)
	}
}
