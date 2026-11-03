//go:build e2e

package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	osexec "os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// The run on secrets end-to-end test (task e2e:secrets) needs a private repository on github.com
// that is used for nothing else, a fine-grained token for it (contents, pull requests: read and
// write) and a model key. It skips when any of them is unset, as in CI.
const (
	e2eRepoURLEnv   = "GOAL_E2E_REPOSITORY_URL"
	e2eRepoTokenEnv = "GOAL_E2E_REPOSITORY_TOKEN" //nolint:gosec // a variable name, not a credential
	e2eModelKeyEnv  = "GOAL_E2E_MODEL_KEY"
)

// The throwaway stack's gateway listener and the egress proxy of its own agents network: every
// interface, so the proxy's container reaches it as host.docker.internal; the gateway answers
// only task tokens.
const (
	e2eGatewayListen = "0.0.0.0:18090"
	e2eEgressGateway = "http://host.docker.internal:18090"
	e2eEgressImage   = "ghcr.io/byfrederick/goal-egress:0.2.0"
)

// The factory's MCP server and the one tool its tools map allows; every other tool is off.
const (
	e2eServer     = "everything"
	e2eServerTool = "echo"
	e2eServerPkg  = "@modelcontextprotocol/server-everything@2.1.0"
)

// e2eRunDeadline bounds the wait for the part to merge: three agent runs, each in a large box
// that installs task and starts the MCP server.
const e2eRunDeadline = 30 * time.Minute

// ADR 0037's run start on secrets (decisions 5, 7, 8 and 10) against a real private repository: a
// version whose repository token and model key are organization secrets, whose default
// environment is large with task and an MCP server allowing one tool, and whose integrator
// merges each part into its assembly's integration branch (ADR 0033). The planner plans one part;
// the coder runs task --version, calls the allowed tool, pushes a commit and opens the part's
// pull request into the integration branch; the integrator approves and goald merges it. The line
// then holds the part at a human station, so the assembly never lands and the repository's
// default branch is never written. The agents are a real model on short, exact prompts: the
// worker has no scripted agent.
func TestE2E_RunOnSecrets(t *testing.T) { //nolint:paralleltest // its stack takes the sign-in test's ports
	repoURL, token, modelKey := os.Getenv(e2eRepoURLEnv), os.Getenv(e2eRepoTokenEnv), os.Getenv(e2eModelKeyEnv)
	if repoURL == "" || token == "" || modelKey == "" {
		t.Skipf("set %s, %s and %s to run it (task e2e:secrets)", e2eRepoURLEnv, e2eRepoTokenEnv, e2eModelKeyEnv)
	}

	suffix := randomSuffix(t)
	remote := newE2ERemote(t, repoURL, token)
	// Registered first, so it runs last: after goald has stopped and can push nothing more.
	t.Cleanup(func() { remote.deleteBranches(t, suffix) })

	defaultHead := remote.head(t, "HEAD")
	network := e2eAgentsNetwork(t, suffix)

	extra := []string{"GOAL_GATEWAY_LISTEN=" + e2eGatewayListen, "GOAL_SANDBOX_NETWORK=" + network, "GOAL_TICK_EVERY=1s"}
	if image := os.Getenv("GOAL_AGENT_IMAGE"); image != "" {
		extra = append(extra, "GOAL_AGENT_IMAGE="+image)
	}

	s := startE2EStack(t, extra...)
	s.signIn(t)

	s.createSecret(t, "repo-token", token)
	s.createSecret(t, "model-key", modelKey)
	factory := s.publishFactory(t, secretsFactory(repoURL, suffix))

	root := s.submitItem(t, factory, "e2e "+suffix, "Plan: "+e2ePlan(suffix))
	part := s.waitForPart(t, root.ID)
	partBranch := domain.Item{Number: part.Number, Title: part.Title}.Branch()
	integration := domain.IntegrationBranch(domain.Item{Number: root.Number, Title: root.Title})

	r := &secretsRun{remote: remote, suffix: suffix, partBranch: partBranch, integration: integration, defaultHead: defaultHead}
	r.pushed, r.part = s.waitForMerge(t, remote, part.ID, partBranch)
	r.events = s.events(t, part.ID)

	// The test's context is cancelled before cleanups run, so this logs the events read above.
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("part events:\n%s", describe(r.events))
		}
	})

	for _, c := range []struct {
		name  string
		check func(*testing.T)
	}{
		{"the agent's push lands on the remote", r.checkPush},
		{"the integration branch is created on the remote", r.checkIntegrationBranch},
		{"the integrator's approval merges the pull request", r.checkMerge},
		{"the allowed MCP tool call is allowed", r.checkAllowedTool},
		{"task runs in the box", r.checkTask},
		{"the box is large", r.checkSize},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			c.check(t)
		})
	}
}

