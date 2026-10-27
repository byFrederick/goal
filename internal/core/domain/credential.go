package domain

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// CredentialID is a credential's id (a UUID).
type CredentialID string

// CredentialType labels what a credential is (ADR 0017 decision 2). Every type reaches a run the
// same way, as one env value.
type CredentialType string

// The credential types.
const (
	CredentialAPIKey     CredentialType = "api_key"
	CredentialOAuthToken CredentialType = "oauth_token"
	CredentialSSHKey     CredentialType = "ssh_key"
	CredentialCloudRole  CredentialType = "cloud_role"
	CredentialAppKey     CredentialType = "app_key"
)

// CredentialTypes are the types, in the order the Credentials screen's tabs show them.
var CredentialTypes = []CredentialType{CredentialAPIKey, CredentialOAuthToken, CredentialSSHKey, CredentialCloudRole, CredentialAppKey}

// Valid reports whether t is one of CredentialTypes.
func (t CredentialType) Valid() bool { return slices.Contains(CredentialTypes, t) }

// MaxCredentialValue is the most bytes a credential's value has.
const MaxCredentialValue = 64 << 10

// CredentialScope is the factories whose runs may open a credential (ADR 0017 decision 4): all
// of them, ones created later included, or the listed ones. The zero scope allows none.
type CredentialScope struct {
	All       bool
	Factories []FactoryID
}

// Allows reports whether a run of factory may open the credential.
func (s CredentialScope) Allows(factory FactoryID) bool {
	return s.All || slices.Contains(s.Factories, factory)
}

// Credential is an organization's credential as everyone who may see it sees it: its metadata,
// never its value (ADR 0017 decisions 1 and 3).
type Credential struct {
	ID          CredentialID
	Name        string
	Type        CredentialType
	Description string
	Scope       CredentialScope
	// Agents are, per factory id, the agents that get the credential in their run's box as env
	// CredentialEnvName(Name) (decision 6). A name the factory's definition dropped matches nothing.
	Agents map[FactoryID][]string
	// ExpiresAt is when the value expires, or nil; RemindDays, when positive, is how many days
	// before it a rotation reminder is due, and needs ExpiresAt.
	ExpiresAt  *time.Time
	RemindDays int
	LastUsedAt *time.Time
	CreatedBy  Actor
	CreatedAt  time.Time
	UpdatedBy  Actor
	UpdatedAt  time.Time
}

// ForAgent reports whether agent of factory gets the credential directly: factory is in its
// scope and agent on its agent list for factory.
func (c Credential) ForAgent(factory FactoryID, agent string) bool {
	return c.Scope.Allows(factory) && slices.Contains(c.Agents[factory], agent)
}

// CheckCredential enforces a credential's metadata: a connection name whose env variable is not
// reserved, a known type, a connection description, a reminder only with an expiry, a scope of
// all factories or a list of distinct ones, and agents only for factories in the scope. The error
// wraps ErrInvalid and lists every broken rule.
func CheckCredential(c Credential) error {
	errs := []error{CheckConnectionDescription(c.Description)}

	if _, err := CredentialEnvName(c.Name); err != nil {
		errs = append(errs, err)
	}

	if !c.Type.Valid() {
		errs = append(errs, fmt.Errorf("credential type %q is not one of %v: %w", c.Type, CredentialTypes, ErrInvalid))
	}

	switch {
	case c.RemindDays < 0:
		errs = append(errs, fmt.Errorf("a rotation reminder is a positive number of days: %w", ErrInvalid))
	case c.RemindDays > 0 && c.ExpiresAt == nil:
		errs = append(errs, fmt.Errorf("a rotation reminder needs an expiry: %w", ErrInvalid))
	}

	if c.Scope.All && len(c.Scope.Factories) > 0 {
		errs = append(errs, fmt.Errorf("a scope is all factories or a list of them, not both: %w", ErrInvalid))
	}

	seen := map[FactoryID]bool{}
	for _, f := range c.Scope.Factories {
		switch {
		case f == "":
			errs = append(errs, fmt.Errorf("a scope's factory id is empty: %w", ErrInvalid))
		case seen[f]:
			errs = append(errs, fmt.Errorf("factory %s is in the scope twice: %w", f, ErrInvalid))
		}

		seen[f] = true
	}

	for _, f := range slices.Sorted(maps.Keys(c.Agents)) {
		if !c.Scope.All && !seen[f] {
			errs = append(errs, fmt.Errorf("agents of factory %q, which is not in the scope: %w", f, ErrInvalid))
		}

		if slices.Contains(c.Agents[f], "") {
			errs = append(errs, fmt.Errorf("an agent name of factory %s is empty: %w", f, ErrInvalid))
		}
	}

	return errors.Join(errs...)
}

