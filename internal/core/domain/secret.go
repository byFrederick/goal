package domain

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// SecretID is a secret's id (a UUID). A secret renamed from a credential keeps the credential's id.
type SecretID string

// SecretSource is where a secret's value comes from (ADR 0037 decision 2). A later source, such as
// a sign-in or a password manager reference, gets its own ADR.
type SecretSource string

// The secret sources.
const (
	// SecretSourceValue is a value pasted or imported, which the run gets as it is.
	SecretSourceValue SecretSource = "value"
)

// SecretSources are the sources there are.
var SecretSources = []SecretSource{SecretSourceValue}

// Valid reports whether s is one of SecretSources.
func (s SecretSource) Valid() bool { return slices.Contains(SecretSources, s) }

// The limits of a secret (ADR 0037 decision 1).
const (
	// MaxSecretValue is the most bytes a secret's value has.
	MaxSecretValue = 64 << 10
	// MaxSecretDescription is the most characters a secret's description has.
	MaxSecretDescription = 500
	// MaxSecretRemindDays is the most days before its expiry a secret's reminder is due.
	MaxSecretRemindDays = 90
)

// SecretScope is the factories whose runs may open a secret: all of them, ones created later
// included, or the listed ones. The zero scope allows none.
type SecretScope struct {
	All       bool
	Factories []FactoryID
}

// Allows reports whether a run of factory may open the secret.
func (s SecretScope) Allows(factory FactoryID) bool {
	return s.All || slices.Contains(s.Factories, factory)
}

// Secret is an organization's secret, the only place a value lives (ADR 0037 decision 1), as
// everyone who may see it sees it: its metadata, never its value.
type Secret struct {
	ID          SecretID
	Name        string
	Source      SecretSource
	Description string
	Scope       SecretScope
	// ExpiresAt is when the value expires, or nil; RemindDays, when positive, is how many days
	// before it a reminder is due, and needs ExpiresAt.
	ExpiresAt  *time.Time
	RemindDays int
	LastUsedAt *time.Time
	CreatedBy  Actor
	CreatedAt  time.Time
	UpdatedBy  Actor
	UpdatedAt  time.Time
}

// CheckSecret enforces a secret's metadata: a name of 3 to 40 lowercase letters, digits and inner
// dashes starting with a letter, a known source, a description of at most MaxSecretDescription
// characters, a reminder of 1 to MaxSecretRemindDays days only with an expiry, and a scope of all
// factories or a list of distinct ones. The error wraps ErrInvalid and lists every broken rule.
func CheckSecret(s Secret) error {
	var errs []error

	if err := CheckSecretName(s.Name); err != nil {
		errs = append(errs, err)
	}

	if !s.Source.Valid() {
		errs = append(errs, fmt.Errorf("secret source %q is not one of %v: %w", s.Source, SecretSources, ErrInvalid))
	}

	if utf8.RuneCountInString(s.Description) > MaxSecretDescription {
		errs = append(errs, fmt.Errorf("a description has at most %d characters: %w", MaxSecretDescription, ErrInvalid))
	}

	switch {
	case s.RemindDays < 0 || s.RemindDays > MaxSecretRemindDays:
		errs = append(errs, fmt.Errorf("a reminder is 1 to %d days before the expiry: %w", MaxSecretRemindDays, ErrInvalid))
	case s.RemindDays > 0 && s.ExpiresAt == nil:
		errs = append(errs, fmt.Errorf("a reminder needs an expiry: %w", ErrInvalid))
	}

	return errors.Join(append(errs, checkScope(s.Scope))...)
}

// checkScope refuses a scope of all factories and a list, and an empty or repeated factory id.
func checkScope(s SecretScope) error {
	var errs []error

	if s.All && len(s.Factories) > 0 {
		errs = append(errs, fmt.Errorf("a scope is all factories or a list of them, not both: %w", ErrInvalid))
	}

	seen := map[FactoryID]bool{}

	for _, f := range s.Factories {
		switch {
		case f == "":
			errs = append(errs, fmt.Errorf("a scope's factory id is empty: %w", ErrInvalid))
		case seen[f]:
			errs = append(errs, fmt.Errorf("factory %s is in the scope twice: %w", f, ErrInvalid))
		}

		seen[f] = true
	}

	return errors.Join(errs...)
}

// CheckSecretName refuses a name that is not a secret's. The error wraps ErrInvalid.
func CheckSecretName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("secret name %q must be 3 to 40 lowercase letters, digits or dashes, "+
			"starting with a letter and not ending with a dash: %w", name, ErrInvalid)
	}

	return nil
}

// checkSecretValue refuses an empty or oversized value; the error never shows it.
func checkSecretValue(v string) error {
	switch {
	case v == "":
		return fmt.Errorf("a secret needs a value: %w", ErrInvalid)
	case len(v) > MaxSecretValue:
		return fmt.Errorf("a secret's value has at most %d bytes: %w", MaxSecretValue, ErrInvalid)
	default:
		return nil
	}
}

// NewSecret is a secret to create, its value included. An empty Source is SecretSourceValue.
// String and GoString hide the value.
type NewSecret struct {
	Name        string
	Source      SecretSource
	Description string
	Scope       SecretScope
	ExpiresAt   *time.Time
	RemindDays  int
	Value       string
}