// secretsRun is what the run on secrets left to check: the part once merged, its events, the
// branches and the commits seen on the remote.
type secretsRun struct {
	remote                          *e2eRemote
	suffix, partBranch, integration string
	defaultHead, pushed             string
	part                            e2eItem
	events                          []e2eEvent
}

// checkPush checks the coder's push reached the part's branch: the commit seen there is the head
// of the part's pull request.
func (r *secretsRun) checkPush(t *testing.T) {
	t.Helper()

	pr := r.remote.pullRequest(t, prURL(t, r.part))
	if r.pushed == "" || pr.Head.Ref != r.partBranch || pr.Head.SHA != r.pushed {
		t.Errorf("seen on %s: %q; the pull request's head %s at %s; want the pushed commit", r.partBranch, r.pushed, pr.Head.Ref, pr.Head.SHA)
	}
}

// checkIntegrationBranch checks the assembly's integration branch exists and holds the part's file.
func (r *secretsRun) checkIntegrationBranch(t *testing.T) {
	t.Helper()

	if r.remote.head(t, "refs/heads/"+r.integration) == "" {
		t.Errorf("no branch %s on the remote", r.integration)
	}

	if !r.remote.hasFile(t, "e2e/"+r.suffix+".txt", r.integration) {
		t.Errorf("e2e/%s.txt is not on %s", r.suffix, r.integration)
	}
}

// checkMerge checks the part's pull request merged into the integration branch with a merged
// event, and the default branch did not move.
func (r *secretsRun) checkMerge(t *testing.T) {
	t.Helper()

	url := prURL(t, r.part)

	pr := r.remote.pullRequest(t, url)
	if !pr.Merged || pr.Base.Ref != r.integration {
		t.Errorf("pull request merged %v into %s; want merged into %s", pr.Merged, pr.Base.Ref, r.integration)
	}

	if !slices.ContainsFunc(r.events, func(e e2eEvent) bool { return e.Kind == "merged" && e.Data["pull_request"] == url }) {
		t.Errorf("no merged event for %s", url)
	}

	if got := r.remote.head(t, "HEAD"); got != r.defaultHead {
		t.Errorf("the default branch moved from %s to %s", r.defaultHead, got)
	}
}

// checkAllowedTool checks the one tool the server's tools map allows ran, neither denied nor
// waiting for a person.
func (r *secretsRun) checkAllowedTool(t *testing.T) {
	t.Helper()

	tool := "mcp__" + e2eServer + "__" + e2eServerTool
	if !succeeded(r.events, func(call e2eEvent) bool { return call.Data["tool"] == tool }) {
		t.Errorf("no %s call that succeeded", tool)
	}

	for _, e := range r.events {
		denied := e.Kind == "tool_denied" && e.Data["tool"] == tool
		asked := e.Kind == "tool_approval_requested" && e.Data["connection"] == e2eServer && e.Data["tool"] == e2eServerTool

		if denied || asked {
			t.Errorf("%s event for the allowed tool: %v", e.Kind, e.Data)
		}
	}
}

// checkTask checks the environment's task tool ran in the box.
func (r *secretsRun) checkTask(t *testing.T) {
	t.Helper()

	if !succeeded(r.events, func(call e2eEvent) bool {
		input, _ := call.Data["input"].(string)
		return call.Data["tool"] == "Bash" && strings.Contains(input, "task --version")
	}) {
		t.Error("no Bash call of task --version that succeeded")
	}
}

// checkSize checks every box of the part was the environment's size, large.
func (r *secretsRun) checkSize(t *testing.T) {
	t.Helper()

	started := 0

	for _, e := range r.events {
		if e.Kind != "sandbox_started" {
			continue
		}

		started++

		if e.Data["size"] != "large" {
			t.Errorf("sandbox_started at %s has size %v; want large", e.Station, e.Data["size"])
		}
	}

	if started == 0 {
		t.Error("no sandbox_started event")
	}
}

