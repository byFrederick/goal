package github

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/ports"
)

const (
	checkRunsPath = "/repos/acme/app/commits/" + headSHA + "/check-runs"
	statusPath    = "/repos/acme/app/commits/" + headSHA + "/status"
	noStatuses    = `{"state":"pending","total_count":0,"statuses":[]}`
	noCheckRuns   = `{"total_count":0,"check_runs":[]}`
)

// fakeChecks is GitHub's checks API for acme/app: each path answers its handler, and it records
// the requests it got.
type fakeChecks struct {
	mu       sync.Mutex
	requests []*http.Request
	handlers map[string]http.HandlerFunc
}

func (f *fakeChecks) serve(t *testing.T) string {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.Clone(r.Context()))
		h := f.handlers[r.URL.Path]
		f.mu.Unlock()

		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		if h == nil {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)

			return
		}

		h(w, r)
	}))
	t.Cleanup(srv.Close)

	return srv.URL
}

func (f *fakeChecks) paths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []string
	for _, r := range f.requests {
		out = append(out, r.URL.Path)
	}

	return out
}

// answer writes body as JSON with status.
func answer(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func checkRun(name, status, conclusion string) string {
	c := "null"
	if conclusion != "" {
		c = strconv.Quote(conclusion)
	}

	return `{"name":` + strconv.Quote(name) + `,"status":` + strconv.Quote(status) + `,"conclusion":` + c +
		`,"html_url":"https://github.com/acme/app/runs/` + name + `"}`
}

func checkRuns(runs ...string) string {
	return `{"total_count":` + strconv.Itoa(len(runs)) + `,"check_runs":[` + strings.Join(runs, ",") + `]}`
}

func checksOf(t *testing.T, api, sha, url string) (ports.CommitChecks, error) {
	t.Helper()

	return New(Config{APIURL: api}).Checks(t.Context(), appRepo(t), token, sha, url)
}

func TestChecks_CheckRunsOnly(t *testing.T) {
	t.Parallel()

	runs := []struct {
		status, conclusion string
		want               ports.Check
	}{
		{"queued", "", ports.Check{State: ports.CheckPending}},
		{"waiting", "", ports.Check{State: ports.CheckPending}},
		{"requested", "", ports.Check{State: ports.CheckPending}},
		{"pending", "", ports.Check{State: ports.CheckPending}},
		{"in_progress", "", ports.Check{State: ports.CheckRunning}},
		{runCompleted, "success", ports.Check{State: ports.CheckFinished, Result: ports.CheckPassed}},
		{runCompleted, "neutral", ports.Check{State: ports.CheckFinished, Result: ports.CheckNeutral}},
		{runCompleted, "skipped", ports.Check{State: ports.CheckFinished, Result: ports.CheckNeutral}},
		{runCompleted, "stale", ports.Check{State: ports.CheckFinished, Result: ports.CheckNeutral}},
		{runCompleted, "failure", ports.Check{State: ports.CheckFinished, Result: ports.CheckFailed}},
		{runCompleted, "timed_out", ports.Check{State: ports.CheckFinished, Result: ports.CheckFailed}},
		{runCompleted, "cancelled", ports.Check{State: ports.CheckFinished, Result: ports.CheckFailed}},
		{runCompleted, "action_required", ports.Check{State: ports.CheckFinished, Result: ports.CheckFailed}},
		{runCompleted, "startup_failure", ports.Check{State: ports.CheckFinished, Result: ports.CheckFailed}},
	}

	var (
		bodies []string
		want   []ports.Check
	)

	for i, r := range runs {
		name := "job" + strconv.Itoa(i)
		bodies = append(bodies, checkRun(name, r.status, r.conclusion))

		w := r.want
		w.Name, w.URL = name, "https://github.com/acme/app/runs/"+name
		want = append(want, w)
	}

	f := &fakeChecks{handlers: map[string]http.HandlerFunc{
		checkRunsPath: func(w http.ResponseWriter, r *http.Request) {
			if q := r.URL.Query(); q.Get("filter") != "latest" || q.Get("per_page") != "100" {
				t.Errorf("check runs query = %q; want filter=latest and per_page=100", r.URL.RawQuery)
			}

			answer(http.StatusOK, checkRuns(bodies...))(w, r)
		},
		statusPath: answer(http.StatusOK, noStatuses),
	}}

	got, err := checksOf(t, f.serve(t), headSHA, "")
	if err != nil {
		t.Fatal(err)
	}

	if got.Head != headSHA || !reflect.DeepEqual(got.Checks, want) {
		t.Errorf("Checks = %+v\nwant head %s and %+v", got, headSHA, want)
	}
}

func TestChecks_StatusesOnly(t *testing.T) {
	t.Parallel()

	f := &fakeChecks{handlers: map[string]http.HandlerFunc{
		checkRunsPath: answer(http.StatusOK, noCheckRuns),
		// The combined state says failure; the checks come from the list.
		statusPath: answer(http.StatusOK, `{"state":"failure","total_count":4,"statuses":[
			{"context":"ci/build","state":"success","target_url":"https://ci.example.com/1"},
			{"context":"ci/lint","state":"failure","target_url":"https://ci.example.com/2"},
			{"context":"ci/e2e","state":"error","target_url":null},
			{"context":"ci/deploy","state":"pending","target_url":"https://ci.example.com/4"}]}`),
	}}

	got, err := checksOf(t, f.serve(t), headSHA, "")
	if err != nil {
		t.Fatal(err)
	}

	want := []ports.Check{
		{Name: "ci/build", State: ports.CheckFinished, Result: ports.CheckPassed, URL: "https://ci.example.com/1"},
		{Name: "ci/lint", State: ports.CheckFinished, Result: ports.CheckFailed, URL: "https://ci.example.com/2"},
		{Name: "ci/e2e", State: ports.CheckFinished, Result: ports.CheckFailed},
		{Name: "ci/deploy", State: ports.CheckPending, URL: "https://ci.example.com/4"},
	}

	if !reflect.DeepEqual(got.Checks, want) {
		t.Errorf("Checks = %+v\nwant %+v", got.Checks, want)
	}
}

// A commit with no CI answers a combined state of pending with no statuses: that is no checks.
func TestChecks_Neither(t *testing.T) {
	t.Parallel()

	f := &fakeChecks{handlers: map[string]http.HandlerFunc{
		checkRunsPath: answer(http.StatusOK, noCheckRuns),
		statusPath:    answer(http.StatusOK, noStatuses),
	}}

	got, err := checksOf(t, f.serve(t), headSHA, "")
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Checks) != 0 || got.Finished() || got.Head != headSHA {
		t.Errorf("Checks = %+v, finished %v; want no checks, not finished", got, got.Finished())
	}
}

