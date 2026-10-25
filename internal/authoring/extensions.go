package authoring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Credentials checks what a definition signs in with, in the organization of ctx: the connections
// check (CheckConnections). B shipped the interim EnvReferences; from phase 4 goald wires
// ConnectionsCheck, which runs EnvReferences too until the cutover. Its problems carry Check =
// CheckConnections; an error is a failure to check, never a finding.
type Credentials interface {
	Check(ctx context.Context, f *config.Factory) ([]domain.DefinitionProblem, error)
}

// CredentialFinding is one secret reference that does not resolve for the factory.
type CredentialFinding struct {
	Use      config.SecretUse
	Code     string
	Severity domain.ProblemSeverity
	// Message names the reference, never a value.
	Message string
}

// Problem codes of EnvReferences.
const (
	// CodeSecretEnvUnset: an env:NAME reference whose variable is unset or empty in goald's
	// environment, or that names no variable.
	CodeSecretEnvUnset = "secret-env-unset" //nolint:gosec // a problem code, not a credential
	// CodeEnvReserved: an env:NAME reference to one of goald's own settings.
	CodeEnvReserved = "env-reserved"
	// CodeCredentialsUnavailable: a reference of another secrets provider (op://, arn:) this
	// install cannot resolve.
	CodeCredentialsUnavailable = "credentials-unavailable"
	// CodeRepositoryHostNotAllowed: a runtime.repository token for a url on a host the install
	// does not clone from (EnvReferences.RepositoryHosts).
	CodeRepositoryHostNotAllowed = "repository-host-not-allowed"
)

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// EnvReferences is the credentials check until phase 4 (docs/specs/README.md, "Interim rule
// until phase 4"): model and repository access are platform secrets of the install, so every
// env:NAME reference must name a variable that is set and non-empty in goald's environment
// (resolved through Secrets, the value discarded) and is not one of goald's own settings
// (Policy.Reserved, such as GOAL_DATABASE_URL and GOAL_GATEWAY_KEY), and the repository's token is
// refused for a repository on a host the install does not clone from (RepositoryHosts). There
// is nothing to grant, so there are no grant, expiry or purpose checks. From phase 4 it checks
// the env: references of the removed forms, which the engine and the MCP gateway still resolve,
// until the cutover (spec 4, 4-14) removes them.
type EnvReferences struct {
	Secrets ports.Secrets
	// Policy names the variables definitions may not read (Reserved) and those holding the
	// install's repository token (RepositoryOnly), which serve runtime.repository and, in a
	// connection, only the headers of an https connection to one of its ConnectionHosts. goald
	// hands the MCP gateway the same policy, which enforces it again on published versions.
	// Phase 4's connectors replace this with a credential per connection.
	Policy domain.SecretPolicy
	// RepositoryHosts are the hosts the install clones and merges on (host[:port], matched
	// exactly without case; default github.com), the binding gitworkspace enforces at clone
	// time: a runtime.repository whose url is on another host is refused at its token
	// (repository-host-not-allowed), whichever reference that is, since the token would be sent
	// there.
	RepositoryHosts []string
}

var _ Credentials = EnvReferences{}

// foreignPrefixes start references of secrets providers this install does not have.
var foreignPrefixes = []string{"op://", "arn:"}

// Check implements Credentials over the definition's secret references (config.Factory
// .SecretUses); literals among them are left to config's literal-secret warning.
func (e EnvReferences) Check(ctx context.Context, f *config.Factory) ([]domain.DefinitionProblem, error) {
	findings, err := e.Findings(ctx, f.SecretUses())
	if err != nil {
		return nil, err
	}

	out := make([]domain.DefinitionProblem, 0, len(findings))
	for _, fd := range findings {
		out = append(out, f.Locate(fd.Use.Doc).Problem(CheckConnections, fd.Code, fd.Severity, fd.Message))
	}

	return out, nil
}

// Findings checks secret references. Each reference is resolved once, however many places use
// it.
func (e EnvReferences) Findings(ctx context.Context, uses []config.SecretUse) ([]CredentialFinding, error) {
	resolved := map[string]*CredentialFinding{}

	var out []CredentialFinding

	for _, u := range uses {
		if u.Literal() {
			continue
		}

		if f := e.offHost(u); f != nil {
			out = append(out, *f)
			continue
		}

		if f := e.misplaced(u); f != nil {
			out = append(out, *f)
			continue
		}

		f, seen := resolved[u.Ref]
		if !seen {
			var err error
			if f, err = e.check(ctx, u.Ref); err != nil {
				return nil, err
			}

			resolved[u.Ref] = f
		}

		if f != nil {
			finding := *f
			finding.Use = u
			out = append(out, finding)
		}
	}

	return out, nil
}

