package engine_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

const (
	// appRepo is the repository the base commit tests' lines clone, served by the github connection.
	appRepo = "https://github.com/acme/app"
	// base is the default branch's head when the tests' items are planned.
	base = "c0ffee000001"
	// storeFile is a file under the store paths the tests' items change.
	storeFile = "internal/store/items.go"
	// storePlan splits an item into one part that changes the store.
	storePlan = `{"plan":[{"key":"store","title":"Store","brief":"Add the table.","paths":["internal/store/**"]}]}`
	// touchesStore is a spec output whose item changes the store.
	touchesStore = `{"touches":["internal/store/**"]}`
)

// changedSinceLine is spec (preloading the open work, so it records base commits) then build,
// with flow.assemblies.changedSince on, plans materialized, the touches field mapped to the
// item's paths and the repository on the github connection.
func changedSinceLine() domain.Line {
	l := lineOf(agentStation(stSpec, agArchitect, 2, 2), agentStation(stBuild, agWorker, 2, 2))
	l.Stations[1].Context = []domain.StationContext{domain.ContextInFlight}
	l.Flow.Assemblies.Materialize = true
	l.Flow.Assemblies.ChangedSince = true
	l.Flow.Dependencies.Paths = fieldPaths
	l.Runtime.Repository = domain.Repository{URL: appRepo, Connection: connGitHub}
	l.Connections = []string{connGitHub}

	return l
}

// changedSinceGateLine is changedSinceLine with a review gate after build whose approval merges,
// sending conflicts back to build.
func changedSinceGateLine() domain.Line {
	l := changedSinceLine()
	review := agentStation(stReview, agReviewer, 1, 1)
	review.Gate = &domain.Gate{OnReject: stBuild, MaxRework: 2}
	review.Action = domain.ActionMerge
	l.Stations = slices.Insert(l.Stations, 3, review)

	return l
}

// land puts commits on the default branch of appRepo after base, its head moving to the last.
func (h *harness) land(commits ...ports.Commit) {
	h.codehost.AddCommits(appRepo, commits...)
	h.codehost.SetBranchHead(appRepo, "", commits[len(commits)-1].SHA)
}

