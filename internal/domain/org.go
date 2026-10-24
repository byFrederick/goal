package domain

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Errors of the organization rules.
var (
	// ErrInvalid means a rule was broken: a taken or reserved slug, a malformed name, a
	// confirmation that does not match. The API answers 409.
	ErrInvalid = errors.New("invalid")
	// ErrNoOrg means a call that acts on an organization's rows got a context without one.
	ErrNoOrg = errors.New("no organization in context")
)

// OrgID is an organization's id (a UUID), the tenant boundary of every org-owned row.
type OrgID string

// Org is one organization of the install.
type Org struct {
	ID         OrgID
	Slug, Name string
	// Mark is the one or two characters the org rail shows.
	Mark      string
	CreatedAt time.Time
}

// OrgSummary is an organization as the organization list shows it.
type OrgSummary struct {
	Org

	FactoryCount int
}

// ActorKind is who did something: a person, an agent or the system.
type ActorKind string

// Actor kinds.
const (
	ActorUser   ActorKind = "user"
	ActorAgent  ActorKind = "agent"
	ActorSystem ActorKind = "system"
)

// Actor is who did something. ID is "local" for the local user, an agent's name or a job's name.
type Actor struct {
	Kind ActorKind
	ID   string
	Name string
}

// LocalUser is the only person in phase 2.2, which has no sign-in: every request acts as it.
// Phase 4 adds real users; refs recorded before then stay readable.
var LocalUser = Actor{Kind: ActorUser, ID: "local", Name: "Local user"}

// Ref is how other tables record an actor (`*_by text` columns): "user:local",
// "agent:investigator", "system:engine".
func (a Actor) Ref() string { return string(a.Kind) + ":" + a.ID }

// ActorFromRef parses a Ref back into an actor and fills its name.
func ActorFromRef(ref string) (Actor, error) {
	kind, id, ok := strings.Cut(ref, ":")
	if !ok || id == "" {
		return Actor{}, fmt.Errorf("actor ref %q is not <kind>:<id>", ref)
	}

	switch ActorKind(kind) {
	case ActorUser:
		if id == LocalUser.ID {
			return LocalUser, nil
		}

		return Actor{Kind: ActorUser, ID: id, Name: id}, nil
	case ActorAgent, ActorSystem:
		return Actor{Kind: ActorKind(kind), ID: id, Name: id}, nil
	default:
		return Actor{}, fmt.Errorf("actor ref %q: unknown kind %q", ref, kind)
	}
}

// AuditEntry is one line of an organization's append-only audit log.
type AuditEntry struct {
	ID    int64 // assigned by the store
	At    time.Time
	Actor Actor
	// Action is <noun>.<past_verb>, such as org.created.
	Action               string
	TargetKind, TargetID string
	// FactoryID is set when the entry concerns one factory.
	FactoryID FactoryID
	// Data never holds secrets, tokens or credential values.
	Data map[string]any
}

// Audit actions of organizations.
const (
	AuditOrgCreated = "org.created"
	AuditOrgUpdated = "org.updated"
	AuditOrgDeleted = "org.deleted"
)

// AuditTargetOrg is the target kind of entries about an organization.
const AuditTargetOrg = "org"

var (
	slugPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$`)
	actionPattern = regexp.MustCompile(`^[a-z_]+\.[a-z_]+$`)
)

// reservedSlugs are words of Goal's own routes; me, auth and invite stay reserved for phase 4,
// and default names the organization existing data moved into.
var reservedSlugs = []string{"new", "settings", "api", "v1", "o", "me", "orgs", "auth", "invite", "admin", "default"}

// CheckSlug enforces the organization address rule: 3 to 40 lowercase letters, digits and
// inner dashes, and not a reserved word. The error wraps ErrInvalid.
func CheckSlug(slug string) error {
	if !slugPattern.MatchString(slug) {
		return fmt.Errorf("slug %q must be 3 to 40 lowercase letters, digits or dashes, not starting or ending with a dash: %w",
			slug, ErrInvalid)
	}

	if slices.Contains(reservedSlugs, slug) {
		return fmt.Errorf("slug %q is reserved: %w", slug, ErrInvalid)
	}

	return nil
}

// CheckOrgName enforces 1 to 80 characters. The error wraps ErrInvalid.
func CheckOrgName(name string) error {
	if n := utf8.RuneCountInString(name); n < 1 || n > 80 || strings.TrimSpace(name) == "" {
		return fmt.Errorf("an organization name has 1 to 80 characters: %w", ErrInvalid)
	}

	return nil
}

// CheckMark enforces 1 or 2 characters. The error wraps ErrInvalid.
func CheckMark(mark string) error {
	if n := utf8.RuneCountInString(mark); n < 1 || n > 2 || strings.TrimSpace(mark) == "" {
		return fmt.Errorf("an organization mark has 1 or 2 characters: %w", ErrInvalid)
	}

	return nil
}

// CheckAuditAction enforces <noun>.<past_verb>. The error wraps ErrInvalid.
func CheckAuditAction(action string) error {
	if !actionPattern.MatchString(action) {
		return fmt.Errorf("audit action %q is not <noun>.<past_verb>: %w", action, ErrInvalid)
	}

	return nil
}

// DefaultMark is the mark of an organization created without one: the initials of its first
// two words ("Example Labs" → "EL"), or the first two letters of a one-word name ("Acme" → "Ac").
func DefaultMark(name string) string {
	words := strings.Fields(name)

	switch len(words) {
	case 0:
		return ""
	case 1:
		r := []rune(words[0])
		mark := string(unicode.ToUpper(r[0]))

		if len(r) > 1 {
			mark += string(unicode.ToLower(r[1]))
		}

		return mark
	default:
		first, _ := utf8.DecodeRuneInString(words[0])
		second, _ := utf8.DecodeRuneInString(words[1])

		return string([]rune{unicode.ToUpper(first), unicode.ToUpper(second)})
	}
}

type (
	orgKey   struct{}
	actorKey struct{}
)

// WithOrg returns ctx bound to an organization. The API sets it from the URL once the org
// middleware resolved it, the engine from the line, the gateway from the task token; every
// method that touches org-owned rows reads it.
func WithOrg(ctx context.Context, id OrgID) context.Context {
	return context.WithValue(ctx, orgKey{}, id)
}

// OrgFrom returns the organization ctx is bound to; an empty id counts as none.
func OrgFrom(ctx context.Context) (OrgID, bool) {
	id, _ := ctx.Value(orgKey{}).(OrgID)

	return id, id != ""
}

// WithActor returns ctx carrying who acts: the API sets the request's actor (always LocalUser
// in 2.2), the engine and jobs their own.
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

// ActorFrom returns the actor ctx carries.
func ActorFrom(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(actorKey{}).(Actor)

	return a, ok
}