// offHost refuses (repository-host-not-allowed) the repository's token when the repository's
// url is not on one of RepositoryHosts; nil for any other use.
func (e EnvReferences) offHost(u config.SecretUse) *CredentialFinding {
	if u.Purpose != config.PurposeRepository {
		return nil
	}

	hosts := []string{"github.com"}
	if len(e.RepositoryHosts) > 0 {
		hosts = make([]string, 0, len(e.RepositoryHosts))
		for _, h := range e.RepositoryHosts {
			hosts = append(hosts, strings.ToLower(h))
		}
	}

	target, err := url.Parse(u.URL)
	if err == nil && slices.Contains(hosts, strings.ToLower(target.Host)) {
		return nil
	}

	host := u.URL
	if err == nil && target.Host != "" {
		host = target.Host
	}

	return &CredentialFinding{
		Use: u, Code: CodeRepositoryHostNotAllowed, Severity: domain.SeverityError,
		Message: fmt.Sprintf("the repository is on %s, but this install clones only from %s: its repository token is sent nowhere else",
			host, strings.Join(hosts, " or ")),
	}
}

// misplaced refuses (env-reserved) a repository token used anywhere but the repository and the
// https headers of a connection to one of Policy.ConnectionHosts; nil when u may use its
// reference.
func (e EnvReferences) misplaced(u config.SecretUse) *CredentialFinding {
	if u.Purpose == config.PurposeRepository || e.Policy.ForConnection(u.Ref, u.URL) != domain.SecretOffHost {
		return nil
	}

	return &CredentialFinding{
		Use: u, Code: CodeEnvReserved, Severity: domain.SeverityError,
		Message: fmt.Sprintf("%s is the repository's token: a connection may send it only in the headers of https://%s",
			u.Ref, strings.Join(e.Policy.ConnectionHosts, " or https://")),
	}
}

// check returns the finding of one reference, nil when it resolves.
func (e EnvReferences) check(ctx context.Context, ref string) (*CredentialFinding, error) {
	finding := func(code, format string, a ...any) *CredentialFinding {
		return &CredentialFinding{Code: code, Severity: domain.SeverityError, Message: fmt.Sprintf(format, a...)}
	}

	for _, p := range foreignPrefixes {
		if strings.HasPrefix(ref, p) {
			return finding(CodeCredentialsUnavailable, "%s references cannot be resolved on this install; use env:NAME", p), nil
		}
	}

	name, ok := strings.CutPrefix(ref, "env:")
	if !ok {
		return nil, nil
	}

	if !envName.MatchString(name) {
		return finding(CodeSecretEnvUnset, "%q does not name an environment variable", ref), nil
	}

	if e.Policy.Reserves(name) {
		return finding(CodeEnvReserved, "env:%s is a setting of goald itself; a definition cannot read it", name), nil
	}

	v, err := e.Secrets.Resolve(ctx, ref)

	switch {
	case errors.Is(err, ports.ErrSecretNotFound), err == nil && v == "":
		return finding(CodeSecretEnvUnset, "env:%s is not set in goald's environment", name), nil
	case err != nil:
		return nil, fmt.Errorf("resolve env:%s: %w", name, err)
	}

	return nil, nil
}

// Check is a validation step another phase registers (phase 4's connector checker). Its
// problems carry Check = ID().
type Check interface {
	ID() string
	Check(ctx context.Context, in CheckInput) []domain.DefinitionProblem
}

// CheckInput is the definition a Check validates.
type CheckInput struct {
	// Factory is nil when factory.yaml does not parse or fails the schema.
	Factory *config.Factory
	Model   Model
	Ranges  Ranges
	Files   fs.FS
}

// Resolver turns a definition's validated connections into what runs on the version, at
// publish (phase 4.2). A failure refuses the publish.
type Resolver interface {
	Resolve(ctx context.Context, f *config.Factory) (Resolution, error)
}

// Resolution is a Resolver's result. Phase 4.2 adds the connection bindings, and the
// Resolved and Bindings fields of ports.Publish that store it in the publish transaction.
type Resolution struct {
	Resolved json.RawMessage
}
