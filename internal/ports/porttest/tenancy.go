package porttest

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

var (
	_ ports.Tenancy = (*Tenancy)(nil)
	_ ports.Audit   = (*Tenancy)(nil)
)

// Tenancy keeps organizations and their audit logs in memory behind one mutex, with the
// database's rules: unique slugs until purge, reserved slugs, soft delete, row-level isolation
// of the audit log by the org in ctx.
type Tenancy struct {
	mu    sync.Mutex
	now   func() time.Time
	orgs  map[domain.OrgID]*orgRow
	audit []auditRow
	ids   int
	// auditIDs numbers entries like an identity column: never reused, even after a purge.
	auditIDs int64
}

type orgRow struct {
	org     domain.Org
	deleted time.Time // zero while live
}

type auditRow struct {
	org   domain.OrgID
	entry domain.AuditEntry
}

// NewTenancy returns a tenancy with no organizations; now stamps creations and appends.
func NewTenancy(now func() time.Time) *Tenancy {
	return &Tenancy{now: now, orgs: map[domain.OrgID]*orgRow{}}
}

// Orgs implements ports.Tenancy.
func (t *Tenancy) Orgs(context.Context) ([]domain.OrgSummary, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var out []domain.OrgSummary

	for _, r := range t.orgs {
		if r.deleted.IsZero() {
			out = append(out, domain.OrgSummary{Org: r.org})
		}
	}

	slices.SortFunc(out, func(a, b domain.OrgSummary) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID))
	})

	return out, nil
}

// Org implements ports.Tenancy.
func (t *Tenancy) Org(_ context.Context, ref string) (domain.Org, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, r := range t.orgs {
		if r.deleted.IsZero() && (r.org.Slug == ref || string(r.org.ID) == ref) {
			return r.org, nil
		}
	}

	return domain.Org{}, fmt.Errorf("org %q: %w", ref, ports.ErrNotFound)
}

// ActiveOrgs implements ports.Tenancy.
func (t *Tenancy) ActiveOrgs(context.Context) ([]domain.OrgID, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var out []domain.OrgID

	for id, r := range t.orgs {
		if r.deleted.IsZero() {
			out = append(out, id)
		}
	}

	slices.Sort(out)

	return out, nil
}

// CreateOrg implements ports.Tenancy.
func (t *Tenancy) CreateOrg(_ context.Context, by domain.Actor, n ports.NewOrg) (domain.Org, error) {
	if n.Mark == "" {
		n.Mark = domain.DefaultMark(n.Name)
	}

	if err := errors.Join(domain.CheckSlug(n.Slug), domain.CheckOrgName(n.Name), domain.CheckMark(n.Mark)); err != nil {
		return domain.Org{}, err
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.slugTaken(n.Slug, "") {
		return domain.Org{}, fmt.Errorf("slug %q is taken: %w", n.Slug, domain.ErrInvalid)
	}

	t.ids++
	o := domain.Org{
		ID:   domain.OrgID(fmt.Sprintf("00000000-0000-7000-8000-%012d", t.ids)),
		Slug: n.Slug, Name: n.Name, Mark: n.Mark, CreatedAt: t.now().UTC().Truncate(time.Microsecond),
	}
	t.orgs[o.ID] = &orgRow{org: o}
	t.appendLocked(o.ID, orgEntry(by, domain.AuditOrgCreated, o, map[string]any{"slug": o.Slug, "name": o.Name}))

	return o, nil
}

// UpdateOrg implements ports.Tenancy.
func (t *Tenancy) UpdateOrg(ctx context.Context, by domain.Actor, u ports.OrgUpdate) (domain.Org, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	r, err := t.liveLocked(ctx)
	if err != nil {
		return domain.Org{}, err
	}

	next, changed, err := u.Apply(r.org)
	if err != nil {
		return domain.Org{}, err
	}

	if len(changed) == 0 {
		return r.org, nil
	}

	if next.Slug != r.org.Slug && t.slugTaken(next.Slug, r.org.ID) {
		return domain.Org{}, fmt.Errorf("slug %q is taken: %w", next.Slug, domain.ErrInvalid)
	}

	r.org = next
	t.appendLocked(next.ID, orgEntry(by, domain.AuditOrgUpdated, next, changed))

	return next, nil
}

// DeleteOrg implements ports.Tenancy.
func (t *Tenancy) DeleteOrg(ctx context.Context, by domain.Actor, now time.Time) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	r, err := t.liveLocked(ctx)
	if err != nil {
		return err
	}

	r.deleted = now
	t.appendLocked(r.org.ID, orgEntry(by, domain.AuditOrgDeleted, r.org, map[string]any{"slug": r.org.Slug, "name": r.org.Name}))

	return nil
}