// mergedBy makes a done item titled title whose merged event names sha, as an item merged
// earlier would be.
func (h *harness) mergedBy(t *testing.T, title, sha string) domain.Item {
	t.Helper()

	it := h.submit(t, domain.NewItem{Title: title})

	done, err := h.store.Transition(inOrg(t), ports.Transition{
		ItemID: it.ID, From: it.Position(), To: domain.Position{Station: stDone, State: domain.StateDone}, At: h.clock.Now(),
		Events: []domain.Event{{
			FactoryID: it.FactoryID, ItemID: it.ID, At: h.clock.Now(), Kind: domain.EventMerged, Station: stInbox,
			Actor: domain.ActorEngine, Data: map[string]any{"sha": sha, "pull_request": prURL},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	return done
}

// changedSinceOf is the `## Changed since your plan` section of the i-th run's feedback, empty
// when it has none.
func (h *harness) changedSinceOf(i int) string {
	_, section, ok := strings.Cut(h.feedback(i), "## Changed since your plan")
	if !ok {
		return ""
	}

	section, _, _ = strings.Cut(section, "\n\n## ")

	return "## Changed since your plan" + section
}

// ref is the item's <factory>#<number>.
func ref(it domain.Item) string { return domain.ItemRef{Factory: testFactory, Number: it.Number}.String() }

// A station with context: [inflight] records the default branch's head as the item's base
// commit when it finishes it, while changedSince is on and the line has a repository; a later
// station without the context keeps it.
func TestEngine_FinishRecordsBaseCommit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line func(*domain.Line)
		want string
	}{
		{name: "recorded", want: base},
		{name: "setting off", line: func(l *domain.Line) { l.Flow.Assemblies.ChangedSince = false }},
		{name: "no repository", line: func(l *domain.Line) { l.Runtime.Repository = domain.Repository{} }},
		{name: "no code host connection", line: func(l *domain.Line) {
			l.Connections, l.Runtime.Repository.Connection = nil, ""
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			line := changedSinceLine()
			if tt.line != nil {
				tt.line(&line)
			}

			h := newHarness(t, line, porttest.Succeed(touchesStore))
			h.codehost.SetBranchHead(appRepo, "", base)

			it := h.submit(t, domain.NewItem{})
			h.step(t) // spec

			if got := h.item(t, it.ID); got.BaseCommit != tt.want || got.Station != stBuild {
				t.Fatalf("after spec: base commit %q at %s, want %q at build", got.BaseCommit, got.Station, tt.want)
			}

			h.codehost.SetBranchHead(appRepo, "", "c0ffee000002")
			h.step(t) // build, which does not preload the open work

			if got := h.item(t, it.ID); got.BaseCommit != tt.want || got.Station != stDone {
				t.Errorf("after build: base commit %q at %s, want %q at done", got.BaseCommit, got.Station, tt.want)
			}
		})
	}
}

// When the code host cannot say the default branch's head, the item finishes with no base
// commit, and the log says why.
func TestEngine_BaseCommitCodeHostErrorStillFinishes(t *testing.T) {
	t.Parallel()

	h := newHarness(t, changedSinceLine(), porttest.Succeed(touchesStore)) // no branch head: BranchHead fails

	it := h.submit(t, domain.NewItem{})
	h.step(t)

	got := h.item(t, it.ID)
	if got.BaseCommit != "" || got.Position() != (domain.Position{Station: stBuild, State: domain.StateQueued}) {
		t.Errorf("item at %+v with base commit %q, want queued at build with none", got.Position(), got.BaseCommit)
	}

	if logs := h.logs.String(); !strings.Contains(logs, "base commit not recorded") {
		t.Errorf("logs do not say the base commit was not recorded:\n%s", logs)
	}
}

// A plan's parts inherit the base commit their assembly was planned against.
func TestEngine_PartsInheritBaseCommit(t *testing.T) {
	t.Parallel()

	h := newHarness(t, changedSinceLine(), porttest.Succeed(storePlan))
	h.codehost.SetBranchHead(appRepo, "", base)

	it := h.submit(t, domain.NewItem{})
	h.step(t)

	parts := h.parts(t, it.ID).Items
	if len(parts) != 1 {
		t.Fatalf("parts %+v, want one", parts)
	}

	if parts[0].BaseCommit != base || h.item(t, it.ID).BaseCommit != base {
		t.Errorf("assembly base commit %q, part's %q; want both %q", h.item(t, it.ID).BaseCommit, parts[0].BaseCommit, base)
	}
}

// A part claimed after main gained commits under its paths hears them: one an item merged by
// that item's ref and title, another by its subject, each with only the files under its paths.
func TestEngine_ClaimGetsChangedSinceYourPlan(t *testing.T) {
	t.Parallel()

	h := newHarness(t, changedSinceLine(), porttest.Succeed(storePlan))
	h.codehost.SetBranchHead(appRepo, "", base)

	it := h.submit(t, domain.NewItem{Title: "Plan the export"})
	h.step(t) // it plans its part at base

	other := h.mergedBy(t, "Rename the items table", "c0ffee000002")
	h.land(
		ports.Commit{SHA: base, Subject: "Start"},
		ports.Commit{SHA: "c0ffee000002", Subject: "squashed", Files: []string{storeFile, "docs/items.md"}},
		ports.Commit{SHA: "c0ffee000003", Subject: "Fix the store's lock", Files: []string{"internal/store/lock.go"}},
	)

	h.step(t) // the part runs spec

	if got := h.worker.Runs()[1].Sandbox.ItemID; got != h.parts(t, it.ID).Items[0].ID {
		t.Fatalf("second run is of %s, want the part's", got)
	}

	section := h.changedSinceOf(1)
	for _, want := range []string{
		fmt.Sprintf("- %s %q: %s", ref(other), other.Title, storeFile),
		`- commit c0ffee000003 "Fix the store's lock": internal/store/lock.go`,
	} {
		if !strings.Contains(section, want) {
			t.Errorf("section lacks %q:\n%s", want, section)
		}
	}

	if strings.Contains(section, "docs/items.md") {
		t.Errorf("section lists a file outside the part's paths:\n%s", section)
	}

	if strings.Contains(h.feedback(0), "## Changed since your plan") {
		t.Errorf("the planning run, with no base commit, has the section:\n%s", h.feedback(0))
	}
}

// Commits that change nothing under the item's paths add nothing, and with none that do there is
// no section; a comparison the code host cannot make leaves it out with a log line, and the run
// goes ahead.
func TestEngine_ChangedSinceSkipsUnrelatedCommits(t *testing.T) {
	t.Parallel()

	h := newHarness(t, changedSinceLine(), porttest.Succeed(touchesStore))
	h.codehost.SetBranchHead(appRepo, "", base)

	h.submit(t, domain.NewItem{})
	h.step(t) // spec, at base

	h.mergedBy(t, "Update the docs", "c0ffee000002")
	h.land(ports.Commit{SHA: base}, ports.Commit{SHA: "c0ffee000002", Subject: "docs", Files: []string{"docs/items.md", "web/src/app.tsx"}})

	h.step(t) // build

	if f := h.feedback(1); strings.Contains(f, "## Changed since your plan") {
		t.Errorf("build's feedback has the section for commits outside its paths:\n%s", f)
	}
}

func TestEngine_ChangedSinceCodeHostErrorLeavesItOut(t *testing.T) {
	t.Parallel()

	h := newHarness(t, changedSinceLine(), porttest.Succeed(touchesStore))
	h.codehost.SetBranchHead(appRepo, "", base)

	it := h.submit(t, domain.NewItem{})
	h.step(t) // spec, at base

	h.codehost.SetBranchHead(appRepo, "", "c0ffee000002") // no history: Compare fails
	h.step(t)

	if n := len(h.worker.Runs()); n != 2 {
		t.Fatalf("%d runs, want build to have run", n)
	}

	if f := h.feedback(1); strings.Contains(f, "## Changed since your plan") {
		t.Errorf("build's feedback has the section though the comparison failed:\n%s", f)
	}

	if got := h.item(t, it.ID).Station; got != stDone {
		t.Errorf("item at %s, want done", got)
	}

	if logs := h.logs.String(); !strings.Contains(logs, "changed since your plan left out") {
		t.Errorf("logs do not say the section was left out:\n%s", logs)
	}
}

// A merge conflict sends the item back to build, and that run hears what main gained under its
// paths since its plan.
func TestEngine_ConflictReworkGetsChangedSinceYourPlan(t *testing.T) {
	t.Parallel()

	s := newScripted()
	h := newHarness(t, changedSinceGateLine(), s.agent)
	h.codehost.SetBranchHead(appRepo, "", base)
	h.codehost.Add(openPR(prURL))
	h.codehost.Conflict(prURL, storeFile)

	it := h.submit(t, domain.NewItem{})
	s.set(it.ID, stSpec, touchesStore)
	s.set(it.ID, stBuild, `{"pull_request":"`+prURL+`"}`)
	s.set(it.ID, stReview, approve)

	h.step(t) // spec
	h.step(t) // build

	other := h.mergedBy(t, "Rename the items table", "c0ffee000002")
	h.land(ports.Commit{SHA: base}, ports.Commit{SHA: "c0ffee000002", Subject: "squashed", Files: []string{storeFile}})

	h.step(t) // review approves; the merge conflicts and sends it back to build

	if n := len(h.eventsOf(it.ID, domain.EventMergeConflict)); n != 1 {
		t.Fatalf("events %v, want one merge_conflict", h.kinds(it.ID))
	}

	h.step(t) // build again

	want := fmt.Sprintf("- %s %q: %s", ref(other), other.Title, storeFile)
	if section := h.changedSinceOf(3); !strings.Contains(section, want) {
		t.Errorf("the rework's section lacks %q:\n%s", want, h.feedback(3))
	}
}

// Two requests that change the same file, submitted back to back, merge in number order: the
// second is held at build until the first merged, then hears it, and neither conflicts.
func TestEngine_OverlappingRequestsMergeInOrder(t *testing.T) {
	t.Parallel()

	const secondPR = "https://github.com/acme/app/pull/8"

	s := newScripted()
	h := newHarness(t, changedSinceGateLine(), s.agent)
	h.codehost.SetBranchHead(appRepo, "", base)
	h.codehost.Add(openPR(prURL))
	h.codehost.Add(openPR(secondPR))

	first := h.submit(t, domain.NewItem{Title: "Rename the items table"})
	second := h.submit(t, domain.NewItem{Title: "Add an items index"})

	for it, pr := range map[domain.ItemID]string{first.ID: prURL, second.ID: secondPR} {
		s.set(it, stSpec, touchesStore)
		s.set(it, stBuild, `{"pull_request":"`+pr+`"}`)
		s.set(it, stReview, approve)
	}

	h.step(t) // both plan at base
	h.step(t) // first builds; second is held behind it
	h.step(t) // first is reviewed and merged

	merged := h.eventsOf(first.ID, domain.EventMerged)
	if len(merged) != 1 {
		t.Fatalf("first's events %v, want merged", h.kinds(first.ID))
	}

	sha, _ := merged[0].Data["sha"].(string)
	h.land(ports.Commit{SHA: base}, ports.Commit{SHA: sha, Subject: "squashed", Files: []string{storeFile}})

	h.step(t) // second builds
	h.step(t) // second is reviewed and merged

	runs := h.worker.Runs()

	at := func(it domain.ItemID, station string) int {
		return slices.IndexFunc(runs, func(c porttest.ExecCall) bool {
			return c.Sandbox.ItemID == it && c.Sandbox.Station == station
		})
	}

	if a, b := at(first.ID, stReview), at(second.ID, stBuild); a < 0 || b < a {
		t.Fatalf("second built at run %d, first merged at run %d; want second only after first merged", b, a)
	}

	want := fmt.Sprintf("- %s %q: %s", ref(first), first.Title, storeFile)
	if section := h.changedSinceOf(at(second.ID, stBuild)); !strings.Contains(section, want) {
		t.Errorf("second's build section lacks %q:\n%s", want, section)
	}

	if merges := h.codehost.Merges(); len(merges) != 2 || merges[0].URL != prURL || merges[1].URL != secondPR {
		t.Errorf("merges %+v, want first's then second's", merges)
	}

	for _, it := range []domain.Item{first, second} {
		if n := len(h.eventsOf(it.ID, domain.EventMergeConflict)); n != 0 {
			t.Errorf("%s has events %v, want no merge_conflict", ref(it), h.kinds(it.ID))
		}

		if got := h.item(t, it.ID).Station; got != stDone {
			t.Errorf("%s at %s, want done", ref(it), got)
		}
	}
}
