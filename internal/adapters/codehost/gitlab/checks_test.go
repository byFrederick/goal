package gitlab

import (
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/ports"
)

const (
	// newHeadSHA is the merge request's head when it moved past the commit Checks was asked about.
	newHeadSHA = "c0ffee01"
	// routeMR and routePipelines name Checks' two GraphQL queries.
	routeMR        = "graphql mergeRequest"
	routePipelines = "graphql pipelines"
	emptyPipelines = "empty_pipelines.json"
	pushPipeline   = "push pipeline on goal/3-add-x"
)

// recorded are the recorded answers.
var recorded = os.DirFS(filepath.Join("testdata", "checks"))

// ciGitLab is a GitLab that answers Checks' calls with recorded answers from testdata/checks: mr
// answers the merge request query, pipelines the pipelines query and project (when not nil)
// REST GET /projects/:id. limited, when set, answers a route with a 429 and those headers.
type ciGitLab struct {
	mr, pipelines string
	project       any
	limited       map[string]http.Header

	mu       sync.Mutex
	requests []string
	// shas are the sha variables the pipelines queries named.
	shas []string
}

// route names a request: routeMR, routePipelines or the REST method and path.
func route(t *testing.T, r *http.Request) (string, map[string]any) {
	t.Helper()

	if r.URL.Path != "/graphql" {
		return r.Method + " " + r.URL.EscapedPath(), nil
	}

	var body struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}

	if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&body) != nil {
		t.Errorf("GraphQL request %s is not a POST of a query", r.Method)
	}

	if body.Variables["fullPath"] != "acme/platform/web" {
		t.Errorf("GraphQL fullPath = %v; want acme/platform/web", body.Variables["fullPath"])
	}

	if strings.Contains(body.Query, "mergeRequest(") {
		if body.Variables["iid"] != "3" {
			t.Errorf("GraphQL iid = %v; want 3", body.Variables["iid"])
		}

		return routeMR, body.Variables
	}

	return routePipelines, body.Variables
}

func (f *ciGitLab) serve(t *testing.T) *Client {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, vars := route(t, r)

		f.mu.Lock()
		f.requests = append(f.requests, name)

		if name == routePipelines {
			sha, _ := vars["sha"].(string)
			f.shas = append(f.shas, sha)
		}
		f.mu.Unlock()

		if r.Header.Get("PRIVATE-TOKEN") != token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		if h, ok := f.limited[name]; ok {
			maps.Copy(w.Header(), h)
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("Retry later " + token))

			return
		}

		var file string

		switch name {
		case routeMR:
			file = f.mr
		case routePipelines:
			file = f.pipelines
		case "GET " + project:
			if f.project != nil {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(f.project)

				return
			}
		}

		if file == "" {
			t.Errorf("Checks sent %s; want no such request", name)
			w.WriteHeader(http.StatusNotFound)

			return
		}

		raw, err := fs.ReadFile(recorded, file)
		if err != nil {
			t.Fatal(err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}))
	t.Cleanup(srv.Close)

	return New(Config{APIURL: srv.URL})
}

func (f *ciGitLab) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.requests)
}

// checksOf runs Checks on acme/platform/web!3 (or the commit alone when url is empty).
func (f *ciGitLab) checksOf(t *testing.T, url string) (ports.CommitChecks, error) {
	t.Helper()

	return f.serve(t).Checks(t.Context(), webRepo(t), token, headSHA, url)
}

func pipelineURL(id int) string {
	return "https://gitlab.com/acme/platform/web/-/pipelines/" + strconv.Itoa(id)
}

func wantChecks(t *testing.T, got ports.CommitChecks, err error, head string, want ...ports.Check) {
	t.Helper()

	if err != nil {
		t.Fatalf("Checks: %v", err)
	}

	if got.Head != head || !reflect.DeepEqual(got.Checks, want) {
		t.Errorf("Checks = %+v; want head %s and %+v", got, head, want)
	}
}

