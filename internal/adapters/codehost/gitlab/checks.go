package gitlab

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// ciPermission is what a token needs to see a project's pipelines.
const ciPermission = "the read_api scope and a role in the project that can see its pipelines"

// rateLimitWait is the wait after a 429 that names no time.
const rateLimitWait = time.Minute

// pipelineFields are the fields of a pipeline Checks reads. Its result is the pipeline's own
// status; jobs are read only to name what failed, 20 of them: enough to name ports.MaxFailedJobs
// after leaving out the allowed failures.
const pipelineFields = `id name status ref source path
failedJobs: jobs(statuses: [FAILED], retried: false, first: 20) { nodes { name allowFailure } }`

// mergeRequestQuery reads a merge request's current head and the pipeline GitLab matched to it
// (by sha or source sha, so detached, merged-result and merge-train pipelines are found). REST's
// head_pipeline is not matched to the head and is never read.
const mergeRequestQuery = `query($fullPath: ID!, $iid: String!) {
  project(fullPath: $fullPath) {
    jobsEnabled
    mergeRequest(iid: $iid) { diffHeadSha headPipeline { ` + pipelineFields + ` } }
  }
}`

// pipelinesQuery reads a commit's pipelines, the newest first: those of its branch and tags and
// those external CI posts as commit statuses.
const pipelinesQuery = `query($fullPath: ID!, $sha: String!) {
  project(fullPath: $fullPath) {
    jobsEnabled
    pipelines(sha: $sha, first: 50) { nodes { ` + pipelineFields + ` } }
  }
}`

// pipelineJSON is a pipeline as GraphQL answers it.
type pipelineJSON struct {
	ID         string  `json:"id"`
	Name       *string `json:"name"`
	Status     string  `json:"status"`
	Ref        string  `json:"ref"`
	Source     string  `json:"source"`
	Path       string  `json:"path"`
	FailedJobs struct {
		Nodes []struct {
			Name         string `json:"name"`
			AllowFailure bool   `json:"allowFailure"`
		} `json:"nodes"`
	} `json:"failedJobs"`
}

// projectJSON is the part of GraphQL's project both queries read.
type projectJSON struct {
	JobsEnabled  bool `json:"jobsEnabled"`
	MergeRequest *struct {
		DiffHeadSHA  string        `json:"diffHeadSha"`
		HeadPipeline *pipelineJSON `json:"headPipeline"`
	} `json:"mergeRequest"`
	Pipelines struct {
		Nodes []pipelineJSON `json:"nodes"`
	} `json:"pipelines"`
}

// Checks implements ports.CodeHost with GitLab's GraphQL API, at the REST root with its /v4
// replaced by /graphql. With a merge request URL it reads the merge request's current head
// (diffHeadSha) and its headPipeline first, then the head's pipelines (project.pipelines(sha:)),
// keeping the latest of each ref and source. Each pipeline is one check, its state and result
// from the pipeline's own status. No pipelines with jobsEnabled false is read further from REST
// GET /projects/:id: builds_access_level disabled is no CI, anything else CI the token cannot
// see (a *ports.CINotReadableError). A 429 is a *ports.RateLimitedError.
func (cl *Client) Checks(ctx context.Context, repo domain.CodeRepository, token, sha, mrURL string) (ports.CommitChecks, error) {
	var (
		m   mergeRequest
		err error
	)

	if mrURL != "" {
		m, err = cl.open(repo, token, mrURL)
	} else {
		m, err = cl.check(repo, token)
	}

	if err != nil {
		return ports.CommitChecks{}, fmt.Errorf("checks: %w", err)
	}

	fullPath := repo.Owner + "/" + repo.Name
	out := ports.CommitChecks{Head: sha}

	var pipelines []pipelineJSON

	if mrURL != "" {
		var p projectJSON
		if err := cl.graphQL(ctx, m, mergeRequestQuery, map[string]any{"fullPath": fullPath, "iid": m.iid}, &p); err != nil {
			return ports.CommitChecks{}, fmt.Errorf("checks of %s: %w", mrURL, err)
		}

		if p.MergeRequest == nil {
			return ports.CommitChecks{}, fmt.Errorf("checks of %s: GitLab shows no such merge request: %w", mrURL, ports.ErrNotFound)
		}

		out.Head = p.MergeRequest.DiffHeadSHA
		if p.MergeRequest.HeadPipeline != nil {
			pipelines = append(pipelines, *p.MergeRequest.HeadPipeline)
		}
	}

	if out.Head == "" || strings.Contains(out.Head, "..") {
		return ports.CommitChecks{}, fmt.Errorf("checks of %s: %q is not a commit", repo, out.Head)
	}

	var p projectJSON
	if err := cl.graphQL(ctx, m, pipelinesQuery, map[string]any{"fullPath": fullPath, "sha": out.Head}, &p); err != nil {
		return ports.CommitChecks{}, fmt.Errorf("checks of %s at %s: %w", repo, out.Head, err)
	}

	pipelines = append(pipelines, p.Pipelines.Nodes...)

	if len(pipelines) == 0 && !p.JobsEnabled {
		if err := cl.ciVisible(ctx, m); err != nil {
			return ports.CommitChecks{}, fmt.Errorf("checks of %s: %w", repo, err)
		}

		return out, nil
	}

	out.Checks = checksOf(repo.Host.Host, pipelines)

	return out, nil
}

// checksOf is one check per pipeline, in order, leaving out a pipeline already listed and one
// whose ref and source an earlier (newer) pipeline has.
func checksOf(host string, pipelines []pipelineJSON) []ports.Check {
	type key struct{ ref, source string }

	var (
		out  []ports.Check
		ids  = map[string]bool{}
		refs = map[key]bool{}
	)

	for _, p := range pipelines {
		k := key{p.Ref, p.Source}
		if ids[p.ID] || refs[k] {
			continue
		}

		ids[p.ID], refs[k] = true, true
		out = append(out, checkOf(host, p))
	}

	return out
}

