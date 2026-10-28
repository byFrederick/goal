package api_test

import (
	"encoding/csv"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api"
	"github.com/byFrederick/goal/internal/core/domain"
)

const (
	runsExportPath  = orgRunsPath + "/export"
	spendExportPath = orgSpendPath + "/export"
)

// csvOf reads an export's body as CSV records, failing on anything else.
func csvOf(t *testing.T, res response) [][]string {
	t.Helper()

	recs, err := csv.NewReader(strings.NewReader(string(res.body))).ReadAll()
	if err != nil {
		t.Fatalf("body %q is not CSV: %v", res.body, err)
	}

	return recs
}

// sameExport checks an export's status, headers and records.
func sameExport(t *testing.T, what string, res response, file string, want [][]string) {
	t.Helper()

	if res.status != http.StatusOK || res.header.Get("Content-Type") != "text/csv; charset=utf-8" ||
		res.header.Get("Content-Disposition") != `attachment; filename="`+file+`"` {
		t.Fatalf("%s = %d %q %q %s; want 200 text/csv; charset=utf-8 attachment %s", what, res.status,
			res.header.Get("Content-Type"), res.header.Get("Content-Disposition"), res.body, file)
	}

	if got := csvOf(t, res); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("%s =\n  %q\nwant\n  %q", what, got, want)
	}
}

// withExportMax lowers the exports' row cap.
func withExportMax(n int) func(*harness, *api.Deps) {
	return func(_ *harness, d *api.Deps) { d.ExportMaxRows = n }
}

// refuseOperation answers the seam as the scope middleware asks it: it refuses the operation op and
// lets every other question through.
func refuseOperation(op string) func(api.AccessRequest) error {
	return func(r api.AccessRequest) error {
		if r.Operation == op && r.Permission == "" {
			return errors.New("refused")
		}

		return nil
	}
}

func TestExportRuns(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	title := `Fix "login", again`
	build := h.seedTitled(t, title)
	guide := h.seedGuide(t, "Write the user guide")

	ok := h.orgRun(t, build, agBackend, t0.Add(time.Minute), domain.RunResult{Status: domain.RunSucceeded, CostUSD: 0.5, Turns: 3})
	failed := h.orgRun(t, build, agBackend, t0.Add(2*time.Minute), domain.RunResult{Status: domain.RunFailed, CostUSD: 0.125})
	running := h.orgRun(t, guide, evalAgent, t0.Add(3*time.Minute), domain.RunResult{Status: domain.RunRunning})
	h.orgRun(t, build, agBackend, t0.Add(-10*time.Hour), domain.RunResult{Status: domain.RunSucceeded}) // yesterday

	at := func(m int) string { return t0.Add(time.Duration(m) * time.Minute).UTC().Format(time.RFC3339) }
	header := strings.Fields("id factory item_ref item_title station agent model status started_at ended_at turns cost_usd")
	rowOK := []string{string(ok), lineSlug, lineItemRef, title, stBuild, agBackend, runModel, "succeeded", at(1), at(2), "3", "0.5"}
	rowFailed := []string{string(failed), lineSlug, lineItemRef, title, stBuild, agBackend, runModel, "failed", at(2), at(3), "0", "0.125"}
	rowRunning := []string{string(running), acmeSlug, acmeItemRef, "Write the user guide", stInbox, evalAgent, runModel, "running", at(3), "", "0", "0"}
	file := "runs-" + t0.UTC().Format(time.DateOnly) + ".csv"

	res := h.request(t, http.MethodGet, runsExportPath, "", nil)
	sameExport(t, "the day's runs", res, file, [][]string{header, rowRunning, rowFailed, rowOK})

	// RFC 4180: a field with a comma or a quote is quoted, and its quotes doubled.
	if !strings.Contains(string(res.body), `,"Fix ""login"", again",`) {
		t.Errorf("body %s; want the title quoted with its quotes doubled", res.body)
	}

	for _, c := range []struct {
		name, query string
		want        [][]string
	}{
		{"by status", "status=failed", [][]string{header, rowFailed}},
		{"by text", "q=LOGIN", [][]string{header, rowFailed, rowOK}},
		{"one factory", "factory=acme", [][]string{header, rowRunning}},
		{"paging is ignored", "limit=1", [][]string{header, rowRunning, rowFailed, rowOK}},
		{"nothing", "until=" + at(-1), [][]string{header}},
	} {
		sameExport(t, c.name, h.request(t, http.MethodGet, runsExportPath+"?"+c.query, "", nil), file, c.want)
	}

	refused(t, "an unknown factory", h.request(t, http.MethodGet, runsExportPath+"?factory=nope", "", nil),
		http.StatusNotFound, "factory-not-found")

	if res := h.request(t, http.MethodGet, runsExportPath+"?since="+at(5)+"&until="+at(5), "", nil); res.status != http.StatusBadRequest {
		t.Errorf("since not before until = %d %s; want 400", res.status, res.body)
	}

	t.Run("Permission", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		h.seam.steps()

		if res := h.request(t, http.MethodGet, runsExportPath, "", nil); res.status != http.StatusOK {
			t.Fatalf("export = %d", res.status)
		}

		if !slices.ContainsFunc(h.seam.steps(), func(s step) bool {
			return s.access != nil && s.access.Operation == "exportRuns" && s.access.Permission == ""
		}) {
			t.Error("the export asked no access question for exportRuns")
		}

		h.seam.deny(refuseOperation("exportRuns"))

		refused(t, "a refused caller", h.request(t, http.MethodGet, runsExportPath, "", nil), http.StatusForbidden, "forbidden")
	})

	t.Run("TooLarge", func(t *testing.T) {
		t.Parallel()

		h := newHarnessLogged(t, nil, io.Discard, withExportMax(2))
		it := h.seedTitled(t, title)

		for i := range 3 {
			h.orgRun(t, it, agBackend, t0.Add(time.Duration(i)*time.Minute), domain.RunResult{Status: domain.RunSucceeded})
		}

		res := h.request(t, http.MethodGet, runsExportPath, "", nil)
		refused(t, "3 runs, at most 2", res, http.StatusRequestEntityTooLarge, "export-too-large")

		if strings.Contains(string(res.body), "item_ref") {
			t.Errorf("the 413's body %s holds CSV", res.body)
		}

		if res := h.request(t, http.MethodGet, runsExportPath+"?until="+t0.Add(2*time.Minute).UTC().Format(time.RFC3339), "", nil); res.status != http.StatusOK ||
			len(csvOf(t, res)) != 3 {
			t.Errorf("2 runs, at most 2 = %d %s; want 200 with both", res.status, res.body)
		}
	})
}