// secretsFactory is the definition the test publishes, as an import's files: inbox, plan (the
// planner), code (the coder), integrate (the integrator's gate, action merge), hold (a person,
// so the assembly never lands) and done, in integration mode branch.
func secretsFactory(repoURL, suffix string) map[string]string {
	text := strings.NewReplacer("REPO_URL", repoURL, "SUFFIX", suffix, "SERVER_PKG", e2eServerPkg,
		"SERVER", e2eServer, "TOOL", e2eServerTool)

	return map[string]string{
		"factory.yaml": text.Replace(`apiVersion: goal/v1
kind: Factory
metadata:
  id: 7c1e2f4a-9b3d-4e6f-8a01-2b3c4d5e6f70
  name: secrets-e2e-SUFFIX
spec:
  runtime:
    model: claude-haiku-5-5
    modelKey: "{{model-key}}"
    repository: { url: "REPO_URL", token: "{{repo-token}}" }
    budget: { perItemUsd: 3, maxTurnsPerTask: 25 }
  contracts:
    plan: ./contracts/plan.schema.json
    change: ./contracts/change.schema.json
    verdict: ./contracts/verdict.schema.json
  agents:
    - name: planner
      prompt: ./agents/planner.md
      maxTurns: 5
    - name: coder
      prompt: ./agents/coder.md
      tools: { allow: [Bash, Read, Write, Edit] }
    - name: integrator
      prompt: ./agents/integrator.md
      maxTurns: 5
  board:
    stations:
      - { id: inbox, kind: queue }
      - { id: plan, kind: agent, agent: planner, output: plan }
      - { id: code, kind: agent, agent: coder, output: change }
      - { id: integrate, kind: agent, agent: integrator, output: verdict, gate: { onReject: code, maxRework: 1 }, action: merge }
      - { id: hold, kind: human }
      - { id: done, kind: done }
    flow:
      assemblies:
        integration: { mode: branch }
`),
		"environments/default.yaml": text.Replace(`apiVersion: goal/v1
kind: Environment
metadata: { name: default }
spec:
  size: large
  tools: [task@3.46.1]
  env:
    GH_TOKEN: "{{repo-token}}"
  mcp:
    SERVER:
      command: [npx, -y, "SERVER_PKG", stdio]
      tools: { TOOL: allow, "*": off }
`),
		"agents/planner.md": "# planner\n\nYou turn an item into a plan. Use no tool.\n\n" +
			"- If your brief has a section headed `## Assembly`, you work on a part of a plan already made: answer `{\"plan\": []}`.\n" +
			"- Otherwise the item's body has a line that starts with `Plan:` followed by a JSON array: answer `{\"plan\": <that array>}`, copied exactly.\n",
		"agents/coder.md": text.Replace("# coder\n\nDo exactly these steps, in order, and nothing else:\n\n" +
			"1. Run `task --version` with the Bash tool, once.\n" +
			"2. Call the tool `mcp__SERVER__TOOL` once, with the message `goal-e2e`.\n" +
			"3. Write the file your brief names, with the content it names.\n" +
			"4. Commit it with `git add -A && git commit -m \"e2e: add the check file\"`, then push your branch with `git push origin HEAD`.\n" +
			"5. Open a pull request from your branch into the base branch your Workspace section names: " +
			"`gh pr create --base <that branch> --head <your branch> --title \"e2e check\" --body \"e2e check\"`. " +
			"If one is open already, use it.\n" +
			"6. Answer `{\"pull_request\": \"<the pull request's URL>\"}`.\n"),
		"agents/integrator.md":          "# integrator\n\nUse no tool. Answer `{\"decision\": \"approve\", \"reason\": \"e2e\"}`.\n",
		"contracts/plan.schema.json":    `{"type":"object","required":["plan"],"properties":{"plan":{"type":"array"}}}`,
		"contracts/change.schema.json":  `{"type":"object","required":["pull_request"],"properties":{"pull_request":{"type":"string"}}}`,
		"contracts/verdict.schema.json": `{"type":"object","required":["decision"],"properties":{"decision":{"enum":["approve","reject"]},"reason":{"type":"string"}}}`,
	}
}

// e2ePlan is the one-part plan the item's body hands the planner.
func e2ePlan(suffix string) string {
	plan, _ := json.Marshal([]map[string]string{{
		"key": "check", "title": "part " + suffix,
		"brief": fmt.Sprintf("Write the file e2e/%s.txt with the one line %s.", suffix, suffix),
	}})

	return string(plan)
}