// PurgeDeletedOrgs implements ports.Tenancy.
func (t *Tenancy) PurgeDeletedOrgs(_ context.Context, cutoff time.Time, limit int) ([]domain.OrgID, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var due []*orgRow

	for _, r := range t.orgs {
		if !r.deleted.IsZero() && r.deleted.Before(cutoff) {
			due = append(due, r)
		}
	}

	slices.SortFunc(due, func(a, b *orgRow) int { return cmp.Or(a.deleted.Compare(b.deleted), cmp.Compare(a.org.ID, b.org.ID)) })

	var out []domain.OrgID

	for _, r := range due[:min(limit, len(due))] {
		delete(t.orgs, r.org.ID)
		t.audit = slices.DeleteFunc(t.audit, func(a auditRow) bool { return a.org == r.org.ID })

		out = append(out, r.org.ID)
	}

	return out, nil
}

// Append implements ports.Audit.
func (t *Tenancy) Append(ctx context.Context, e domain.AuditEntry) error {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return fmt.Errorf("append audit entry: %w", domain.ErrNoOrg)
	}

	if err := domain.CheckAuditAction(e.Action); err != nil {
		return err
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if _, ok := t.orgs[org]; !ok {
		return fmt.Errorf("append audit entry: org %s: %w", org, ports.ErrNotFound)
	}

	t.appendLocked(org, e)

	return nil
}

// AuditLog implements ports.Audit.
func (t *Tenancy) AuditLog(ctx context.Context, q ports.AuditQuery) ([]domain.AuditEntry, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, fmt.Errorf("audit log: %w", domain.ErrNoOrg)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	var out []domain.AuditEntry

	for _, a := range slices.Backward(t.audit) {
		if len(out) == q.Limit {
			break
		}

		if a.org == org && matches(q, a.entry) {
			out = append(out, a.entry)
		}
	}

	return out, nil
}

// matches reports whether e passes q's filters.
func matches(q ports.AuditQuery, e domain.AuditEntry) bool {
	return (q.Before <= 0 || e.ID < q.Before) && (q.Action == "" || e.Action == q.Action) &&
		(q.TargetKind == "" || e.TargetKind == q.TargetKind) && (q.TargetID == "" || e.TargetID == q.TargetID) &&
		(q.FactoryID == "" || e.FactoryID == q.FactoryID)
}

// slugTaken reports whether an org other than except holds slug, live or deleted until purged.
func (t *Tenancy) slugTaken(slug string, except domain.OrgID) bool {
	for id, r := range t.orgs {
		if id != except && r.org.Slug == slug {
			return true
		}
	}

	return false
}

// liveLocked returns the live org ctx is bound to.
func (t *Tenancy) liveLocked(ctx context.Context) (*orgRow, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, fmt.Errorf("org: %w", domain.ErrNoOrg)
	}

	r, ok := t.orgs[org]
	if !ok || !r.deleted.IsZero() {
		return nil, fmt.Errorf("org %s: %w", org, ports.ErrNotFound)
	}

	return r, nil
}

// appendLocked stores e like the database would: an id, a time, Data through JSON.
func (t *Tenancy) appendLocked(org domain.OrgID, e domain.AuditEntry) {
	t.auditIDs++
	e.ID = t.auditIDs

	if e.At.IsZero() {
		e.At = t.now()
	}

	e.At = e.At.UTC().Truncate(time.Microsecond)
	e.Data = jsonRoundTrip(e.Data)
	t.audit = append(t.audit, auditRow{org: org, entry: e})
}

func orgEntry(by domain.Actor, action string, o domain.Org, data map[string]any) domain.AuditEntry {
	return domain.AuditEntry{Actor: by, Action: action, TargetKind: domain.AuditTargetOrg, TargetID: string(o.ID), Data: data}
}

// jsonRoundTrip gives data the shape it has after a jsonb column: nil is an empty object.
func jsonRoundTrip(data map[string]any) map[string]any {
	out := map[string]any{}

	b, err := json.Marshal(data)
	if err == nil {
		_ = json.Unmarshal(b, &out)
	}

	if out == nil {
		out = map[string]any{}
	}

	return out
}
