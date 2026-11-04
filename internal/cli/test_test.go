package cli

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

const webTests = "/v1/orgs/acme/factories/web/jonah/tests"

func goalTest(t *testing.T, api string, args ...string) (string, error) {
	t.Helper()

	var buf bytes.Buffer

	err := run(t.Context(), append([]string{cmdTest}, args...), &printer{w: &buf}, testEnv(t, api, map[string]string{envOrg: orgAcme}), nil)

	return buf.String(), err
}

// goal test keep, list and rm call the kept-test routes: keep sends the item's number and the
// station, list prints each test with its last result, rm removes one by id; a bad call is the
// usage, found before goald is called, and a documented 409 its one-line message.
func TestTest_KeepListRemove(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 11, 4, 9, 30, 0, 0, time.UTC)
	kept := gen.KeptTest{Id: "k1", Item: gen.KeptTestItem{Id: "i12", Number: 12, Title: "Fix the lease"}, Station: stModelBuild, KeptBy: "user:ada", KeptAt: at}
	run := "r1"
	passed := kept
	passed.Id, passed.Item.Number, passed.Item.Title = "k2", 9, "Add a page"
	passed.LastResult = &gen.KeptTestLastResult{Result: gen.KeptTestLastResultResultFailed, At: at, RunId: &run}

	srv, sent := routeGoald(t, map[string]goaldAnswer{
		listFactoriesRoute:              {http.StatusOK, gen.FactoryList{Factories: []gen.Factory{{Id: "f1", Name: searchFactory}}}},
		"POST " + webTests:              {http.StatusCreated, kept},
		"GET " + webTests:               {http.StatusOK, gen.KeptTestList{Tests: []gen.KeptTest{kept, passed}}},
		"DELETE " + webTests + "/k1":    {http.StatusNoContent, nil},
		"DELETE " + webTests + "/other": {http.StatusConflict, jonahRefusal(http.StatusNotFound, "kept-test-not-found")},
	})

	got, err := goalTest(t, srv.URL, "keep", "12", "--station", stModelBuild)
	if err != nil || got != "kept #12 Fix the lease at build as test k1\n" {
		t.Errorf("goal test keep = %q, %v", got, err)
	}

	got, err = goalTest(t, srv.URL, subList, "--factory", searchFactory)
	if err != nil {
		t.Fatal(err)
	}

	for _, row := range []string{
		"build    #12   Fix the lease  never run         user:ada  k1",
		"build    #9    Add a page     failed 2026-11-04 09:30  user:ada  k2",
	} {
		if !strings.Contains(strings.Join(strings.Fields(got), " "), strings.Join(strings.Fields(row), " ")) {
			t.Errorf("goal test list printed\n%s\nwant a row %q", got, row)
		}
	}

	if got, err := goalTest(t, srv.URL, "rm", "k1"); err != nil || got != "removed kept test k1\n" {
		t.Errorf("goal test rm = %q, %v", got, err)
	}

	if _, err := goalTest(t, srv.URL, "rm", "other"); err == nil {
		t.Error("goal test rm of a refused id = nil, want goald's refusal")
	}

	for _, bad := range [][]string{{"keep", "12"}, {"keep", "twelve", "--station", stModelBuild}, {"rm"}, {subList, argExtra}, {"run"}, {}} {
		if _, err := goalTest(t, srv.URL, bad...); !errors.Is(err, errUsage) {
			t.Errorf("goal test %v = %v, want the usage", bad, err)
		}
	}

	want := []string{
		listFactoriesRoute, `POST ` + webTests + ` {"item":12,"station":"build"}`, "GET " + webTests,
		listFactoriesRoute, "DELETE " + webTests + "/k1", listFactoriesRoute, "DELETE " + webTests + "/other",
	}
	if s := sent(); strings.Join(s, "\n") != strings.Join(want, "\n") {
		t.Errorf("goald was sent\n%s\nwant\n%s", strings.Join(s, "\n"), strings.Join(want, "\n"))
	}
}

// A keep goald refuses with a documented 409 is its one-line message.
func TestTest_KeepConflictIsOneLine(t *testing.T) {
	t.Parallel()

	srv, _ := routeGoald(t, map[string]goaldAnswer{
		listFactoriesRoute: {http.StatusOK, gen.FactoryList{Factories: []gen.Factory{{Id: "f1", Name: searchFactory}}}},
		"POST " + webTests: {http.StatusConflict, jonahRefusal(http.StatusConflict, "not-passed-first-time")},
	})

	if _, err := goalTest(t, srv.URL, "keep", "12", "--station", stModelBuild); err == nil || err.Error() != testConflicts["not-passed-first-time"] {
		t.Errorf("goal test keep of an item sent back = %v, want %q", err, testConflicts["not-passed-first-time"])
	}
}