// createSecret creates an organization secret of the default organization open to every factory.
func (s *e2eStack) createSecret(t *testing.T, name, value string) {
	t.Helper()

	body, _ := json.Marshal(map[string]any{"name": name, "value": value, "scope": map[string]any{"all_factories": true, "factories": []string{}}})
	if res := s.do(t, http.MethodPost, "/v1/orgs/default/secrets", string(body)); res.status != http.StatusCreated {
		t.Fatalf("create secret %s = %d %s; want 201", name, res.status, res.body)
	}
}

// publishFactory imports files as a new factory of the default organization and publishes its
// draft; it returns the factory's id.
func (s *e2eStack) publishFactory(t *testing.T, files map[string]string) string {
	t.Helper()

	start := map[string]any{}
	for path, text := range files {
		start[path] = map[string]string{"text": text}
	}

	body, _ := json.Marshal(map[string]any{"start": map[string]any{"kind": "import", "files": start}})

	created := s.do(t, http.MethodPost, "/v1/orgs/default/factories", string(body))
	if created.status != http.StatusCreated {
		t.Fatalf("import the factory = %d %s; want 201", created.status, created.body)
	}

	var factory struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.body, &factory); err != nil {
		t.Fatal(err)
	}

	var draft struct {
		Revision     int64 `json:"revision"`
		ProblemCount int   `json:"problem_count"`
	}
	s.decode(t, s.do(t, http.MethodGet, "/v1/orgs/default/factories/"+factory.ID+"/draft", ""), &draft)

	publish := fmt.Sprintf(`{"revision":%d,"expected_current_version_id":null,"message":"e2e"}`, draft.Revision)
	if res := s.do(t, http.MethodPost, "/v1/orgs/default/factories/"+factory.ID+"/draft/publish", publish); res.status != http.StatusCreated {
		t.Fatalf("publish = %d %s; want 201 (the draft has %d problems)", res.status, res.body, draft.ProblemCount)
	}

	return factory.ID
}

// e2eItem is what the test reads of an item.
type e2eItem struct {
	ID      string                     `json:"id"`
	Number  int64                      `json:"number"`
	Title   string                     `json:"title"`
	Station string                     `json:"station"`
	State   string                     `json:"state"`
	Outputs map[string]json.RawMessage `json:"outputs"`
}

func (s *e2eStack) submitItem(t *testing.T, factory, title, body string) e2eItem {
	t.Helper()

	req, _ := json.Marshal(map[string]string{"title": title, "body": body})

	res := s.do(t, http.MethodPost, "/v1/orgs/default/factories/"+factory+"/items", string(req))
	if res.status != http.StatusCreated {
		t.Fatalf("submit = %d %s; want 201", res.status, res.body)
	}

	var it e2eItem
	if err := json.Unmarshal(res.body, &it); err != nil {
		t.Fatal(err)
	}

	return it
}

func (s *e2eStack) item(t *testing.T, id string) e2eItem {
	t.Helper()

	var it e2eItem
	s.decode(t, s.do(t, http.MethodGet, "/v1/orgs/default/items/"+id, ""), &it)

	return it
}

// waitForPart waits for the planner to make root an assembly of its one part.
func (s *e2eStack) waitForPart(t *testing.T, root string) e2eItem {
	t.Helper()

	deadline := time.Now().Add(e2eRunDeadline)
	for time.Now().Before(deadline) {
		var parts struct {
			Parts []struct {
				ID string `json:"id"`
			} `json:"parts"`
		}
		s.decode(t, s.do(t, http.MethodGet, "/v1/orgs/default/items/"+root+"/parts", ""), &parts)

		if len(parts.Parts) == 1 {
			return s.item(t, parts.Parts[0].ID)
		}

		if it := s.item(t, root); it.State == "blocked" || it.State == "cancelled" || it.State == "done" {
			t.Fatalf("the item is %s at %s before it became an assembly:\n%s", it.State, it.Station, describe(s.events(t, root)))
		}

		time.Sleep(5 * time.Second)
	}

	t.Fatalf("the item made no assembly of one part within %s:\n%s", e2eRunDeadline, describe(s.events(t, root)))

	return e2eItem{}
}

