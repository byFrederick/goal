package domain

import (
	"errors"
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
	if n != want {
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