// checkCredentialValue refuses an empty or oversized value; the error never shows it.
func checkCredentialValue(v string) error {
	switch {
	case v == "":
		return fmt.Errorf("a credential needs a value: %w", ErrInvalid)
	case len(v) > MaxCredentialValue:
		return fmt.Errorf("a credential's value has at most %d bytes: %w", MaxCredentialValue, ErrInvalid)
	default:
		return nil
	}
}

// NewCredential is a credential to create, its value included. String and GoString hide the value.
type NewCredential struct {
	Name        string
	Type        CredentialType
	Description string
	Scope       CredentialScope
	Agents      map[FactoryID][]string
	ExpiresAt   *time.Time
	RemindDays  int
	Value       string
}

// Credential is n's metadata.
func (n NewCredential) Credential() Credential {
	return Credential{
		Name: n.Name, Type: n.Type, Description: n.Description, Scope: n.Scope, Agents: n.Agents,
		ExpiresAt: n.ExpiresAt, RemindDays: n.RemindDays,
	}
}

// Check is CheckCredential of n's metadata and a value that is set and not too long.
func (n NewCredential) Check() error {
	return errors.Join(CheckCredential(n.Credential()), checkCredentialValue(n.Value))
}

// String shows n without its value.
func (n NewCredential) String() string {
	return fmt.Sprintf("NewCredential{Name:%s Type:%s Value:%s}", n.Name, n.Type, hiddenValue(n.Value))
}

// GoString is String, so %#v hides the value too.
func (n NewCredential) GoString() string { return n.String() }

// CredentialExpiry is a credential's expiry and rotation reminder; the zero value clears both.
type CredentialExpiry struct {
	At         *time.Time
	RemindDays int
}

// CredentialPatch changes a credential. Name, when set, renames it (its sealed value stays: it is
// bound to its id); Type, Description, Scope and Expiry, when set, replace theirs; Agents, when not
// nil, replaces the agent list; Replace, when set, replaces the value. String and GoString hide it.
type CredentialPatch struct {
	Name        *string
	Type        *CredentialType
	Description *string
	Scope       *CredentialScope
	Agents      map[FactoryID][]string
	Expiry      *CredentialExpiry
	Replace     *string
}

// Empty reports whether p changes nothing.
func (p CredentialPatch) Empty() bool {
	return !p.ChangesMetadata() && p.Replace == nil
}

// ChangesMetadata reports whether p changes anything but the value.
func (p CredentialPatch) ChangesMetadata() bool {
	return p.Name != nil || p.Type != nil || p.Description != nil || p.Scope != nil || p.Agents != nil || p.Expiry != nil
}

// Apply returns c with p's metadata changes; the caller checks the result with CheckCredential.
func (p CredentialPatch) Apply(c Credential) Credential {
	if p.Name != nil {
		c.Name = *p.Name
	}

	if p.Type != nil {
		c.Type = *p.Type
	}

	if p.Description != nil {
		c.Description = *p.Description
	}

	if p.Scope != nil {
		c.Scope = CredentialScope{All: p.Scope.All, Factories: slices.Clone(p.Scope.Factories)}
	}

	if p.Agents != nil {
		c.Agents = maps.Clone(p.Agents)
	}

	if p.Expiry != nil {
		c.ExpiresAt, c.RemindDays = p.Expiry.At, p.Expiry.RemindDays
	}

	return c
}

// CheckValue refuses a Replace that is empty or too long.
func (p CredentialPatch) CheckValue() error {
	if p.Replace == nil {
		return nil
	}

	return checkCredentialValue(*p.Replace)
}

// String shows which parts p changes, never the value.
func (p CredentialPatch) String() string {
	var parts []string

	if p.Name != nil {
		parts = append(parts, "name:"+*p.Name)
	}

	for part, set := range map[string]bool{
		"type": p.Type != nil, "description": p.Description != nil, "scope": p.Scope != nil,
		"agents": p.Agents != nil, "expiry": p.Expiry != nil, "replace": p.Replace != nil,
	} {
		if set {
			parts = append(parts, part)
		}
	}

	slices.Sort(parts)

	return "CredentialPatch{" + strings.Join(parts, " ") + "}"
}

// GoString is String, so %#v hides the value too.
func (p CredentialPatch) GoString() string { return p.String() }

