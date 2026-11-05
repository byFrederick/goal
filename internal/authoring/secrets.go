package authoring

import (
	"context"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Problem codes of the secrets check (ADR 0037 decisions 5-7). All are errors but plain-key's
// warning: a version that has one cannot run.
const (
	// CodeSecretMissing: the organization has no secret of a name a {{secret}} reference names.
	CodeSecretMissing = "secret-missing"
	// CodeSecretNotInScope: a referenced secret's factory scope leaves the factory out.
	CodeSecretNotInScope = "secret-not-in-scope"
	// CodeReservedEnv: an environment file sets a variable the platform keeps (domain.ReservedRunEnv).
	CodeReservedEnv = "reserved-env"
	// CodePlainKey: a plain env value looks like a key; an error when it has a known token prefix,
	// a warning when only its name says so. Its fix moves it to a {{secret}} reference.
	CodePlainKey = "plain-key"
	// CodeKnowledgeSourceUnknown: the organization has no knowledge source of a name the
	// definition lists.
	CodeKnowledgeSourceUnknown = "knowledge-source-unknown"
	// CodeKnowledgeSecretNotInScope: a listed source reads with a secret whose factory scope
	// leaves the factory out, so listing it would hand the factory that secret's reach.
	CodeKnowledgeSecretNotInScope = "knowledge-secret-not-in-scope"
	// CodeKnowledgeCopiesCheckout: a warning; a listed Repository source reads the factory's own
	// repository, whose files its runs already have on the item's branch.
	CodeKnowledgeCopiesCheckout = "knowledge-copies-checkout"
	// CodeMergeNeedsCodeHost: a station merges, but runtime.repository has no token, so nothing
	// could merge the item's pull requests.
	CodeMergeNeedsCodeHost = "merge-needs-code-host"
	// CodeRepositoryHostUnlisted: a repository's URL is on a host the organization doesn't list,
	// neither built in nor added, so its token would go to a host of the author's choosing (ADR
	// 0041 decision 1).
	CodeRepositoryHostUnlisted = "repository-host-unlisted"
	// CodeSecretSignsWebhook: a {{secret}} reference names a secret an organization webhook is
	// signed by, which no run may hold, or it could forge deliveries (ADR 0040 decision 15).
	CodeSecretSignsWebhook = "secret-signs-webhook"
)

// FixMoveToSecrets is the label of plain-key's fix.
const FixMoveToSecrets = "Move to Secrets"

// keyNameSuffixes end the names of variables that usually hold a key.
var keyNameSuffixes = []string{"_TOKEN", "_KEY", "_SECRET", "_PASSWORD"}

// WebhookSecrets lists the secrets the organization of ctx's webhooks are signed by
// (ports.Intake's WebhookSecretUses).
type WebhookSecrets interface {
	WebhookSecretUses(ctx context.Context) ([]domain.WebhookSecretUse, error)
}

// KnowledgeSources lists the organization of ctx's knowledge sources (ports.Knowledge's Sources).
type KnowledgeSources interface {
	Sources(ctx context.Context) ([]domain.KnowledgeSource, error)
}

// SecretsCheck is the credentials check from ADR 0037 on: every {{secret}} the definition refers
// to exists (secret-missing), signs no webhook (secret-signs-webhook) and is open to the factory
// (secret-not-in-scope); no environment
// sets a variable the platform keeps (reserved-env) or holds a key as plain text (plain-key);
// every knowledge source it lists exists (knowledge-source-unknown), reads with no secret closed
// to the factory (knowledge-secret-not-in-scope) and, a warning, does not copy the factory's own
// repository (knowledge-copies-checkout); a merge station has a repository token to merge
// with (merge-needs-code-host); and every repository is on a code host the organization lists
// (repository-host-unlisted). It warns when an agent that only reads code, or a check, may call
// GitHub MCP tools that write (github-tools-not-read-only) or whose access is untested
// (github-tools-unknown). It reads secrets' names and scopes, never a value.
type SecretsCheck struct {
	Secrets ports.OrgSecrets
	// Knowledge lists the organization's knowledge sources (goald wires the knowledge store); nil
	// is an organization without any.
	Knowledge KnowledgeSources
	// Webhooks lists the secrets the organization's webhooks are signed by (goald wires the
	// intake store); nil is an organization without webhooks.
	Webhooks WebhookSecrets
	// Hosts lists the code hosts the organization added (goald wires the store's ports.CodeHosts);
	// nil is an organization with the built-in hosts only.
	Hosts OrgCodeHosts
	// Tests lists the factory's kept environment tests (goald wires the store's
	// ports.EnvironmentTests), which the GitHub tools warnings read; nil skips those warnings.
	Tests EnvironmentTestList
}

// OrgCodeHosts lists the code hosts the organization of ctx added (ports.CodeHosts' CodeHosts).
type OrgCodeHosts interface {
	CodeHosts(ctx context.Context) ([]domain.CodeHost, error)
}

var _ Credentials = SecretsCheck{}

// Check implements Credentials.
func (c SecretsCheck) Check(ctx context.Context, f *config.Factory) ([]domain.DefinitionProblem, error) {
	list, err := c.Secrets.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list secrets: %w", err)
	}

	var sources []domain.KnowledgeSource
	if c.Knowledge != nil {
		if sources, err = c.Knowledge.Sources(ctx); err != nil {
			return nil, fmt.Errorf("list knowledge sources: %w", err)
		}
	}

	r := &secretsRun{f: f, secrets: make(map[string]domain.Secret, len(list)), signs: map[string][]string{}}
	for _, s := range list {
		r.secrets[s.Name] = s
	}

	if c.Webhooks != nil {
		uses, err := c.Webhooks.WebhookSecretUses(ctx)
		if err != nil {
			return nil, fmt.Errorf("list webhook secrets: %w", err)
		}

		for _, u := range uses {
			r.signs[u.Secret] = append(r.signs[u.Secret], u.Webhook)
		}
	}

	hosts := domain.BuiltInCodeHosts()

	if c.Hosts != nil {
		added, err := c.Hosts.CodeHosts(ctx)
		if err != nil {
			return nil, fmt.Errorf("list code hosts: %w", err)
		}

		hosts = append(hosts, added...)
	}

	r.references()
	r.env()
	r.knowledge(sources)
	r.merge()
	r.repositoryHosts(hosts)

	if c.Tests != nil {
		tests, err := c.Tests.List(ctx, domain.FactoryID(f.Metadata.ID))
		if err != nil {
			return nil, fmt.Errorf("list environment tests: %w", err)
		}

		r.gitHubTools(tests)
	}

	return r.out, nil
}