// A merged-result pipeline runs on GitLab's merge commit, not the head: it comes from the merge
// request's headPipeline, and the head's own branch pipeline follows it. The head is the merge
// request's, read before the head's pipelines.
func TestChecks_MergedResultPipeline(t *testing.T) {
	t.Parallel()

	f := &ciGitLab{mr: "merged_result_mr.json", pipelines: "merged_result_pipelines.json"}
	got, err := f.checksOf(t, mrURL)

	wantChecks(t, got, err, newHeadSHA,
		ports.Check{Name: "merge_request_event pipeline on refs/merge-requests/3/merge", State: ports.CheckRunning, URL: pipelineURL(201)},
		ports.Check{Name: pushPipeline, State: ports.CheckFinished, Result: ports.CheckPassed, URL: pipelineURL(200)},
	)

	if want := []string{routeMR, routePipelines}; !slices.Equal(f.sent(), want) {
		t.Errorf("sent %v; want %v (never REST head_pipeline)", f.sent(), want)
	}

	if !slices.Equal(f.shas, []string{newHeadSHA}) {
		t.Errorf("pipelines read for %v; want the merge request's head %s", f.shas, newHeadSHA)
	}
}

func TestChecks_MergeTrainPipeline(t *testing.T) {
	t.Parallel()

	f := &ciGitLab{mr: "merge_train_mr.json", pipelines: emptyPipelines}
	got, err := f.checksOf(t, mrURL)

	wantChecks(t, got, err, headSHA, ports.Check{Name: "Merge train", State: ports.CheckPending, URL: pipelineURL(210)})
}

// A detached merge request pipeline runs on the head, so it is both the headPipeline and one of
// the head's pipelines: it is listed once.
func TestChecks_DetachedPipeline(t *testing.T) {
	t.Parallel()

	f := &ciGitLab{mr: "detached_mr.json", pipelines: "detached_pipelines.json"}
	got, err := f.checksOf(t, mrURL)

	wantChecks(t, got, err, headSHA, ports.Check{
		Name: "merge_request_event pipeline on refs/merge-requests/3/head", State: ports.CheckFinished,
		Result: ports.CheckFailed, URL: pipelineURL(220), FailedJobs: []string{"test"},
	})
}

// Of the head's pipelines, the latest of each ref and source is kept: an older push pipeline and
// an older external status are left out, an external CI's pipeline and a scheduled one are in.
func TestChecks_BranchPipelines(t *testing.T) {
	t.Parallel()

	f := &ciGitLab{mr: "branch_mr.json", pipelines: "branch_pipelines.json"}
	got, err := f.checksOf(t, mrURL)

	wantChecks(t, got, err, headSHA,
		ports.Check{Name: pushPipeline, State: ports.CheckRunning, URL: pipelineURL(302)},
		ports.Check{Name: "acme-ci/build", State: ports.CheckFinished, Result: ports.CheckPassed, URL: pipelineURL(304)},
		ports.Check{Name: "schedule pipeline on main", State: ports.CheckFinished, Result: ports.CheckPassed, URL: pipelineURL(300)},
	)
}

// Right after a push the head has no pipeline yet: no checks and no error, which the caller
// reads as pending, not as no CI.
func TestChecks_NullHeadPipeline(t *testing.T) {
	t.Parallel()

	f := &ciGitLab{mr: "null_head_mr.json", pipelines: emptyPipelines}
	got, err := f.checksOf(t, mrURL)

	wantChecks(t, got, err, newHeadSHA)

	if got.Finished() {
		t.Error("no checks is finished; want not finished")
	}
}

// Without a merge request, Checks reads the commit's pipelines only.
func TestChecks_CommitOnly(t *testing.T) {
	t.Parallel()

	f := &ciGitLab{pipelines: "commit_pipelines.json"}
	got, err := f.checksOf(t, "")

	wantChecks(t, got, err, headSHA, ports.Check{Name: "push pipeline on main", State: ports.CheckPending, URL: pipelineURL(400)})

	if want := []string{routePipelines}; !slices.Equal(f.sent(), want) || !slices.Equal(f.shas, []string{headSHA}) {
		t.Errorf("sent %v for %v; want %v for %s", f.sent(), f.shas, want, headSHA)
	}
}