// CredentialUse is what credentials are opened for: one run of factory, for agent. Its
// credential.used audit rows record it.
type CredentialUse struct {
	Factory FactoryID
	Agent   string
	Run     RunID
}

// AuditData is what the credential.used row of credential name records: never its value.
func (u CredentialUse) AuditData(name string) map[string]any {
	out := map[string]any{"name": name}

	for key, v := range map[string]string{"factory_id": string(u.Factory), "agent": u.Agent, "run_id": string(u.Run)} {
		if v != "" {
			out[key] = v
		}
	}

	return out
}

// ResolvedCredential is a credential opened for one run: its metadata and its value. Only
// ports.Credentials.Resolve returns one. String and GoString hide the value.
type ResolvedCredential struct {
	Credential Credential
	Value      string
}

// String shows r without its value.
func (r ResolvedCredential) String() string {
	return fmt.Sprintf("ResolvedCredential{Name:%s Value:%s}", r.Credential.Name, hiddenValue(r.Value))
}

// GoString is String, so %#v hides the value too.
func (r ResolvedCredential) GoString() string { return r.String() }

// hiddenValue is what a String shows for a value.
func hiddenValue(v string) string {
	if v == "" {
		return "<none>"
	}

	return "<hidden>"
}

// CredentialAAD is the associated data a credential's sealed value is bound to (ADR 0017 decision
// 3): its organization, its id and the key id, so a ciphertext copied to another row, organization
// or key id does not open.
func CredentialAAD(org OrgID, id CredentialID, keyID string) []byte {
	return []byte("goal/credential/v1|" + string(org) + "|" + string(id) + "|" + keyID)
}

// CredentialEnvName is the env variable an agent gets credential name as (ADR 0017 decision 6):
// the name upper-cased with each - turned into _. It refuses a name that is not a connection name
// or whose variable is ReservedRunEnv; the error wraps ErrInvalid.
func CredentialEnvName(name string) (string, error) {
	if err := CheckConnectionName(name); err != nil {
		return "", err
	}

	env := strings.ReplaceAll(strings.ToUpper(name), "-", "_")
	if ReservedRunEnv(env) {
		return "", fmt.Errorf("credential %q: its env variable %s is reserved for the platform: %w", name, env, ErrInvalid)
	}

	return env, nil
}

// credentialRef is a whole {{name}} reference; anyRef is one anywhere in a value.
var (
	credentialRef = regexp.MustCompile(`^\{\{([^{}]*)\}\}$`)
	anyRef        = regexp.MustCompile(`\{\{[^{}]*\}\}`)
)

// CredentialRef reports whether a connection's secret value is a reference to a credential, and
// to which (ADR 0017 decision 5): the whole value is {{name}}. A value with no {{…}} is no
// reference; one that holds {{…}} anywhere else, or names no valid credential, is refused. The
// error wraps ErrInvalid and never shows the value.
func CredentialRef(value string) (string, bool, error) {
	if m := credentialRef.FindStringSubmatch(value); m != nil {
		if namePattern.MatchString(m[1]) {
			return m[1], true, nil
		}

		return "", false, fmt.Errorf("a credential reference {{name}} names a credential: 3 to 40 lowercase letters, digits or dashes: %w", ErrInvalid)
	}

	if anyRef.MatchString(value) {
		return "", false, fmt.Errorf("a secret value refers to a credential only as its whole value, {{name}}: %w", ErrInvalid)
	}

	return "", false, nil
}

// The import formats of ParseCredentialImport.
const (
	CredentialImportEnv  = "env"
	CredentialImportYAML = "yaml"
)

// CredentialImportEntry is one credential of an import: its name and value. String and GoString
// hide the value.
type CredentialImportEntry struct {
	Name  string
	Value string
}

// String shows e without its value.
func (e CredentialImportEntry) String() string {
	return fmt.Sprintf("CredentialImportEntry{Name:%s Value:%s}", e.Name, hiddenValue(e.Value))
}

// GoString is String, so %#v hides the value too.
func (e CredentialImportEntry) GoString() string { return e.String() }

// CredentialImportPreview is what importing credential Name would do (ADR 0017 decision 9): create
// it, or, when Exists, replace or skip one that Connections connections use.
type CredentialImportPreview struct {
	Name        string
	Exists      bool
	Connections int
}