// secretsRun is one definition checked against one organization's secrets.
type secretsRun struct {
	f       *config.Factory
	secrets map[string]domain.Secret
	// signs are the webhooks each secret signs, by secret name.
	signs map[string][]string
	out   []domain.DefinitionProblem
}

func (r *secretsRun) add(l config.Location, sev domain.ProblemSeverity, code, msg string, fixes ...domain.ProblemFix) {
	p := l.Problem(CheckSecrets, code, sev, msg)
	p.Fixes = fixes
	r.out = append(r.out, p)
}

// references reports each {{secret}} that names no secret of the organization, one a webhook is
// signed by, or one whose scope leaves the factory out.
func (r *secretsRun) references() {
	factory := domain.FactoryID(r.f.Metadata.ID)

	for _, ref := range r.f.SecretRefs() {
		s, ok := r.secrets[ref.Name]

		switch {
		case !ok:
			r.add(ref.Location, domain.SeverityError, CodeSecretMissing, fmt.Sprintf(
				"%s: the organization has no secret named %s; create it on the Secrets page", ref.Where, ref.Name))
		case len(r.signs[ref.Name]) > 0:
			r.add(ref.Location, domain.SeverityError, CodeSecretSignsWebhook, fmt.Sprintf(
				"%s: the secret %s signs the webhook %s, and no run may hold it, or it could forge deliveries; use another secret",
				ref.Where, ref.Name, strings.Join(r.signs[ref.Name], ", ")))
		case !s.Scope.Allows(factory):
			r.add(ref.Location, domain.SeverityError, CodeSecretNotInScope, fmt.Sprintf(
				"%s: the secret %s is not open to this factory; add the factory to its scope on the Secrets page", ref.Where, ref.Name))
		}
	}
}

// env reports each environment variable the platform keeps, and each plain value that looks
// like a key, with a fix that refers to a secret named after the variable instead.
func (r *secretsRun) env() {
	for _, e := range r.f.Environments() {
		for _, name := range slices.Sorted(maps.Keys(e.Spec.Env)) {
			path := []string{"spec", "env", name}
			value := e.Spec.Env[name]

			if domain.ReservedRunEnv(name) {
				r.add(e.Locate(path...), domain.SeverityError, CodeReservedEnv, fmt.Sprintf(
					"%s env.%s: the platform keeps this variable for itself (GOAL_*, HOME, PATH, the proxy variables…); name it otherwise",
					e.File(), name))

				continue
			}

			sev, why := plainKey(name, value)
			if why == "" {
				continue
			}

			secret := kebab(name)
			r.add(e.Locate(path...), sev, CodePlainKey, fmt.Sprintf(
				"%s env.%s: %s; keep it in a secret and refer to it as {{%s}}", e.File(), name, why, secret),
				e.ReplaceFix(FixMoveToSecrets, path, "{{"+secret+"}}"))
		}
	}
}

