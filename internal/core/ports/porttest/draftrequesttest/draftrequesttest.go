// Package draftrequesttest is the conformance suite for ports.DraftRequests (ADR 0021). The
// in-memory fake in porttest and the PostgreSQL adapter run it, so tests on the fake hold for the
// database's rules: one queued or running request per factory, the oldest queued request of any
// live organization claimed first, leases fenced by their token, expired leases requeued and
// organizations kept apart.
package draftrequesttest

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Harness is one DraftRequests under test.
type Harness struct {
	Requests ports.DraftRequests
	// NewOrg returns a new organization.
	NewOrg func(t *testing.T) domain.OrgID
	// NewFactory returns a new factory of org.
	NewFactory func(t *testing.T, org domain.OrgID) domain.FactoryID
	// DeleteOrg soft-deletes org.
	DeleteOrg func(t *testing.T, org domain.OrgID)
}

// Run runs the suite. Tests run in parallel and call newHarness once each; harnesses must be
// isolated from each other, since claims and requeues reach every organization.
func Run(t *testing.T, newHarness func(t *testing.T) Harness) {
	t.Helper()

	tests := []struct {
		name string
		test func(f *fixture)
	}{
		{"Create_ThenGetRoundTrips", testRoundTrip},
		{"Create_InvalidRefused", testInvalid},
		{"Create_UnknownFactoryNotFound", testUnknownFactory},
		{"Create_SecondOpenRequestRefused", testOneOpen},
		{"Create_AllowedAfterFinish", testAfterFinish},
		{"List_NewestFirstLimited", testList},
		{"Claim_OldestFirstAcrossOrgs", testClaimOrder},
		{"Claim_NoneQueuedIsNil", testClaimNone},
		{"Claim_SkipsDeletedOrgs", testClaimDeletedOrg},
		{"Claim_ConcurrentClaimsTakeEachOnce", testClaimConcurrent},
		{"Heartbeat_ExtendsTheLease", testHeartbeat},
		{"Heartbeat_EndedLeaseLost", testHeartbeatEnded},
		{"Finish_RecordsTheResult", testFinish},
		{"Finish_InvalidRefused", testFinishInvalid},
		{"Lease_StaleHolderLost", testStaleHolder},
		{"RequeueExpired_OnlyEndedLeases", testRequeue},
		{"OtherOrg_SeesNothing", testIsolation},
		{"Methods_NoOrgIsAnError", testNoOrg},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			f := &fixture{Harness: h, t: t, now: time.Now().UTC().Truncate(time.Microsecond)}
			f.org = h.NewOrg(t)
			f.factory = h.NewFactory(t, f.org)
			tc.test(f)
		})
	}
}

var asker = domain.Actor{Kind: domain.ActorUser, ID: "u-asker", Name: "u-asker"}

type fixture struct {
	Harness

	t       *testing.T
	now     time.Time
	org     domain.OrgID
	factory domain.FactoryID
}

func (f *fixture) in(org domain.OrgID) context.Context { return domain.WithOrg(f.t.Context(), org) }

func (f *fixture) create(org domain.OrgID, factory domain.FactoryID, text string) domain.DraftRequest {
	f.t.Helper()

	r, err := f.Requests.Create(f.in(org), domain.NewDraftRequest{Factory: factory, Requester: asker, Text: text})
	if err != nil {
		f.t.Fatalf("Create: %v", err)
	}

	return r
}

func (f *fixture) get(org domain.OrgID, id domain.DraftRequestID) domain.DraftRequest {
	f.t.Helper()

	r, err := f.Requests.Get(f.in(org), id)
	if err != nil {
		f.t.Fatalf("Get: %v", err)
	}

	return r
}

// claim claims at now for a minute and fails without a claim.
func (f *fixture) claim(now time.Time) *domain.DraftRequestClaim {
	f.t.Helper()

	c, err := f.Requests.Claim(f.t.Context(), now, now.Add(time.Minute))
	if err != nil {
		f.t.Fatalf("Claim: %v", err)
	}

	if c == nil {
		f.t.Fatal("Claim = nil, want a request")
	}

	return c
}

func (f *fixture) finish(c *domain.DraftRequestClaim, at time.Time) {
	f.t.Helper()

	err := f.Requests.Finish(f.in(c.Org), c.Lease, domain.DraftRequestResult{Status: domain.DraftRequestDone, Summary: "done", At: at})
	if err != nil {
		f.t.Fatalf("Finish: %v", err)
	}
}