// A manual pipeline waits for a person, so it counts as finished, neutral.
func TestChecks_Manual(t *testing.T) {
	t.Parallel()

	f := &ciGitLab{mr: "manual_mr.json", pipelines: emptyPipelines}
	got, err := f.checksOf(t, mrURL)

	wantChecks(t, got, err, headSHA, ports.Check{
		Name: pushPipeline, State: ports.CheckFinished, Result: ports.CheckNeutral, URL: pipelineURL(500),
	})

	if !got.Finished() {
		t.Error("a manual pipeline is not finished; want finished")
	}
}

func TestChecks_StatusMapping(t *testing.T) {
	t.Parallel()

	for status, want := range map[string]ports.Check{
		"CREATED":              {State: ports.CheckPending},
		"WAITING_FOR_RESOURCE": {State: ports.CheckPending},
		"PREPARING":            {State: ports.CheckPending},
		"WAITING_FOR_CALLBACK": {State: ports.CheckPending},
		"PENDING":              {State: ports.CheckPending},
		"SCHEDULED":            {State: ports.CheckPending},
		"RUNNING":              {State: ports.CheckRunning},
		"CANCELING":            {State: ports.CheckRunning},
		"SUCCESS":              {State: ports.CheckFinished, Result: ports.CheckPassed},
		"FAILED":               {State: ports.CheckFinished, Result: ports.CheckFailed},
		"CANCELED":             {State: ports.CheckFinished, Result: ports.CheckFailed},
		"SKIPPED":              {State: ports.CheckFinished, Result: ports.CheckNeutral},
		"MANUAL":               {State: ports.CheckFinished, Result: ports.CheckNeutral},
		"SOMETHING_NEW":        {State: ports.CheckPending},
	} {
		state, result := pipelineState(status)
		if state != want.State || result != want.Result {
			t.Errorf("pipelineState(%s) = %s, %q; want %s, %q", status, state, result, want.State, want.Result)
		}
	}
}

// A job that fails with allow_failure leaves the pipeline green: the pipeline's own status wins,
// and no failed job is named.
func TestChecks_AllowFailure(t *testing.T) {
	t.Parallel()

	f := &ciGitLab{mr: "allow_failure_mr.json", pipelines: emptyPipelines}
	got, err := f.checksOf(t, mrURL)

	wantChecks(t, got, err, headSHA, ports.Check{
		Name: pushPipeline, State: ports.CheckFinished, Result: ports.CheckPassed, URL: pipelineURL(600),
	})
}

// A failed pipeline names at most ports.MaxFailedJobs of its failed jobs, without the allowed
// failures.
func TestChecks_FailedJobs(t *testing.T) {
	t.Parallel()

	f := &ciGitLab{mr: "failed_jobs_mr.json", pipelines: emptyPipelines}
	got, err := f.checksOf(t, mrURL)

	wantChecks(t, got, err, headSHA, ports.Check{
		Name: pushPipeline, State: ports.CheckFinished, Result: ports.CheckFailed, URL: pipelineURL(700),
		FailedJobs: []string{"e2e 1/12", "e2e 2/12", "e2e 3/12", "e2e 4/12", "e2e 5/12"},
	})
}