// plainKey says why a plain env value looks like a key, "" when it does not or refers to a
// secret: an error for a known token prefix, a warning for a name only.
func plainKey(name, value string) (domain.ProblemSeverity, string) {
	if strings.Contains(value, "{{") || strings.TrimSpace(value) == "" {
		return "", ""
	}

	for _, p := range domain.KeyPrefixes {
		if strings.HasPrefix(strings.TrimSpace(value), p) {
			return domain.SeverityError, fmt.Sprintf("the value starts with %s, as a key does, and is written in plain text", p)
		}
	}

	upper := strings.ToUpper(name)
	for _, s := range keyNameSuffixes {
		if strings.HasSuffix(upper, s) {
			return domain.SeverityWarning, "the name says it holds a key, but the value is written in plain text"
		}
	}

	return "", ""
}

// kebab is a variable's name as a secret's: GH_TOKEN is gh-token.
func kebab(name string) string {
	return strings.Trim(strings.ReplaceAll(strings.ToLower(name), "_", "-"), "-")
}

// knowledge reports each knowledge source the definition lists that the organization lacks, that
// reads with a secret not open to the factory, or that reads the factory's own repository.
func (r *secretsRun) knowledge(sources []domain.KnowledgeSource) {
	k := r.f.Spec.Knowledge
	if k == nil {
		return
	}

	factory := domain.FactoryID(r.f.Metadata.ID)

	for i, name := range k.Names() {
		p := []string{"spec", "knowledge", "sources", strconv.Itoa(i)}
		at := r.f.Locate("/" + strings.Join(p, "/"))

		j := slices.IndexFunc(sources, func(s domain.KnowledgeSource) bool { return s.Name == name })
		if j < 0 {
			r.add(at, domain.SeverityError, CodeKnowledgeSourceUnknown,
				"knowledge.sources: the organization has no knowledge source named "+name, config.RemoveFix("Remove "+name, p))

			continue
		}

		src := sources[j]

		if s, ok := r.secrets[src.Secret]; ok && !s.Scope.Allows(factory) {
			r.add(at, domain.SeverityError, CodeKnowledgeSecretNotInScope, fmt.Sprintf(
				"knowledge.sources: the source %s reads with the secret %s, which is not open to this factory; "+
					"add the factory to the secret's scope on the Secrets page, or remove the source", name, src.Secret))
		}

		if repo := r.f.Spec.Runtime.Repository; repo != nil && src.CopiesCheckout(repo.URL) {
			r.add(at, domain.SeverityWarning, CodeKnowledgeCopiesCheckout, fmt.Sprintf(
				"knowledge.sources: the source %s reads runtime.repository, %s, whose files every run already has on the item's branch",
				name, repo.URL))
		}
	}
}

// merge reports each merge station of a factory with no repository that writes with a token:
// nothing could merge the item's pull requests. The fix takes the merge off the station.
func (r *secretsRun) merge() {
	if slices.ContainsFunc(r.f.Spec.Runtime.RepositoryEntries(), func(e config.RepositoryEntry) bool {
		return e.Access == config.AccessWrite && e.Token != ""
	}) {
		return
	}

	for i, s := range r.f.Spec.Board.Stations {
		if s.Action != config.ActionMerge {
			continue
		}

		p := []string{"spec", "board", "stations", strconv.Itoa(i), "action"}
		r.add(r.f.Locate("/"+strings.Join(p, "/")), domain.SeverityError, CodeMergeNeedsCodeHost, fmt.Sprintf(
			"station %s: action merge needs runtime.repository.token, the {{secret}} that merges the item's pull requests", s.ID),
			config.RemoveFix("Remove the merge step", p))
	}
}

// repositoryHosts reports each repository, the shorthand or an entry, whose URL's host (with its
// port, if it has one) is none of hosts: goald sends repository tokens only to listed hosts.
func (r *secretsRun) repositoryHosts(hosts []domain.CodeHost) {
	listed := func(raw string) (string, bool) {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return "", true // the schema and the repositories check refuse it
		}

		h := strings.ToLower(u.Host)
		_, ok := domain.ListedCodeHost(hosts, h)

		return h, ok
	}

	unlisted := func(p, what, host string) {
		r.add(r.f.Locate(p), domain.SeverityError, CodeRepositoryHostUnlisted, fmt.Sprintf(
			"%s: %s is not one of the organization's code hosts, so its token would go to a host nobody listed; "+
				"an owner adds it under Code hosts in the organization's settings", what, host))
	}

	rt := r.f.Spec.Runtime
	if s := rt.Repository; s != nil {
		if h, ok := listed(s.URL); !ok {
			unlisted("/spec/runtime/repository/url", "runtime.repository", h)
		}
	}

	for i, e := range rt.Repositories {
		if h, ok := listed(e.URL); !ok {
			unlisted("/spec/runtime/repositories/"+strconv.Itoa(i)+"/url", "repository "+e.Name, h)
		}
	}
}