func testRoundTrip(f *fixture) {
	r, err := f.Requests.Create(f.in(f.org), domain.NewDraftRequest{
		Factory: f.factory, Requester: asker, Text: "  give Review a 4th reviewer ", Agent: "reviewer",
	})
	if err != nil {
		f.t.Fatalf("Create: %v", err)
	}

	if r.ID == "" || r.Org != f.org || r.Factory != f.factory || r.Requester.Ref() != asker.Ref() ||
		r.Text != "give Review a 4th reviewer" || r.Agent != "reviewer" || r.Status != domain.DraftRequestQueued ||
		r.CreatedAt.IsZero() || r.StartedAt != nil || r.FinishedAt != nil || r.LeaseExpiresAt != nil {
		f.t.Fatalf("Create = %+v, want a queued request as asked", r)
	}

	if got := f.get(f.org, r.ID); !same(got, r) {
		f.t.Fatalf("Get = %+v, want %+v", got, r)
	}

	if _, err := f.Requests.Get(f.in(f.org), "01900000-0000-7000-8000-000000000000"); !errors.Is(err, ports.ErrNotFound) {
		f.t.Fatalf("Get an unknown id = %v, want ErrNotFound", err)
	}

	if _, err := f.Requests.Get(f.in(f.org), "not-a-uuid"); !errors.Is(err, ports.ErrNotFound) {
		f.t.Fatalf("Get a malformed id = %v, want ErrNotFound", err)
	}
}

func testInvalid(f *fixture) {
	for _, n := range []domain.NewDraftRequest{
		{Factory: f.factory, Requester: asker, Text: ""},
		{Factory: f.factory, Requester: asker, Text: strings.Repeat("a", domain.MaxDraftRequestText+1)},
		{Factory: f.factory, Requester: asker, Text: "ok", Agent: "Not An Agent"},
	} {
		if _, err := f.Requests.Create(f.in(f.org), n); !errors.Is(err, domain.ErrInvalid) {
			f.t.Errorf("Create(%q, agent %q) = %v, want ErrInvalid", n.Text, n.Agent, err)
		}
	}

	if got, err := f.Requests.List(f.in(f.org), f.factory, 10); err != nil || len(got) != 0 {
		f.t.Fatalf("List = %v, %v; want nothing stored", got, err)
	}
}

func testUnknownFactory(f *fixture) {
	other := f.NewFactory(f.t, f.NewOrg(f.t)) // another organization's factory is unknown here too

	for _, id := range []domain.FactoryID{"01900000-0000-7000-8000-000000000000", "not-a-uuid", other} {
		_, err := f.Requests.Create(f.in(f.org), domain.NewDraftRequest{Factory: id, Requester: asker, Text: "add a reviewer"})
		if !errors.Is(err, ports.ErrNotFound) {
			f.t.Errorf("Create on factory %s = %v, want ErrNotFound", id, err)
		}
	}
}

func testOneOpen(f *fixture) {
	f.create(f.org, f.factory, "first")

	if _, err := f.Requests.Create(f.in(f.org), domain.NewDraftRequest{Factory: f.factory, Requester: asker, Text: "second"}); !errors.Is(err, domain.ErrDraftRequestOpen) {
		f.t.Fatalf("Create a second queued = %v, want ErrDraftRequestOpen", err)
	}

	f.claim(f.now)

	if _, err := f.Requests.Create(f.in(f.org), domain.NewDraftRequest{Factory: f.factory, Requester: asker, Text: "second"}); !errors.Is(err, domain.ErrDraftRequestOpen) {
		f.t.Fatalf("Create beside a running one = %v, want ErrDraftRequestOpen", err)
	}

	// Another factory has its own turn.
	f.create(f.org, f.NewFactory(f.t, f.org), "elsewhere")

	if got, _ := f.Requests.List(f.in(f.org), f.factory, 10); len(got) != 1 {
		f.t.Fatalf("List = %d requests, want the first only", len(got))
	}
}

func testAfterFinish(f *fixture) {
	f.create(f.org, f.factory, "first")
	f.finish(f.claim(f.now), f.now.Add(time.Second))
	second := f.create(f.org, f.factory, "second")

	c := f.claim(f.now.Add(2 * time.Second))
	if c.Request.ID != second.ID {
		f.t.Fatalf("Claim = %s, want the second request %s", c.Request.ID, second.ID)
	}

	err := f.Requests.Finish(f.in(f.org), c.Lease, domain.DraftRequestResult{Status: domain.DraftRequestFailed, Error: "box exited", At: f.now.Add(3 * time.Second)})
	if err != nil {
		f.t.Fatalf("Finish failed: %v", err)
	}

	f.create(f.org, f.factory, "third")
}