// GitLab lists no pipelines both when CI is off and when the token cannot see it; with jobsEnabled
// false, the project's builds_access_level tells them apart. Only disabled is no CI.
func TestChecks_HiddenCI(t *testing.T) {
	t.Parallel()

	for name, answer := range map[string]map[string]any{
		"disabled": {"id": 42, "builds_access_level": "disabled"},
		"private":  {"id": 42, "builds_access_level": "private"},
		"enabled":  {"id": 42, "builds_access_level": "enabled"},
		"missing":  {"id": 42},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := &ciGitLab{mr: "null_head_mr.json", pipelines: "hidden_pipelines.json", project: answer}
			got, err := f.checksOf(t, mrURL)

			if !slices.Contains(f.sent(), "GET "+project) {
				t.Errorf("sent %v; want GET %s for builds_access_level", f.sent(), project)
			}

			if name == "disabled" {
				wantChecks(t, got, err, newHeadSHA)

				return
			}

			var notReadable *ports.CINotReadableError
			if !errors.As(err, &notReadable) || !errors.Is(err, ports.ErrCINotReadable) {
				t.Fatalf("Checks = %+v, %v; want a *ports.CINotReadableError", got, err)
			}

			if !strings.Contains(notReadable.Permission, "read_api") || !strings.Contains(notReadable.Permission, "pipelines") {
				t.Errorf("Permission = %q; want read_api and a role that can see pipelines", notReadable.Permission)
			}
		})
	}
}

// An unknown project, or one the token cannot see, is not found; GraphQL's errors are errors,
// never no pipelines, and never carry the token.
func TestChecks_GraphQLRefusals(t *testing.T) {
	t.Parallel()

	_, err := (&ciGitLab{mr: "no_project.json"}).checksOf(t, mrURL)
	if !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("a null project: err = %v; want ports.ErrNotFound", err)
	}

	got, err := (&ciGitLab{pipelines: "graphql_errors.json"}).checksOf(t, "")
	if err == nil || !strings.Contains(err.Error(), "doesn't exist") {
		t.Errorf("GraphQL errors: Checks = %+v, %v; want an error with GitLab's message", got, err)
	}
}

// A 429 on any call is a *ports.RateLimitedError at Retry-After, else RateLimit-Reset, else in a
// minute, and the token stays out of the error.
func TestChecks_RateLimited(t *testing.T) {
	t.Parallel()

	reset := time.Now().Add(10 * time.Minute).Truncate(time.Second)

	for _, tt := range []struct {
		name    string
		route   string
		header  http.Header
		project any
		want    time.Duration
	}{
		{name: "retry-after", route: routeMR, header: http.Header{"Retry-After": {"30"}}, want: 30 * time.Second},
		{
			name: "ratelimit-reset", route: routePipelines,
			header: http.Header{"Ratelimit-Reset": {strconv.FormatInt(reset.Unix(), 10)}}, want: time.Until(reset),
		},
		{name: "neither", route: routeMR, header: http.Header{}, want: time.Minute},
		{name: "rest", route: "GET " + project, header: http.Header{"Retry-After": {"120"}}, project: map[string]any{}, want: 2 * time.Minute},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := &ciGitLab{
				mr: "null_head_mr.json", pipelines: "hidden_pipelines.json", project: tt.project,
				limited: map[string]http.Header{tt.route: tt.header},
			}

			before := time.Now()
			_, err := f.checksOf(t, mrURL)

			var limited *ports.RateLimitedError
			if !errors.As(err, &limited) {
				t.Fatalf("err = %v; want a *ports.RateLimitedError", err)
			}

			if strings.Contains(err.Error(), token) {
				t.Errorf("err = %v; it carries the token", err)
			}

			low, high := before.Add(tt.want-2*time.Second), time.Now().Add(tt.want+2*time.Second)
			if limited.RetryAt.Before(low) || limited.RetryAt.After(high) {
				t.Errorf("RetryAt = %v; want about %v from now", limited.RetryAt, tt.want)
			}
		})
	}
}

// GraphQL is the REST root with its /v4 replaced by /graphql, on the same host.
func TestGraphQLURL(t *testing.T) {
	t.Parallel()

	for api, want := range map[string]string{
		"https://gitlab.com/api/v4":                "https://gitlab.com/api/graphql",
		"https://gitlab.example.com/gitlab/api/v4": "https://gitlab.example.com/gitlab/api/graphql",
		"http://127.0.0.1:8080":                    "http://127.0.0.1:8080/graphql",
	} {
		if got := graphQLURL(api); got != want {
			t.Errorf("graphQLURL(%s) = %s; want %s", api, got, want)
		}
	}
}