// checkOf is pipeline p as a check: named by its name, else its source and ref.
func checkOf(host string, p pipelineJSON) ports.Check {
	ch := ports.Check{}
	ch.State, ch.Result = pipelineState(p.Status)

	switch {
	case p.Name != nil && *p.Name != "":
		ch.Name = *p.Name
	case p.Source != "":
		ch.Name = p.Source + " pipeline on " + p.Ref
	default:
		ch.Name = "pipeline on " + p.Ref
	}

	if strings.HasPrefix(p.Path, "/") {
		ch.URL = "https://" + host + p.Path
	}

	if ch.Result == ports.CheckFailed {
		for _, j := range p.FailedJobs.Nodes {
			if !j.AllowFailure && len(ch.FailedJobs) < ports.MaxFailedJobs {
				ch.FailedJobs = append(ch.FailedJobs, j.Name)
			}
		}
	}

	return ch
}

// pipelineState maps a pipeline's status. Skipped and manual pipelines are finished, neutral:
// nothing more happens without a person. A status GitLab adds later is pending, never passed.
func pipelineState(status string) (ports.CheckState, ports.CheckResult) {
	switch status {
	case "RUNNING", "CANCELING":
		return ports.CheckRunning, ""
	case "SUCCESS":
		return ports.CheckFinished, ports.CheckPassed
	case "FAILED", "CANCELED":
		return ports.CheckFinished, ports.CheckFailed
	case "SKIPPED", "MANUAL":
		return ports.CheckFinished, ports.CheckNeutral
	default: // CREATED, WAITING_FOR_RESOURCE, PREPARING, WAITING_FOR_CALLBACK, PENDING, SCHEDULED
		return ports.CheckPending, ""
	}
}

// ciVisible tells CI that is off from CI the token cannot see, both of which GitLab answers with
// no pipelines and jobsEnabled false: the project's builds_access_level is disabled only when CI
// is off. A missing level is not knowing, so it is CI the token cannot see.
func (cl *Client) ciVisible(ctx context.Context, m mergeRequest) error {
	var p struct {
		BuildsAccessLevel *string `json:"builds_access_level"`
	}

	status, msg, header, err := cl.exchange(ctx, m, http.MethodGet, m.path(""), nil, &p, maxBodyBytes)

	switch {
	case err != nil:
		return fmt.Errorf("CI access level: %w", err)
	case status == http.StatusTooManyRequests:
		return rateLimited(header)
	case status == http.StatusNotFound:
		return fmt.Errorf("CI access level: GitLab answered 404: %s: %w", msg, ports.ErrNotFound)
	case status != http.StatusOK:
		return fmt.Errorf("CI access level: GitLab answered %d: %s", status, msg)
	case p.BuildsAccessLevel != nil && *p.BuildsAccessLevel == "disabled":
		return nil
	default:
		return &ports.CINotReadableError{Permission: ciPermission}
	}
}

// graphQLURL is the GraphQL endpoint of the REST root api: its /v4 replaced by /graphql, so a
// host under a relative URL root keeps it, else /graphql appended.
func graphQLURL(api string) string {
	if root, ok := strings.CutSuffix(api, "/v4"); ok {
		return root + "/graphql"
	}

	return api + "/graphql"
}

// graphQL posts query with vars and decodes the answer's project into out. GraphQL answers 200
// with an errors array, which is an error, never no pipelines; a null project is
// ports.ErrNotFound, as GitLab answers a project the token cannot see.
func (cl *Client) graphQL(ctx context.Context, m mergeRequest, query string, vars map[string]any, out *projectJSON) error {
	gql := m
	gql.api = graphQLURL(m.api)

	var answer struct {
		Data *struct {
			Project *projectJSON `json:"project"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	status, msg, header, err := cl.exchange(ctx, gql, http.MethodPost, "", map[string]any{"query": query, "variables": vars}, &answer, maxBodyBytes)

	switch {
	case err != nil:
		return fmt.Errorf("GraphQL: %w", err)
	case status == http.StatusTooManyRequests:
		return rateLimited(header)
	case status != http.StatusOK:
		return fmt.Errorf("GraphQL: GitLab answered %d: %s", status, msg)
	}

	if len(answer.Errors) > 0 {
		msgs := make([]string, 0, len(answer.Errors))
		for _, e := range answer.Errors {
			msgs = append(msgs, strings.ReplaceAll(e.Message, m.token, "[redacted]"))
		}

		return fmt.Errorf("GraphQL: %s", strings.Join(msgs, "; "))
	}

	if answer.Data == nil || answer.Data.Project == nil {
		return fmt.Errorf("GraphQL: GitLab shows no project %s to the token: %w", vars["fullPath"], ports.ErrNotFound)
	}

	*out = *answer.Data.Project

	return nil
}

// rateLimited is a 429's retry time: Retry-After seconds from now, else the RateLimit-Reset unix
// time, else rateLimitWait from now.
func rateLimited(header http.Header) error {
	now := time.Now()
	retryAt := now.Add(rateLimitWait)

	if s, err := strconv.Atoi(header.Get("Retry-After")); err == nil && s >= 0 {
		retryAt = now.Add(time.Duration(s) * time.Second)
	} else if u, err := strconv.ParseInt(header.Get("RateLimit-Reset"), 10, 64); err == nil && u > 0 {
		retryAt = time.Unix(u, 0)
	}

	return &ports.RateLimitedError{RetryAt: retryAt}
}
