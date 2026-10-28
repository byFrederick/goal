package porttest

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var (
	_ ports.DraftRequests    = (*DraftRequests)(nil)
	_ ports.DefinitionRunner = (*DefinitionRunner)(nil)
)

// DraftRequests keeps draft requests in memory with the database's rules: one queued or running
// request per factory, claims of the oldest queued request of a live organization (the tenancy's
// ActiveOrgs), leases fenced by a token drawn on each claim, and organizations kept apart. It
// knows only the factories AddFactory registered.
type DraftRequests struct {
	tenancy ports.Tenancy
	now     func() time.Time

	mu        sync.Mutex
	factories map[domain.FactoryID]domain.OrgID
	rows      []*draftRequestRow // in creation order
	ids       int
}

type draftRequestRow struct {
	r     domain.DraftRequest
	token string
}

// NewDraftRequests returns requests whose claims see the live organizations of tenancy, with now
// as the time of a creation.
func NewDraftRequests(tenancy ports.Tenancy, now func() time.Time) *DraftRequests {
	return &DraftRequests{tenancy: tenancy, now: now, factories: map[domain.FactoryID]domain.OrgID{}}
}

// AddFactory registers factory as org's, so requests may be created on it.
func (d *DraftRequests) AddFactory(org domain.OrgID, factory domain.FactoryID) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.factories[factory] = org
}

// Create implements ports.DraftRequests.
func (d *DraftRequests) Create(ctx context.Context, n domain.NewDraftRequest) (domain.DraftRequest, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.DraftRequest{}, domain.ErrNoOrg
	}

	if err := n.Check(); err != nil {
		return domain.DraftRequest{}, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.factories[n.Factory] != org {
		return domain.DraftRequest{}, fmt.Errorf("factory %s: %w", n.Factory, ports.ErrNotFound)
	}

	for _, row := range d.rows {
		if row.r.Factory == n.Factory && row.r.Status.Open() {
			return domain.DraftRequest{}, fmt.Errorf("factory %s: %w", n.Factory, domain.ErrDraftRequestOpen)
		}
	}

	d.ids++
	r := domain.DraftRequest{
		ID: domain.DraftRequestID(fmt.Sprintf("01900000-0000-7000-8000-%012d", d.ids)), Org: org, Factory: n.Factory,
		Requester: storedActor(n.Requester), Text: n.Text, Agent: n.Agent, Status: domain.DraftRequestQueued,
		CreatedAt: d.now().UTC().Truncate(time.Microsecond),
	}
	d.rows = append(d.rows, &draftRequestRow{r: r})

	return cloneDraftRequest(r), nil
}

// Get implements ports.DraftRequests.
func (d *DraftRequests) Get(ctx context.Context, id domain.DraftRequestID) (domain.DraftRequest, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.DraftRequest{}, domain.ErrNoOrg
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	for _, row := range d.rows {
		if row.r.ID == id && row.r.Org == org {
			return cloneDraftRequest(row.r), nil
		}
	}

	return domain.DraftRequest{}, fmt.Errorf("draft request %s: %w", id, ports.ErrNotFound)
}

// List implements ports.DraftRequests.
func (d *DraftRequests) List(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.DraftRequest, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, domain.ErrNoOrg
	}

	if limit < 1 || limit > domain.MaxDraftRequestList {
		return nil, fmt.Errorf("list %d draft requests, 1 to %d: %w", limit, domain.MaxDraftRequestList, domain.ErrInvalid)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	var out []domain.DraftRequest

	for _, row := range slices.Backward(d.rows) {
		if row.r.Org == org && row.r.Factory == factory && len(out) < limit {
			out = append(out, cloneDraftRequest(row.r))
		}
	}

	return out, nil
}

