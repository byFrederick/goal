package domain

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// ConnectionID is a connection's id (a UUID).
type ConnectionID string

// MaxConnectionDescription is the most characters a description has (00051's checks).
const MaxConnectionDescription = 500

var (
	namePattern    = regexp.MustCompile(`^[a-z][a-z0-9-]{1,38}[a-z0-9]$`)
	envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// CheckConnectionName enforces a name as connections had it, which credentials, intake sources,
// triggers and standing rules keep: 3 to 40 lowercase letters, digits and inner dashes, starting
// with a letter. The error wraps ErrInvalid.
func CheckConnectionName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("name %q must be 3 to 40 lowercase letters, digits or dashes, starting with a letter and not ending with a dash: %w",
			name, ErrInvalid)
	}

	return nil
}

// CheckConnectionDescription enforces at most MaxConnectionDescription characters. The error
// wraps ErrInvalid.
func CheckConnectionDescription(d string) error {
	if utf8.RuneCountInString(d) > MaxConnectionDescription {
		return fmt.Errorf("a description has at most %d characters: %w", MaxConnectionDescription, ErrInvalid)
	}

	return nil
}

// reservedRunEnv are the variables a box's env keeps for the platform; reservedRunEnvPrefixes
// the prefixes.
var (
	reservedRunEnv = []string{
		"HOME", "PATH", "USER", "SHELL", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "NODE_USE_ENV_PROXY", "CLAUDE_CONFIG_DIR",
	}
	reservedRunEnvPrefixes = []string{"GOAL_", "LD_", "MISE_"}
)

// ReservedRunEnv reports whether nothing an organization sets may set the env variable name in a run's box:
// GOAL_*, HOME, PATH, USER, SHELL, the proxy variables, NODE_USE_ENV_PROXY, CLAUDE_CONFIG_DIR,
// LD_* and MISE_*, in any case.
func ReservedRunEnv(name string) bool {
	upper := strings.ToUpper(name)

	return slices.Contains(reservedRunEnv, upper) ||
		slices.ContainsFunc(reservedRunEnvPrefixes, func(p string) bool { return strings.HasPrefix(upper, p) })
}

// checkEnvName refuses an env variable a run's box may not be given.
func checkEnvName(name string) error {
	switch {
	case !envNamePattern.MatchString(name):
		return fmt.Errorf("env %q is not a variable name: %w", name, ErrInvalid)
	case ReservedRunEnv(name):
		return fmt.Errorf("env %q is reserved for the platform: %w", name, ErrInvalid)
	default:
		return nil
	}
}

// EngineActor is who acts for a run or a knowledge source's sync in the audit log.
var EngineActor = Actor{Kind: ActorSystem, ID: "engine", Name: "engine"}

// ConnectionAAD is the associated data a connection's sealed secrets are bound to (spec 4 §4.3):
// its organization, its id and the key id, so a ciphertext copied to another row, organization or
// key id does not open.
func ConnectionAAD(org OrgID, id ConnectionID, keyID string) []byte {
	return []byte("goal/connection/v1|" + string(org) + "|" + string(id) + "|" + keyID)
}

// Audit actions of connections (spec 4 §12), which the audit log still holds.
const (
	AuditConnectionCreated        = "connection.created"
	AuditConnectionUpdated        = "connection.updated"
	AuditConnectionSecretReplaced = "connection.secret_replaced"
	AuditConnectionDeleted        = "connection.deleted"
	AuditConnectionTested         = "connection.tested"
	AuditConnectionUsed           = "connection.used"
)