func TestChecks_Pages(t *testing.T) {
	t.Parallel()

	var api string

	f := &fakeChecks{handlers: map[string]http.HandlerFunc{
		checkRunsPath: func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("page") == "2" {
				answer(http.StatusOK, checkRuns(checkRun("two", "in_progress", "")))(w, r)

				return
			}

			w.Header().Set("Link", `<`+api+checkRunsPath+`?filter=latest&per_page=100&page=2>; rel="next"`)
			answer(http.StatusOK, checkRuns(checkRun("one", runCompleted, "success")))(w, r)
		},
		statusPath: answer(http.StatusOK, noStatuses),
	}}
	api = f.serve(t)

	got, err := checksOf(t, api, headSHA, "")
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, c := range got.Checks {
		names = append(names, c.Name)
	}

	if !reflect.DeepEqual(names, []string{"one", "two"}) {
		t.Errorf("checks = %v; want both pages, [one two]", names)
	}
}

// A commit whose check runs link a next page past the cap is an error, never part of its checks.
func TestChecks_TooManyPages(t *testing.T) {
	t.Parallel()

	f := &fakeChecks{handlers: map[string]http.HandlerFunc{
		checkRunsPath: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Link", `<https://example.com/next>; rel="next"`)
			answer(http.StatusOK, checkRuns(checkRun("one", runCompleted, "success")))(w, r)
		},
		statusPath: answer(http.StatusOK, noStatuses),
	}}

	if got, err := checksOf(t, f.serve(t), headSHA, ""); err == nil {
		t.Errorf("Checks = %+v; want an error past %d pages", got, maxCheckRunPages)
	}
}

func TestChecks_NotReadable(t *testing.T) {
	t.Parallel()

	forbidden := answer(http.StatusForbidden, `{"message":"Resource not accessible by personal access token"}`)

	for name, handlers := range map[string]map[string]http.HandlerFunc{
		"check runs": {checkRunsPath: forbidden, statusPath: answer(http.StatusOK, noStatuses)},
		"statuses":   {checkRunsPath: answer(http.StatusOK, noCheckRuns), statusPath: forbidden},
	} {
		f := &fakeChecks{handlers: handlers}

		_, err := checksOf(t, f.serve(t), headSHA, "")

		var notReadable *ports.CINotReadableError
		if !errors.Is(err, ports.ErrCINotReadable) || !errors.As(err, &notReadable) {
			t.Fatalf("a 403 on %s: err = %v; want a *ports.CINotReadableError", name, err)
		}

		if p := notReadable.Permission; !strings.Contains(p, "fine-grained") || !strings.Contains(p, "Checks") || !strings.Contains(p, "Commit statuses") {
			t.Errorf("a 403 on %s: permission = %q; want read access to Checks and Commit statuses", name, p)
		}

		if strings.Contains(err.Error(), token) {
			t.Errorf("a 403 on %s: the error shows the token", name)
		}
	}
}