func testList(f *fixture) {
	var ids []domain.DraftRequestID

	for i := range 3 {
		r := f.create(f.org, f.factory, "ask")
		ids = append(ids, r.ID)

		f.finish(f.claim(f.now.Add(time.Duration(i)*time.Second)), f.now.Add(time.Duration(i)*time.Second))
	}

	f.create(f.org, f.NewFactory(f.t, f.org), "another factory's")

	got, err := f.Requests.List(f.in(f.org), f.factory, 2)
	if err != nil {
		f.t.Fatalf("List: %v", err)
	}

	if want := []domain.DraftRequestID{ids[2], ids[1]}; !slices.Equal(idsOf(got), want) {
		f.t.Fatalf("List(2) = %v, want %v", idsOf(got), want)
	}

	for _, limit := range []int{0, -1, domain.MaxDraftRequestList + 1} {
		if _, err := f.Requests.List(f.in(f.org), f.factory, limit); !errors.Is(err, domain.ErrInvalid) {
			f.t.Errorf("List(%d) = %v, want ErrInvalid", limit, err)
		}
	}

	if got, err := f.Requests.List(f.in(f.org), "not-a-uuid", 10); err != nil || len(got) != 0 {
		f.t.Errorf("List of an unknown factory = %v, %v; want none", got, err)
	}
}

func testClaimOrder(f *fixture) {
	org2 := f.NewOrg(f.t)
	first := f.create(f.org, f.factory, "first")
	second := f.create(org2, f.NewFactory(f.t, org2), "second")
	third := f.create(f.org, f.NewFactory(f.t, f.org), "third")

	for _, want := range []struct {
		id  domain.DraftRequestID
		org domain.OrgID
	}{{first.ID, f.org}, {second.ID, org2}, {third.ID, f.org}} {
		c := f.claim(f.now)
		if c.Request.ID != want.id || c.Org != want.org || c.Lease.Request != want.id || c.Lease.Token == "" {
			f.t.Fatalf("Claim = %s of %s (lease %+v), want %s of %s", c.Request.ID, c.Org, c.Lease, want.id, want.org)
		}

		r := c.Request
		if r.Status != domain.DraftRequestRunning || r.StartedAt == nil || !r.StartedAt.Equal(f.now) ||
			r.LeaseExpiresAt == nil || !r.LeaseExpiresAt.Equal(f.now.Add(time.Minute)) {
			f.t.Fatalf("claimed = %+v, want running since now with its lease", r)
		}

		if got := f.get(c.Org, r.ID); !same(got, r) {
			f.t.Fatalf("Get after claim = %+v, want %+v", got, r)
		}
	}
}

func testClaimNone(f *fixture) {
	if c, err := f.Requests.Claim(f.t.Context(), f.now, f.now.Add(time.Minute)); err != nil || c != nil {
		f.t.Fatalf("Claim with nothing queued = %v, %v; want nil", c, err)
	}

	f.create(f.org, f.factory, "one")
	f.claim(f.now)

	if c, err := f.Requests.Claim(f.t.Context(), f.now, f.now.Add(time.Minute)); err != nil || c != nil {
		f.t.Fatalf("Claim with only a running one = %v, %v; want nil", c, err)
	}
}

func testClaimDeletedOrg(f *fixture) {
	gone := f.NewOrg(f.t)
	f.create(gone, f.NewFactory(f.t, gone), "of a deleted organization")
	f.DeleteOrg(f.t, gone)

	if c, err := f.Requests.Claim(f.t.Context(), f.now, f.now.Add(time.Minute)); err != nil || c != nil {
		f.t.Fatalf("Claim = %v, %v; want nil: a deleted organization's request waits", c, err)
	}

	live := f.create(f.org, f.factory, "live")
	if c := f.claim(f.now); c.Request.ID != live.ID {
		f.t.Fatalf("Claim = %s, want the live organization's %s", c.Request.ID, live.ID)
	}
}