// ParseCredentialImport reads the credentials of an import in format (ADR 0017 decision 9), in the
// order they come:
//
//   - env: KEY=value lines; blank lines and # comments are skipped, one pair of surrounding quotes
//     is stripped, and a key becomes a name lower-cased with each _ turned into - (SENTRY_TOKEN is
//     sentry-token).
//   - yaml: a top-level credentials: key, then one indented name: value line per credential, the
//     value plain, 'single-' or "double-quoted". Comments and a leading --- are allowed; anything
//     else (block scalars, nested maps, lists, anchors, aliases, tags, flow collections, another
//     top-level key) is refused. It is a subset of YAML, read without a YAML library.
//
// Every name must be a credential name (CredentialEnvName), and none may come twice. The error
// wraps ErrInvalid, names lines and names, and never shows a value.
func ParseCredentialImport(format, text string) ([]CredentialImportEntry, error) {
	var parse func(lines []string) ([]CredentialImportEntry, error)

	switch format {
	case CredentialImportEnv:
		parse = parseEnvImport
	case CredentialImportYAML:
		parse = parseYAMLImport
	default:
		return nil, fmt.Errorf("import format %q is neither %s nor %s: %w", format, CredentialImportEnv, CredentialImportYAML, ErrInvalid)
	}

	lines := strings.Split(strings.TrimPrefix(text, "\ufeff"), "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}

	entries, err := parse(lines)
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	for _, e := range entries {
		if seen[e.Name] {
			return nil, fmt.Errorf("credential %q is in the import twice: %w", e.Name, ErrInvalid)
		}

		seen[e.Name] = true
	}

	return entries, nil
}

func parseEnvImport(lines []string) ([]CredentialImportEntry, error) {
	var out []CredentialImportEntry

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("line %d is not KEY=value: %w", i+1, ErrInvalid)
		}

		key = strings.TrimSpace(key)
		if !envNamePattern.MatchString(key) {
			return nil, fmt.Errorf("line %d: the key is not an env variable name: %w", i+1, ErrInvalid)
		}

		name := strings.ReplaceAll(strings.ToLower(key), "_", "-")
		if _, err := CredentialEnvName(name); err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}

		value, err := unquoteEnv(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("line %d (%s): %w", i+1, key, err)
		}

		if err := checkCredentialValue(value); err != nil {
			return nil, fmt.Errorf("line %d (%s): %w", i+1, key, err)
		}

		out = append(out, CredentialImportEntry{Name: name, Value: value})
	}

	return out, nil
}

// unquoteEnv strips one pair of matching surrounding quotes from v; an opening quote with no
// closing one is refused.
func unquoteEnv(v string) (string, error) {
	if v == "" || (v[0] != '"' && v[0] != '\'') {
		return v, nil
	}

	if len(v) < 2 || v[len(v)-1] != v[0] {
		return "", fmt.Errorf("a quoted value is not closed: %w", ErrInvalid)
	}

	return v[1 : len(v)-1], nil
}

func parseYAMLImport(lines []string) ([]CredentialImportEntry, error) {
	var (
		out    []CredentialImportEntry
		inside bool
		indent string
	)

	for i, line := range lines {
		n := i + 1
		trimmed := strings.TrimSpace(line)

		switch {
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
			continue
		case trimmed == "---" && !inside && len(out) == 0:
			continue
		}

		lead := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		if strings.Contains(lead, "\t") {
			return nil, fmt.Errorf("line %d is indented with a tab: %w", n, ErrInvalid)
		}

		if lead == "" {
			if inside || stripYAMLComment(trimmed) != "credentials:" {
				return nil, fmt.Errorf("line %d: an import has one top-level key, credentials:, holding name: value lines: %w", n, ErrInvalid)
			}

			inside = true

			continue
		}

		switch {
		case !inside:
			return nil, fmt.Errorf("line %d comes before credentials: %w", n, ErrInvalid)
		case indent == "":
			indent = lead
		case lead != indent:
			return nil, fmt.Errorf("line %d is not indented as the lines before it; nested maps and multi-line values are not supported: %w", n, ErrInvalid)
		}

		name, rest, ok := strings.Cut(trimmed, ":")
		if !ok || strings.HasPrefix(trimmed, "- ") || trimmed == "-" {
			return nil, fmt.Errorf("line %d is not name: value: %w", n, ErrInvalid)
		}

		if !namePattern.MatchString(name) {
			return nil, fmt.Errorf("line %d: the key is not a credential name: 3 to 40 lowercase letters, digits or dashes: %w", n, ErrInvalid)
		}

		if _, err := CredentialEnvName(name); err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}

		if rest != "" && rest[0] != ' ' {
			return nil, fmt.Errorf("line %d (%s): a value follows \": \": %w", n, name, ErrInvalid)
		}

		value, err := yamlScalar(strings.TrimSpace(rest))
		if err != nil {
			return nil, fmt.Errorf("line %d (%s): %w", n, name, err)
		}

		if err := checkCredentialValue(value); err != nil {
			return nil, fmt.Errorf("line %d (%s): %w", n, name, err)
		}

		out = append(out, CredentialImportEntry{Name: name, Value: value})
	}

	if !inside {
		return nil, fmt.Errorf("an import has a top-level credentials: key: %w", ErrInvalid)
	}

	return out, nil
}

