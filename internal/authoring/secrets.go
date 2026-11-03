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
)

// FixMoveToSecrets is the label of plain-key's fix.
const FixMoveToSecrets = "Move to Secrets"

// keyPrefixes start the values of tokens and keys that are never a plain setting: Anthropic and
// OpenAI keys, GitHub classic and fine-grained tokens, GitLab tokens and Slack bot and user tokens.
var keyPrefixes = []string{"sk-", "ghp_", "github_pat_", "glpat-", "xoxb-", "xoxp-"}

// keyNameSuffixes end the names of variables that usually hold a key.
var keyNameSuffixes = []string{"_TOKEN", "_KEY", "_SECRET", "_PASSWORD"}

// KnowledgeSources lists the organization of ctx's knowledge sources (ports.Knowledge's Sources).
type KnowledgeSources interface {
	Sources(ctx context.Context) ([]domain.KnowledgeSource, error)
}

// sourceRepositoryKey is the config key of the repository a Repository source, or a synced
// source read by gitdocs, reads.
const sourceRepositoryKey = "repository"

// SecretsCheck is the credentials check from ADR 0037 on: every {{secret}} the definition refers
// to exists (secret-missing) and is open to the factory (secret-not-in-scope); no environment
// sets a variable the platform keeps (reserved-env) or holds a key as plain text (plain-key);
// every knowledge source it lists exists (knowledge-source-unknown), reads with no secret closed
// to the factory (knowledge-secret-not-in-scope) and, a warning, does not copy the factory's own
// repository (knowledge-copies-checkout); and a merge station has a repository token to merge
// with (merge-needs-code-host). It reads secrets' names and scopes, never a value.
type SecretsCheck struct {
	Secrets ports.OrgSecrets
	// Knowledge lists the organization's knowledge sources (goald wires the knowledge store); nil
	// is an organization without any.
	Knowledge KnowledgeSources
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

	r := &secretsRun{f: f, secrets: make(map[string]domain.Secret, len(list))}
	for _, s := range list {
		r.secrets[s.Name] = s
	}

	r.references()
	r.env()
	r.knowledge(sources)
	r.merge()

	return r.out, nil
}

// secretsRun is one definition checked against one organization's secrets.
type secretsRun struct {
	f       *config.Factory
	secrets map[string]domain.Secret
	out     []domain.DefinitionProblem
}

func (r *secretsRun) add(l config.Location, sev domain.ProblemSeverity, code, msg string, fixes ...domain.ProblemFix) {
	p := l.Problem(CheckSecrets, code, sev, msg)
	p.Fixes = fixes
	r.out = append(r.out, p)
}

// references reports each {{secret}} that names no secret of the organization, or one whose
// scope leaves the factory out.
func (r *secretsRun) references() {
	factory := domain.FactoryID(r.f.Metadata.ID)

	for _, ref := range r.f.SecretRefs() {
		s, ok := r.secrets[ref.Name]

		switch {
		case !ok:
			r.add(ref.Location, domain.SeverityError, CodeSecretMissing, fmt.Sprintf(
				"%s: the organization has no secret named %s; create it on the Secrets page", ref.Where, ref.Name))
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

	for _, p := range keyPrefixes {
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

		if repo := r.f.Spec.Runtime.Repository; repo != nil && copiesCheckout(src, repo.URL) {
			r.add(at, domain.SeverityWarning, CodeKnowledgeCopiesCheckout, fmt.Sprintf(
				"knowledge.sources: the source %s reads runtime.repository, %s, whose files every run already has on the item's branch",
				name, repo.URL))
		}
	}
}

// copiesCheckout reports whether src reads the repository at checkout: a Repository source, or a
// synced source that gitdocs reads, until the cut-over converts it (ADR 0038 decision 16), whose
// repository has the same host and path, whatever their case, a trailing slash or .git.
func copiesCheckout(src domain.KnowledgeSource, checkout string) bool {
	if src.Kind != domain.SourceRepository && src.Kind != domain.SourceSynced {
		return false
	}

	a, b := repositoryKey(src.Config[sourceRepositoryKey]), repositoryKey(checkout)

	return a != "" && a == b
}

// repositoryKey is a repository URL as host/owner/repo in lower case, "" for a value that is not
// an absolute URL.
func repositoryKey(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}

	path := strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
	if path == "" {
		return ""
	}

	return strings.ToLower(u.Host + "/" + path)
}

// merge reports each merge station of a factory whose repository has no token: nothing could
// merge the item's pull requests. The fix takes the merge off the station.
func (r *secretsRun) merge() {
	if repo := r.f.Spec.Runtime.Repository; repo != nil && repo.Token != "" {
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