func testClaimConcurrent(f *fixture) {
	const n = 4

	want := make([]domain.DraftRequestID, 0, n)
	for range n {
		want = append(want, f.create(f.org, f.NewFactory(f.t, f.org), "ask").ID)
	}

	var (
		mu  sync.Mutex
		got []domain.DraftRequestID
		wg  sync.WaitGroup
	)

	for range 2 * n {
		wg.Go(func() {
			c, err := f.Requests.Claim(f.t.Context(), f.now, f.now.Add(time.Minute))
			if err != nil {
				f.t.Errorf("Claim: %v", err)

				return
			}

			if c != nil {
				mu.Lock()
				got = append(got, c.Request.ID)
				mu.Unlock()
			}
		})
	}

	wg.Wait()
	slices.Sort(got)
	slices.Sort(want)

	if !slices.Equal(got, want) {
		f.t.Fatalf("concurrent claims took %v, want each of %v once", got, want)
	}
}

func testHeartbeat(f *fixture) {
	f.create(f.org, f.factory, "ask")
	c := f.claim(f.now)

	until := f.now.Add(5 * time.Minute)
	if err := f.Requests.Heartbeat(f.in(f.org), c.Lease, f.now.Add(30*time.Second), until); err != nil {
		f.t.Fatalf("Heartbeat: %v", err)
	}

	if r := f.get(f.org, c.Request.ID); r.LeaseExpiresAt == nil || !r.LeaseExpiresAt.Equal(until) {
		f.t.Fatalf("lease ends %v, want %v", r.LeaseExpiresAt, until)
	}

	// The lease now outlives the first claim's minute.
	if n, err := f.Requests.RequeueExpired(f.t.Context(), f.now.Add(2*time.Minute)); err != nil || n != 0 {
		f.t.Fatalf("RequeueExpired = %d, %v; want 0", n, err)
	}
}

func testHeartbeatEnded(f *fixture) {
	f.create(f.org, f.factory, "ask")
	c := f.claim(f.now)

	err := f.Requests.Heartbeat(f.in(f.org), c.Lease, f.now.Add(2*time.Minute), f.now.Add(3*time.Minute))
	if !errors.Is(err, ports.ErrLeaseLost) {
		f.t.Fatalf("Heartbeat after the lease ended = %v, want ErrLeaseLost", err)
	}

	f.finish(c, f.now.Add(2*time.Minute))

	if err := f.Requests.Heartbeat(f.in(f.org), c.Lease, f.now, f.now.Add(time.Minute)); !errors.Is(err, ports.ErrLeaseLost) {
		f.t.Fatalf("Heartbeat after Finish = %v, want ErrLeaseLost", err)
	}

	bad := domain.DraftRequestLease{Request: "not-a-uuid", Token: "not-a-uuid"}
	if err := f.Requests.Heartbeat(f.in(f.org), bad, f.now, f.now.Add(time.Minute)); !errors.Is(err, ports.ErrLeaseLost) {
		f.t.Fatalf("Heartbeat of a malformed lease = %v, want ErrLeaseLost", err)
	}
}

func testFinish(f *fixture) {
	f.create(f.org, f.factory, "ask")
	c := f.claim(f.now)
	at := f.now.Add(42 * time.Second)

	err := f.Requests.Finish(f.in(f.org), c.Lease, domain.DraftRequestResult{
		Status: domain.DraftRequestDone, Summary: "gave Review a 4th reviewer",
		ChangedPaths: []string{"factory.yaml", "agents/reviewer.md"}, CostUSD: 0.4321, Turns: 12, At: at,
	})
	if err != nil {
		f.t.Fatalf("Finish: %v", err)
	}

	r := f.get(f.org, c.Request.ID)
	if r.Status != domain.DraftRequestDone || r.Summary != "gave Review a 4th reviewer" ||
		!slices.Equal(r.ChangedPaths, []string{"factory.yaml", "agents/reviewer.md"}) || r.CostUSD != 0.4321 || r.Turns != 12 ||
		r.Error != "" || r.FinishedAt == nil || !r.FinishedAt.Equal(at) || r.LeaseExpiresAt != nil || r.StartedAt == nil {
		f.t.Fatalf("finished = %+v, want done with the result", r)
	}

	if err := f.Requests.Finish(f.in(f.org), c.Lease, domain.DraftRequestResult{Status: domain.DraftRequestFailed, At: at}); !errors.Is(err, ports.ErrLeaseLost) {
		f.t.Fatalf("a second Finish = %v, want ErrLeaseLost", err)
	}

	f.create(f.org, f.NewFactory(f.t, f.org), "long")
	c = f.claim(f.now)

	long := strings.Repeat("é", domain.MaxDraftRequestSummary+10)
	if err := f.Requests.Finish(f.in(f.org), c.Lease, domain.DraftRequestResult{Status: domain.DraftRequestFailed, Summary: long, Error: long, At: at}); err != nil {
		f.t.Fatalf("Finish with a long summary: %v", err)
	}

	r = f.get(f.org, c.Request.ID)
	if r.Status != domain.DraftRequestFailed || len([]rune(r.Summary)) != domain.MaxDraftRequestSummary || len([]rune(r.Error)) != domain.MaxDraftRequestError {
		f.t.Fatalf("failed = %s with a %d character summary and %d character error, want them cut", r.Status, len([]rune(r.Summary)), len([]rune(r.Error)))
	}
}