// Claim implements ports.DraftRequests.
func (d *DraftRequests) Claim(ctx context.Context, now, until time.Time) (*domain.DraftRequestClaim, error) {
	live, err := d.tenancy.ActiveOrgs(ctx)
	if err != nil {
		return nil, fmt.Errorf("claim draft request: %w", err)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	var next *draftRequestRow

	for _, row := range d.rows {
		if row.r.Status != domain.DraftRequestQueued || !slices.Contains(live, row.r.Org) {
			continue
		}

		if next == nil || cmp.Or(row.r.CreatedAt.Compare(next.r.CreatedAt), cmp.Compare(row.r.ID, next.r.ID)) < 0 {
			next = row
		}
	}

	if next == nil {
		return nil, nil //nolint:nilnil // nothing queued is no claim, as the port says
	}

	d.ids++
	started, ends := now.UTC().Truncate(time.Microsecond), until.UTC().Truncate(time.Microsecond)
	next.r.Status, next.r.StartedAt, next.r.LeaseExpiresAt = domain.DraftRequestRunning, &started, &ends
	next.token = fmt.Sprintf("01900000-0000-7000-9000-%012d", d.ids)

	return &domain.DraftRequestClaim{
		Org: next.r.Org, Request: cloneDraftRequest(next.r), Lease: domain.DraftRequestLease{Request: next.r.ID, Token: next.token},
	}, nil
}

// heldLocked is the row l holds in the organization of ctx while it is running; ErrLeaseLost
// otherwise.
func (d *DraftRequests) heldLocked(org domain.OrgID, l domain.DraftRequestLease) (*draftRequestRow, error) {
	for _, row := range d.rows {
		if row.r.ID == l.Request && row.r.Org == org && row.token == l.Token && row.r.Status == domain.DraftRequestRunning {
			return row, nil
		}
	}

	return nil, fmt.Errorf("draft request %s: %w", l.Request, ports.ErrLeaseLost)
}

// Heartbeat implements ports.DraftRequests.
func (d *DraftRequests) Heartbeat(ctx context.Context, l domain.DraftRequestLease, now, until time.Time) error {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.ErrNoOrg
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	row, err := d.heldLocked(org, l)
	if err != nil {
		return err
	}

	if !row.r.LeaseExpiresAt.After(now) {
		return fmt.Errorf("draft request %s: %w", l.Request, ports.ErrLeaseLost)
	}

	ends := until.UTC().Truncate(time.Microsecond)
	row.r.LeaseExpiresAt = &ends

	return nil
}

// Finish implements ports.DraftRequests.
func (d *DraftRequests) Finish(ctx context.Context, l domain.DraftRequestLease, r domain.DraftRequestResult) error {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.ErrNoOrg
	}

	if err := r.Check(); err != nil {
		return err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	row, err := d.heldLocked(org, l)
	if err != nil {
		return err
	}

	at := r.At.UTC().Truncate(time.Microsecond)
	row.r.Status, row.r.Summary, row.r.Error = r.Status, cutRunes(r.Summary, domain.MaxDraftRequestSummary), cutRunes(r.Error, domain.MaxDraftRequestError)
	row.r.ChangedPaths, row.r.CostUSD, row.r.Turns = slices.Clone(r.ChangedPaths), r.CostUSD, r.Turns
	row.r.FinishedAt, row.r.LeaseExpiresAt, row.token = &at, nil, ""

	return nil
}

// RequeueExpired implements ports.DraftRequests.
func (d *DraftRequests) RequeueExpired(_ context.Context, now time.Time) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	n := 0

	for _, row := range d.rows {
		if row.r.Status == domain.DraftRequestRunning && !row.r.LeaseExpiresAt.After(now) {
			row.r.Status, row.r.StartedAt, row.r.LeaseExpiresAt, row.token = domain.DraftRequestQueued, nil, nil, ""
			n++
		}
	}

	return n, nil
}

// storedActor is a as the database gives it back: from its ref alone.
func storedActor(a domain.Actor) domain.Actor {
	if s, err := domain.ActorFromRef(a.Ref()); err == nil {
		return s
	}

	return a
}

func cloneDraftRequest(r domain.DraftRequest) domain.DraftRequest {
	r.ChangedPaths = slices.Clone(r.ChangedPaths)

	return r
}

// cutRunes keeps the first n characters of s.
func cutRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}

	return s
}

// DefinitionRunner answers draft request runs with a function a test sets, and keeps the tasks it
// was given.
type DefinitionRunner struct {
	mu    sync.Mutex
	run   func(ctx context.Context, call int, t ports.DefinitionTask) (ports.DefinitionResult, error)
	tasks []ports.DefinitionTask
}

// NewDefinitionRunner answers each run with run, given the call's number from 1.
func NewDefinitionRunner(run func(ctx context.Context, call int, t ports.DefinitionTask) (ports.DefinitionResult, error)) *DefinitionRunner {
	return &DefinitionRunner{run: run}
}

// Tasks returns every task it was given, in order.
func (d *DefinitionRunner) Tasks() []ports.DefinitionTask {
	d.mu.Lock()
	defer d.mu.Unlock()

	return slices.Clone(d.tasks)
}

// RunDefinition implements ports.DefinitionRunner.
func (d *DefinitionRunner) RunDefinition(ctx context.Context, t ports.DefinitionTask) (ports.DefinitionResult, error) {
	d.mu.Lock()
	d.tasks = append(d.tasks, t)
	call := len(d.tasks)
	d.mu.Unlock()

	return d.run(ctx, call, t)
}