// waitForMerge waits until the part has a merged event, watching the remote for its branch on the
// way; it returns the last commit seen on the branch and the part.
func (s *e2eStack) waitForMerge(t *testing.T, remote *e2eRemote, part, branch string) (string, e2eItem) {
	t.Helper()

	var pushed string

	deadline := time.Now().Add(e2eRunDeadline)
	for time.Now().Before(deadline) {
		if head := remote.head(t, "refs/heads/"+branch); head != "" {
			pushed = head
		}

		it := s.item(t, part)
		if slices.ContainsFunc(s.events(t, part), func(e e2eEvent) bool { return e.Kind == "merged" }) {
			return pushed, it
		}

		if it.State == "blocked" || it.State == "cancelled" || it.State == "done" || it.Station == "hold" {
			t.Fatalf("the part is %s at %s with no merged event:\n%s", it.State, it.Station, describe(s.events(t, part)))
		}

		time.Sleep(5 * time.Second)
	}

	t.Fatalf("the part did not merge within %s:\n%s", e2eRunDeadline, describe(s.events(t, part)))

	return "", e2eItem{}
}

// prURL is the pull request the coder's output names.
func prURL(t *testing.T, part e2eItem) string {
	t.Helper()

	var out struct {
		PullRequest string `json:"pull_request"`
	}
	if err := json.Unmarshal(part.Outputs["code"], &out); err != nil || out.PullRequest == "" {
		t.Fatalf("the coder's output %s names no pull_request", part.Outputs["code"])
	}

	return out.PullRequest
}

// e2eEvent is one item event.
type e2eEvent struct {
	ID      int64          `json:"id"`
	Kind    string         `json:"kind"`
	Station string         `json:"station"`
	Data    map[string]any `json:"data"`
}

// events is every event of the item, oldest first.
func (s *e2eStack) events(t *testing.T, item string) []e2eEvent {
	t.Helper()

	var all []e2eEvent

	for after := int64(0); ; {
		var page struct {
			Events    []e2eEvent `json:"events"`
			NextAfter int64      `json:"next_after"`
		}
		s.decode(t, s.do(t, http.MethodGet, fmt.Sprintf("/v1/orgs/default/items/%s/events?after=%d&limit=1000", item, after), ""), &page)

		if len(page.Events) == 0 {
			return all
		}

		all, after = append(all, page.Events...), page.NextAfter
	}
}

// succeeded reports whether a tool_call that match picks has a tool_result, by its call id, that
// is not an error.
func succeeded(events []e2eEvent, match func(e2eEvent) bool) bool {
	for _, call := range events {
		if call.Kind != "tool_call" || !match(call) {
			continue
		}

		if slices.ContainsFunc(events, func(r e2eEvent) bool {
			return r.Kind == "tool_result" && r.Data["id"] == call.Data["id"] && r.Data["is_error"] == false
		}) {
			return true
		}
	}

	return false
}

// describe is one line per event, its kind, station and data, for a failure's log.
func describe(events []e2eEvent) string {
	var b strings.Builder

	for _, e := range events {
		data, _ := json.Marshal(e.Data)
		fmt.Fprintf(&b, "%s @%s %.300s\n", e.Kind, e.Station, data)
	}

	return b.String()
}

// e2eAgentsNetwork creates an agents network of the test's own, internal without a bridge address
// as compose.yaml's, with an egress proxy on it that forwards the gateway's paths to the test's
// goald; both are removed when the test ends. The install's network and proxy are never touched.
func e2eAgentsNetwork(t *testing.T, suffix string) string {
	t.Helper()

	network, proxy := "goal-e2e-agents-"+suffix, "goal-e2e-egress-"+suffix
	docker := func(args ...string) string {
		out, err := osexec.CommandContext(t.Context(), "docker", args...).CombinedOutput() //nolint:gosec // the test's own names
		if err != nil {
			t.Fatalf("docker %s: %v\n%s", strings.Join(args, " "), err, out)
		}

		return string(out)
	}

	docker("network", "create", "--internal", "-o", "com.docker.network.bridge.inhibit_ipv4=true", network)
	t.Cleanup(func() { _ = osexec.Command("docker", "network", "rm", network).Run() }) //nolint:gosec,noctx // the test's own network

	docker("run", "--detach", "--name", proxy, "--add-host", "host.docker.internal:host-gateway",
		"--env", "GOAL_EGRESS_GATEWAY="+e2eEgressGateway, "--read-only", "--cap-drop", "ALL", e2eEgressImage)
	t.Cleanup(func() { _ = osexec.Command("docker", "rm", "--force", proxy).Run() }) //nolint:gosec,noctx // the test's own proxy

	docker("network", "connect", "--alias", "goal-egress", network, proxy)

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if osexec.CommandContext(t.Context(), "docker", "exec", proxy, "/goal-egress", "-health").Run() == nil { //nolint:gosec // the test's own proxy
			return network
		}

		time.Sleep(500 * time.Millisecond)
	}

	t.Fatalf("the egress proxy %s was not healthy within 30s", proxy)

	return ""
}