func testFinishInvalid(f *fixture) {
	f.create(f.org, f.factory, "ask")
	c := f.claim(f.now)

	for _, r := range []domain.DraftRequestResult{
		{Status: domain.DraftRequestQueued, At: f.now},
		{Status: "cancelled", At: f.now},
		{Status: domain.DraftRequestDone, CostUSD: -1, At: f.now},
	} {
		if err := f.Requests.Finish(f.in(f.org), c.Lease, r); !errors.Is(err, domain.ErrInvalid) {
			f.t.Errorf("Finish(%+v) = %v, want ErrInvalid", r, err)
		}
	}

	if r := f.get(f.org, c.Request.ID); r.Status != domain.DraftRequestRunning {
		f.t.Fatalf("status = %s, want still running", r.Status)
	}
}

func testStaleHolder(f *fixture) {
	f.create(f.org, f.factory, "ask")
	stale := f.claim(f.now)

	// The holder stalls past its lease; the request is requeued and claimed again.
	later := f.now.Add(2 * time.Minute)
	if n, err := f.Requests.RequeueExpired(f.t.Context(), later); err != nil || n != 1 {
		f.t.Fatalf("RequeueExpired = %d, %v; want 1", n, err)
	}

	fresh := f.claim(later)
	if fresh.Request.ID != stale.Request.ID || fresh.Lease.Token == stale.Lease.Token {
		f.t.Fatalf("second claim = %+v, want the same request under a new token", fresh.Lease)
	}

	if err := f.Requests.Heartbeat(f.in(f.org), stale.Lease, later, later.Add(time.Minute)); !errors.Is(err, ports.ErrLeaseLost) {
		f.t.Fatalf("stale Heartbeat = %v, want ErrLeaseLost", err)
	}

	err := f.Requests.Finish(f.in(f.org), stale.Lease, domain.DraftRequestResult{Status: domain.DraftRequestDone, Summary: "stale", At: later})
	if !errors.Is(err, ports.ErrLeaseLost) {
		f.t.Fatalf("stale Finish = %v, want ErrLeaseLost", err)
	}

	if r := f.get(f.org, fresh.Request.ID); r.Status != domain.DraftRequestRunning || r.Summary != "" {
		f.t.Fatalf("after the stale holder = %+v, want running untouched", r)
	}

	if err := f.Requests.Heartbeat(f.in(f.org), fresh.Lease, later, later.Add(time.Minute)); err != nil {
		f.t.Fatalf("fresh Heartbeat: %v", err)
	}

	f.finish(fresh, later)
}

func testRequeue(f *fixture) {
	org2 := f.NewOrg(f.t)
	f.create(f.org, f.factory, "short lease")
	f.create(org2, f.NewFactory(f.t, org2), "long lease")
	queued := f.create(f.org, f.NewFactory(f.t, f.org), "still queued")

	short := f.claim(f.now)

	long, err := f.Requests.Claim(f.t.Context(), f.now, f.now.Add(time.Hour))
	if err != nil || long == nil {
		f.t.Fatalf("Claim = %v, %v", long, err)
	}

	if n, err := f.Requests.RequeueExpired(f.t.Context(), f.now.Add(30*time.Second)); err != nil || n != 0 {
		f.t.Fatalf("RequeueExpired before any lease ended = %d, %v; want 0", n, err)
	}

	if n, err := f.Requests.RequeueExpired(f.t.Context(), f.now.Add(time.Minute)); err != nil || n != 1 {
		f.t.Fatalf("RequeueExpired once the short lease ended = %d, %v; want 1", n, err)
	}

	r := f.get(f.org, short.Request.ID)
	if r.Status != domain.DraftRequestQueued || r.StartedAt != nil || r.LeaseExpiresAt != nil {
		f.t.Fatalf("requeued = %+v, want queued with no start or lease", r)
	}

	if r := f.get(org2, long.Request.ID); r.Status != domain.DraftRequestRunning {
		f.t.Fatalf("long lease = %s, want still running", r.Status)
	}

	if r := f.get(f.org, queued.ID); r.Status != domain.DraftRequestQueued {
		f.t.Fatalf("queued = %s, want untouched", r.Status)
	}

	// The requeued request keeps its place: it is older than the one still queued.
	if c := f.claim(f.now.Add(time.Minute)); c.Request.ID != short.Request.ID {
		f.t.Fatalf("Claim after requeue = %s, want the requeued %s", c.Request.ID, short.Request.ID)
	}
}