func TestExportSpend(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)
	h.seedSpend(t)

	header := strings.Fields("factory agent model cost_usd")
	acmeRow := []string{acmeSlug, evalAgent, "claude-fable-5-1", "1"}
	lineFable := []string{lineSlug, agBackend, "claude-fable-5-1", "5"}
	lineHaiku := []string{lineSlug, agentFrontend, modelHaiku, "4"}
	file := "spend-" + t0.UTC().Format(time.DateOnly) + ".csv"

	// Rows are ordered by factory id, then agent and model.
	byFactory := func(acme, line [][]string) [][]string {
		if acmeID < lineID {
			return slices.Concat([][]string{header}, acme, line)
		}

		return slices.Concat([][]string{header}, line, acme)
	}

	sameExport(t, "this week", h.as(t, ownerToken, http.MethodGet, spendExportPath, ""), file,
		byFactory([][]string{acmeRow}, [][]string{{lineSlug, agBackend, "claude-fable-5-1", "2"}}))

	sameExport(t, "90 days", h.as(t, ownerToken, http.MethodGet, spendExportPath+"?window=90d", ""), file,
		byFactory([][]string{acmeRow}, [][]string{lineFable, lineHaiku}))

	// A member who sees acme alone exports acme's rows alone, as the spend list shows them.
	sameExport(t, "the viewer's 90 days", h.as(t, viewerToken, http.MethodGet, spendExportPath+"?window=90d", ""), file,
		[][]string{header, acmeRow})

	if res := h.as(t, ownerToken, http.MethodGet, spendExportPath+"?window=year", ""); res.status != http.StatusBadRequest {
		t.Errorf("window=year = %d %s; want 400", res.status, res.body)
	}

	t.Run("Permission", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		h.seam.deny(refuseOperation("exportSpend"))

		refused(t, "a refused caller", h.request(t, http.MethodGet, spendExportPath, "", nil), http.StatusForbidden, "forbidden")
	})

	t.Run("TooLarge", func(t *testing.T) {
		t.Parallel()

		h := newHarnessLogged(t, nil, io.Discard, withExportMax(2))
		h.seedSpend(t)

		refused(t, "3 rows, at most 2", h.request(t, http.MethodGet, spendExportPath+"?window=90d", "", nil),
			http.StatusRequestEntityTooLarge, "export-too-large")

		if res := h.request(t, http.MethodGet, spendExportPath, "", nil); res.status != http.StatusOK || len(csvOf(t, res)) != 3 {
			t.Errorf("this week's 2 rows, at most 2 = %d %s; want 200 with both", res.status, res.body)
		}
	})
}
