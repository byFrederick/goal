package authoring

import (
	"context"
	"fmt"
	"maps"
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
	// definition may search.
	CodeKnowledgeSourceUnknown = "knowledge-source-unknown"
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

// KnowledgeSources names the organization of ctx's knowledge sources (spec 4 §4.8).
type KnowledgeSources interface {
	SourceNames(ctx context.Context) ([]string, error)
}

// SecretsCheck is the credentials check from ADR 0037 on: every {{secret}} the definition refers
// to exists (secret-missing) and is open to the factory (secret-not-in-scope); no environment
// sets a variable the platform keeps (reserved-env) or holds a key as plain text (plain-key);
// every knowledge source it may search exists (knowledge-source-unknown); and a merge station
// has a repository token to merge with (merge-needs-code-host). It reads secrets' names and
// scopes, never a value.
type SecretsCheck struct {
	Secrets ports.OrgSecrets
	// Knowledge names the organization's knowledge sources (goald wires knowledge.Searcher); nil
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

	var sources []string
	if c.Knowledge != nil {
		if sources, err = c.Knowledge.SourceNames(ctx); err != nil {
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

// knowledge reports the knowledge sources the definition may search that the organization lacks.
func (r *secretsRun) knowledge(sources []string) {
	k := r.f.Spec.Knowledge
	if k == nil {
		return
	}

	for i, s := range k.Sources {
		if slices.Contains(sources, s) {
			continue
		}

		p := []string{"spec", "knowledge", "sources", strconv.Itoa(i)}
		r.add(r.f.Locate("/"+strings.Join(p, "/")), domain.SeverityError, CodeKnowledgeSourceUnknown,
			"knowledge.sources: the organization has no knowledge source named "+s, config.RemoveFix("Remove "+s, p))
	}
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