func testIsolation(f *fixture) {
	r := f.create(f.org, f.factory, "ours")
	other := f.NewOrg(f.t)

	if _, err := f.Requests.Get(f.in(other), r.ID); !errors.Is(err, ports.ErrNotFound) {
		f.t.Fatalf("Get from another organization = %v, want ErrNotFound", err)
	}

	if got, err := f.Requests.List(f.in(other), f.factory, 10); err != nil || len(got) != 0 {
		f.t.Fatalf("List from another organization = %v, %v; want none", got, err)
	}

	c := f.claim(f.now)

	if err := f.Requests.Heartbeat(f.in(other), c.Lease, f.now, f.now.Add(time.Minute)); !errors.Is(err, ports.ErrLeaseLost) {
		f.t.Fatalf("Heartbeat from another organization = %v, want ErrLeaseLost", err)
	}

	err := f.Requests.Finish(f.in(other), c.Lease, domain.DraftRequestResult{Status: domain.DraftRequestDone, At: f.now})
	if !errors.Is(err, ports.ErrLeaseLost) {
		f.t.Fatalf("Finish from another organization = %v, want ErrLeaseLost", err)
	}
}

func testNoOrg(f *fixture) {
	ctx := f.t.Context()
	n := domain.NewDraftRequest{Factory: f.factory, Requester: asker, Text: "ask"}
	l := domain.DraftRequestLease{Request: "01900000-0000-7000-8000-000000000000", Token: "01900000-0000-7000-8000-000000000000"}

	if _, err := f.Requests.Create(ctx, n); !errors.Is(err, domain.ErrNoOrg) {
		f.t.Errorf("Create = %v, want ErrNoOrg", err)
	}

	if _, err := f.Requests.Get(ctx, l.Request); !errors.Is(err, domain.ErrNoOrg) {
		f.t.Errorf("Get = %v, want ErrNoOrg", err)
	}

	if _, err := f.Requests.List(ctx, f.factory, 10); !errors.Is(err, domain.ErrNoOrg) {
		f.t.Errorf("List = %v, want ErrNoOrg", err)
	}

	if err := f.Requests.Heartbeat(ctx, l, f.now, f.now); !errors.Is(err, domain.ErrNoOrg) {
		f.t.Errorf("Heartbeat = %v, want ErrNoOrg", err)
	}

	if err := f.Requests.Finish(ctx, l, domain.DraftRequestResult{Status: domain.DraftRequestDone, At: f.now}); !errors.Is(err, domain.ErrNoOrg) {
		f.t.Errorf("Finish = %v, want ErrNoOrg", err)
	}
}

func idsOf(rs []domain.DraftRequest) []domain.DraftRequestID {
	out := make([]domain.DraftRequestID, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.ID)
	}

	return out
}

// same compares two reads of a request: times by instant, the requester by ref.
func same(a, b domain.DraftRequest) bool {
	return a.ID == b.ID && a.Org == b.Org && a.Factory == b.Factory && a.Requester.Ref() == b.Requester.Ref() &&
		a.Text == b.Text && a.Agent == b.Agent && a.Status == b.Status && a.Summary == b.Summary &&
		slices.Equal(a.ChangedPaths, b.ChangedPaths) && a.CostUSD == b.CostUSD && a.Turns == b.Turns && a.Error == b.Error &&
		a.CreatedAt.Equal(b.CreatedAt) && sameTime(a.StartedAt, b.StartedAt) && sameTime(a.FinishedAt, b.FinishedAt) &&
		sameTime(a.LeaseExpiresAt, b.LeaseExpiresAt)
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}

	return a.Equal(*b)
}