// e2eRemote is the test repository, reached with git and GitHub's API as the token.
type e2eRemote struct {
	url, token, owner, repo string
}

func newE2ERemote(t *testing.T, repoURL, token string) *e2eRemote {
	t.Helper()

	u, err := url.Parse(repoURL)
	if err != nil || u.Host != "github.com" {
		t.Fatalf("%s %q is not a github.com repository URL", e2eRepoURLEnv, repoURL)
	}

	owner, repo, ok := strings.Cut(strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git"), "/")
	if !ok {
		t.Fatalf("%s %q names no owner/repository", e2eRepoURLEnv, repoURL)
	}

	return &e2eRemote{url: repoURL, token: token, owner: owner, repo: repo}
}

// git runs git against the remote, signed in as the token through the environment, never argv.
func (r *e2eRemote) git(t *testing.T, args ...string) (string, error) {
	t.Helper()

	cmd := osexec.Command("git", args...) //nolint:gosec,noctx // fixed git commands; also run in Cleanup, after the test's context ends
	basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + r.token))
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.extraHeader", "GIT_CONFIG_VALUE_0=Authorization: Basic "+basic)

	out, err := cmd.Output()

	return string(out), err
}

// head is the commit ref points at on the remote, empty when there is no such ref.
func (r *e2eRemote) head(t *testing.T, ref string) string {
	t.Helper()

	out, err := r.git(t, "ls-remote", r.url, ref)
	if err != nil {
		t.Fatalf("git ls-remote %s: %v", ref, err)
	}

	for line := range strings.Lines(out) {
		if sha, name, ok := strings.Cut(strings.TrimSpace(line), "\t"); ok && name == ref {
			return sha
		}
	}

	return ""
}

// deleteBranches deletes every goal/ branch on the remote whose name holds suffix: the item
// branches and the integration branch of this run, whether it passed or not.
func (r *e2eRemote) deleteBranches(t *testing.T, suffix string) {
	t.Helper()

	out, err := r.git(t, "ls-remote", "--heads", r.url, "refs/heads/goal/*")
	if err != nil {
		t.Errorf("list the remote's branches to clean up: %v", err)
		return
	}

	for line := range strings.Lines(out) {
		_, ref, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok || !strings.Contains(ref, suffix) {
			continue
		}

		if _, err := r.git(t, "push", r.url, "--delete", ref); err != nil {
			t.Errorf("delete %s on the remote: %v", ref, err)
		}
	}
}

// e2ePullRequest is what the test reads of a GitHub pull request.
type e2ePullRequest struct {
	Merged bool `json:"merged"`
	Head   struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

// pullRequest reads the pull request at prURL (https://github.com/<owner>/<repo>/pull/<n>).
func (r *e2eRemote) pullRequest(t *testing.T, prURL string) e2ePullRequest {
	t.Helper()

	prefix := fmt.Sprintf("https://github.com/%s/%s/pull/", r.owner, r.repo)

	number, ok := strings.CutPrefix(prURL, prefix)
	if !ok {
		t.Fatalf("pull request %s is not of %s/%s", prURL, r.owner, r.repo)
	}

	var pr e2ePullRequest
	if status := r.api(t, "/pulls/"+number, &pr); status != http.StatusOK {
		t.Fatalf("read pull request %s = %d", prURL, status)
	}

	return pr
}

// hasFile reports whether path exists on the remote's branch.
func (r *e2eRemote) hasFile(t *testing.T, path, branch string) bool {
	t.Helper()

	return r.api(t, "/contents/"+path+"?ref="+url.QueryEscape(branch), nil) == http.StatusOK
}

// api GETs path under the repository in GitHub's API as the token, decodes a 200 into out when
// it is set, and returns the status.
func (r *e2eRemote) api(t *testing.T, path string, out any) int {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		fmt.Sprintf("https://api.github.com/repos/%s/%s%s", r.owner, r.repo, path), nil)
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("Accept", "application/vnd.github+json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}

	if res.StatusCode == http.StatusOK && out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
	}

	return res.StatusCode
}
