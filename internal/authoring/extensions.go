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

// Credentials checks a definition's secret references (the credentials check). B ships the
// interim EnvReferences; phase 3.1 adds VaultCredentials over its vault and goald wires that
// instead. Uses are config.Factory.SecretUses(); literals among them are left to config's
// literal-secret warning.
type Credentials interface {
	Check(ctx context.Context, f domain.FactoryID, uses []config.SecretUse) ([]CredentialFinding, error)
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
	// CodeCredentialsUnavailable: a credential: reference (or a reference of another secrets
	// provider) this install cannot resolve yet.
	CodeCredentialsUnavailable = "credentials-unavailable"
)

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// EnvReferences is the credentials check until phase 3 (docs/specs/README.md, "Interim rule
// until phase 3"): model and repository access are platform secrets of the install, so every
// env:NAME reference must name a variable that is set and non-empty in goald's environment
// (resolved through Secrets, the value discarded) and is not one of goald's own settings
// (Reserved, such as GOAL_DATABASE_URL and GOAL_GATEWAY_KEY); a credential: reference is an
// error until credentials arrive with phase 3. There is nothing to grant before phase 3, so
// there are no grant, expiry or purpose checks.
type EnvReferences struct {
	Secrets ports.Secrets
	// Reserved are the variables definitions may not read: goald's own settings and the
	// platform's secrets (the model's access, keys that never reach a task).
	Reserved []string
	// RepositoryOnly are variables holding the install's repository token: a definition may use
	// them for runtime.repository and, in a connection, only in the headers of an https
	// connection to one of RepositoryHosts (host names, matched exactly). Phase 4's connectors
	// replace this with a credential per connection.
	RepositoryOnly  []string
	RepositoryHosts []string
}

var _ Credentials = EnvReferences{}

// foreignPrefixes start references of secrets providers this install does not have.
var foreignPrefixes = []string{"op://", "arn:"}

// Check implements Credentials. Each reference is resolved once, however many places use it.
func (e EnvReferences) Check(ctx context.Context, _ domain.FactoryID, uses []config.SecretUse) ([]CredentialFinding, error) {
	resolved := map[string]*CredentialFinding{}

	var out []CredentialFinding

	for _, u := range uses {
		if u.Literal() {
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

// misplaced refuses (env-reserved) a repository token used anywhere but the repository and the
// https headers of a connection to one of RepositoryHosts; nil when u may use its reference.
func (e EnvReferences) misplaced(u config.SecretUse) *CredentialFinding {
	name, ok := strings.CutPrefix(u.Ref, "env:")
	if !ok || !slices.Contains(e.RepositoryOnly, name) || u.Purpose == config.PurposeRepository {
		return nil
	}

	if target, err := url.Parse(u.URL); err == nil && target.Scheme == "https" && slices.Contains(e.RepositoryHosts, target.Hostname()) {
		return nil
	}

	return &CredentialFinding{
		Use: u, Code: CodeEnvReserved, Severity: domain.SeverityError,
		Message: fmt.Sprintf("env:%s is the repository's token: a connection may send it only in the headers of https://%s",
			name, strings.Join(e.RepositoryHosts, " or https://")),
	}
}

// check returns the finding of one reference, nil when it resolves.
func (e EnvReferences) check(ctx context.Context, ref string) (*CredentialFinding, error) {
	finding := func(code, format string, a ...any) *CredentialFinding {
		return &CredentialFinding{Code: code, Severity: domain.SeverityError, Message: fmt.Sprintf(format, a...)}
	}

	if name, ok := strings.CutPrefix(ref, config.CredentialPrefix); ok {
		return finding(CodeCredentialsUnavailable, "credential %q cannot be used yet: credentials arrive with phase 3", name), nil
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

	if slices.Contains(e.Reserved, name) {
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

// Check is a validation step another phase registers (phase 3's connector checker). Its
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
// publish (phase 3.2). A failure refuses the publish.
type Resolver interface {
	Resolve(ctx context.Context, f *config.Factory) (Resolution, error)
}

// Resolution is a Resolver's result. Phase 3.2 adds the connection bindings, and the
// Resolved and Bindings fields of ports.Publish that store it in the publish transaction.
type Resolution struct {
	Resolved json.RawMessage
}