func TestChecks_RateLimited(t *testing.T) {
	t.Parallel()

	reset := time.Now().Add(time.Hour).Truncate(time.Second)

	for name, tt := range map[string]struct {
		handler    http.HandlerFunc
		from, till time.Duration
		at         time.Time
	}{
		"429 with Retry-After": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "120")
				answer(http.StatusTooManyRequests, `{"message":"slow down"}`)(w, r)
			},
			from: 110 * time.Second, till: 130 * time.Second,
		},
		"403 with no requests left": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Ratelimit-Remaining", "0")
				w.Header().Set("X-Ratelimit-Reset", strconv.FormatInt(reset.Unix(), 10))
				answer(http.StatusForbidden, `{"message":"API rate limit exceeded"}`)(w, r)
			},
			at: reset,
		},
	} {
		f := &fakeChecks{handlers: map[string]http.HandlerFunc{checkRunsPath: tt.handler, statusPath: tt.handler}}

		_, err := checksOf(t, f.serve(t), headSHA, "")

		var limited *ports.RateLimitedError
		if !errors.As(err, &limited) || errors.Is(err, ports.ErrCINotReadable) {
			t.Fatalf("%s: err = %v; want only a *ports.RateLimitedError", name, err)
		}

		if !tt.at.IsZero() {
			if !limited.RetryAt.Equal(tt.at) {
				t.Errorf("%s: retry at %v; want %v", name, limited.RetryAt, tt.at)
			}

			continue
		}

		if wait := time.Until(limited.RetryAt); wait < tt.from || wait > tt.till {
			t.Errorf("%s: retry in %v; want about 120s", name, wait)
		}
	}
}

// The second read of a URL sends its ETag, and a 304 with no body gives the first answer again.
func TestChecks_NotModified(t *testing.T) {
	t.Parallel()

	etagged := func(etag, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("If-None-Match") == etag {
				w.WriteHeader(http.StatusNotModified)

				return
			}

			w.Header().Set("ETag", etag)
			answer(http.StatusOK, body)(w, r)
		}
	}

	f := &fakeChecks{handlers: map[string]http.HandlerFunc{
		checkRunsPath: etagged(`"runs-1"`, checkRuns(checkRun("build", runCompleted, "success"))),
		statusPath:    etagged(`W/"status-1"`, `{"state":"success","statuses":[{"context":"ci/x","state":"success"}]}`),
	}}

	c := New(Config{APIURL: f.serve(t)})

	first, err := c.Checks(t.Context(), appRepo(t), token, headSHA, "")
	if err != nil {
		t.Fatal(err)
	}

	second, err := c.Checks(t.Context(), appRepo(t), token, headSHA, "")
	if err != nil {
		t.Fatal(err)
	}

	if len(first.Checks) != 2 || !reflect.DeepEqual(first, second) {
		t.Errorf("after a 304: %+v; want the first answer, %+v", second, first)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	sent := map[string]string{}
	for _, r := range f.requests[2:] {
		sent[r.URL.Path] = r.Header.Get("If-None-Match")
	}

	if want := map[string]string{checkRunsPath: `"runs-1"`, statusPath: `W/"status-1"`}; !reflect.DeepEqual(sent, want) {
		t.Errorf("second reads sent If-None-Match %v; want %v", sent, want)
	}
}

// With a pull request's URL, Checks reads the checks of its current head, not of the sha given.
func TestChecks_PullRequestHead(t *testing.T) {
	t.Parallel()

	f := &fakeChecks{handlers: map[string]http.HandlerFunc{
		"/repos/acme/app/pulls/7": answer(http.StatusOK, `{"state":"open","head":{"sha":"`+headSHA+`"}}`),
		checkRunsPath:             answer(http.StatusOK, checkRuns(checkRun("build", "queued", ""))),
		statusPath:                answer(http.StatusOK, noStatuses),
	}}

	got, err := checksOf(t, f.serve(t), "0ld5ha", prURL)
	if err != nil {
		t.Fatal(err)
	}

	if got.Head != headSHA || len(got.Checks) != 1 {
		t.Errorf("Checks = %+v; want the head %s's one check", got, headSHA)
	}

	for _, p := range f.paths() {
		if strings.Contains(p, "0ld5ha") {
			t.Errorf("Checks read %s; want only the pull request's head", p)
		}
	}
}

// A sha or a pull request URL that names nothing of the repository is refused before anything is
// sent.
func TestChecks_Refused(t *testing.T) {
	t.Parallel()

	f := &fakeChecks{}
	api := f.serve(t)

	for _, tt := range []struct{ sha, url string }{
		{"", ""},
		{"not/a/sha", ""},
		{headSHA, "https://github.com/acme/other/pull/7"},
	} {
		if _, err := checksOf(t, api, tt.sha, tt.url); err == nil {
			t.Errorf("Checks(%q, %q) = nil error; want a refusal", tt.sha, tt.url)
		}
	}

	if p := f.paths(); len(p) != 0 {
		t.Errorf("sent %v; want nothing", p)
	}
}