// stripYAMLComment cuts a " #" comment off the end of an unquoted line.
func stripYAMLComment(s string) string {
	if strings.HasPrefix(s, "#") {
		return ""
	}

	if i := strings.Index(s, " #"); i >= 0 {
		s = s[:i]
	}

	return strings.TrimSpace(s)
}

// yamlScalar reads one single-line value of the subset: plain, 'single-' or "double-quoted".
// Errors never show the value.
func yamlScalar(v string) (string, error) {
	if v == "" || v[0] == '#' {
		return "", fmt.Errorf("no value on the line; nested maps and block scalars are not supported: %w", ErrInvalid)
	}

	switch v[0] {
	case '"':
		end := closingDoubleQuote(v)
		if end < 0 || stripYAMLComment(" "+v[end+1:]) != "" {
			return "", fmt.Errorf("a double-quoted value is not closed at the end of its line: %w", ErrInvalid)
		}

		s, err := strconv.Unquote(v[:end+1])
		if err != nil {
			return "", fmt.Errorf("a double-quoted value has an escape that is not supported: %w", ErrInvalid)
		}

		return s, nil
	case '\'':
		body, rest, ok := singleQuoted(v[1:])
		if !ok || stripYAMLComment(" "+rest) != "" {
			return "", fmt.Errorf("a single-quoted value is not closed at the end of its line: %w", ErrInvalid)
		}

		return body, nil
	case '|', '>':
		return "", fmt.Errorf("block scalars (| and >) are not supported: %w", ErrInvalid)
	case '&', '*', '!':
		return "", fmt.Errorf("anchors, aliases and tags are not supported: %w", ErrInvalid)
	case '{', '[':
		return "", fmt.Errorf("flow maps and lists are not supported: %w", ErrInvalid)
	case '-', '?', ':':
		if len(v) == 1 || v[1] == ' ' {
			return "", fmt.Errorf("lists and complex keys are not supported: %w", ErrInvalid)
		}
	case '%', '@', '`', ',':
		return "", fmt.Errorf("a plain value does not start with %c; quote it: %w", v[0], ErrInvalid)
	}

	s := stripYAMLComment(v)
	switch {
	case s == "~" || s == "null" || s == "Null" || s == "NULL":
		return "", fmt.Errorf("a null is no value: %w", ErrInvalid)
	case strings.Contains(s, ": ") || strings.HasSuffix(s, ":"):
		return "", fmt.Errorf("a plain value holds no \": \"; quote it: %w", ErrInvalid)
	}

	return s, nil
}

// closingDoubleQuote is the index of the quote that closes the double-quoted v, or -1.
func closingDoubleQuote(v string) int {
	for i := 1; i < len(v); i++ {
		switch v[i] {
		case '\\':
			i++
		case '"':
			return i
		}
	}

	return -1
}

// singleQuoted reads a single-quoted body after its opening quote, where '' is one quote, and
// returns it with what follows the closing quote.
func singleQuoted(v string) (body, rest string, ok bool) {
	var b strings.Builder

	for i := 0; i < len(v); i++ {
		if v[i] != '\'' {
			b.WriteByte(v[i])

			continue
		}

		if i+1 < len(v) && v[i+1] == '\'' {
			b.WriteByte('\'')
			i++

			continue
		}

		return b.String(), v[i+1:], true
	}

	return "", "", false
}

// Audit actions of credentials (ADR 0017 decision 7); no entry holds a value.
const (
	AuditCredentialCreated       = "credential.created"
	AuditCredentialUpdated       = "credential.updated"
	AuditCredentialValueReplaced = "credential.value_replaced"
	AuditCredentialDeleted       = "credential.deleted"
	AuditCredentialUsed          = "credential.used"
)

// AuditTargetCredential is the audit target kind of credentials.
const AuditTargetCredential = "credential"