// Secret is n's metadata.
func (n NewSecret) Secret() Secret {
	source := n.Source
	if source == "" {
		source = SecretSourceValue
	}

	return Secret{
		Name: n.Name, Source: source, Description: n.Description, Scope: n.Scope, ExpiresAt: n.ExpiresAt, RemindDays: n.RemindDays,
	}
}

// Check is CheckSecret of n's metadata and a value that is set and not too long.
func (n NewSecret) Check() error {
	return errors.Join(CheckSecret(n.Secret()), checkSecretValue(n.Value))
}

// String shows n without its value.
func (n NewSecret) String() string {
	return fmt.Sprintf("NewSecret{Name:%s Value:%s}", n.Name, hiddenValue(n.Value))
}

// GoString is String, so %#v hides the value too.
func (n NewSecret) GoString() string { return n.String() }

// SecretExpiry is a secret's expiry and reminder; the zero value clears both.
type SecretExpiry struct {
	At         *time.Time
	RemindDays int
}

// SecretPatch changes a secret. Name, when set, renames it (its sealed value stays: it is bound to
// its id); Description, Scope and Expiry, when set, replace theirs; Replace, when set, replaces
// the value, the only write of a value. String and GoString hide it.
type SecretPatch struct {
	Name        *string
	Description *string
	Scope       *SecretScope
	Expiry      *SecretExpiry
	Replace     *string
}

// Empty reports whether p changes nothing.
func (p SecretPatch) Empty() bool { return !p.ChangesMetadata() && p.Replace == nil }

// ChangesMetadata reports whether p changes anything but the value.
func (p SecretPatch) ChangesMetadata() bool {
	return p.Name != nil || p.Description != nil || p.Scope != nil || p.Expiry != nil
}

// Apply returns s with p's metadata changes; the caller checks the result with CheckSecret.
func (p SecretPatch) Apply(s Secret) Secret {
	s.Scope.Factories = slices.Clone(s.Scope.Factories)

	if p.Name != nil {
		s.Name = *p.Name
	}

	if p.Description != nil {
		s.Description = *p.Description
	}

	if p.Scope != nil {
		s.Scope = SecretScope{All: p.Scope.All, Factories: slices.Clone(p.Scope.Factories)}
	}

	if p.Expiry != nil {
		s.ExpiresAt, s.RemindDays = p.Expiry.At, p.Expiry.RemindDays
	}

	return s
}

// CheckValue refuses a Replace that is empty or too long.
func (p SecretPatch) CheckValue() error {
	if p.Replace == nil {
		return nil
	}

	return checkSecretValue(*p.Replace)
}

// String shows which parts p changes, never the value.
func (p SecretPatch) String() string {
	var parts []string

	if p.Name != nil {
		parts = append(parts, "name:"+*p.Name)
	}

	for part, set := range map[string]bool{
		"description": p.Description != nil, "scope": p.Scope != nil, "expiry": p.Expiry != nil, "replace": p.Replace != nil,
	} {
		if set {
			parts = append(parts, part)
		}
	}

	slices.Sort(parts)

	return "SecretPatch{" + strings.Join(parts, " ") + "}"
}

// GoString is String, so %#v hides the value too.
func (p SecretPatch) GoString() string { return p.String() }

// SecretUse is what secrets are opened for: one run of factory, for agent. Its secret.used audit
// rows record it.
type SecretUse struct {
	Factory FactoryID
	Agent   string
	Run     RunID
}

// AuditData is what the used row of secret name records: never its value.
func (u SecretUse) AuditData(name string) map[string]any {
	out := map[string]any{"name": name}

	for key, v := range map[string]string{"factory_id": string(u.Factory), "agent": u.Agent, "run_id": string(u.Run)} {
		if v != "" {
			out[key] = v
		}
	}

	return out
}

// ResolvedSecret is a secret opened for one run: its metadata and its value. Only
// ports.OrgSecrets.Resolve returns one. String and GoString hide the value.
type ResolvedSecret struct {
	Secret Secret
	Value  string
}

// String shows r without its value.
func (r ResolvedSecret) String() string {
	return fmt.Sprintf("ResolvedSecret{Name:%s Value:%s}", r.Secret.Name, hiddenValue(r.Value))
}

// GoString is String, so %#v hides the value too.
func (r ResolvedSecret) GoString() string { return r.String() }

// SecretAAD is the associated data a secret's sealed value is bound to (ADR 0037 decision 1): its
// organization, its id and the key id, so a ciphertext copied to another row, organization or key
// id does not open. They are CredentialAAD's bytes, "goal/credential/v1" included: the secrets are
// 00065's credentials renamed with their ids, sealed before the rename.
func SecretAAD(org OrgID, id SecretID, keyID string) []byte {
	return []byte("goal/credential/v1|" + string(org) + "|" + string(id) + "|" + keyID)
}

// Audit actions of secrets; no entry holds a value.
const (
	AuditSecretCreated       = "secret.created"
	AuditSecretUpdated       = "secret.updated"
	AuditSecretValueReplaced = "secret.value_replaced"
	AuditSecretDeleted       = "secret.deleted"
	AuditSecretUsed          = "secret.used"
)

// AuditTargetSecret is the audit target kind of secrets.
const AuditTargetSecret = "secret"
