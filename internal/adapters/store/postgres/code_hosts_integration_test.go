//go:build integration

package postgres

import (
	"reflect"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/byFrederick/goal/internal/core/domain"
)

// An organization's added hosts round-trip in byte order whatever the collation, row-level
// security keeps them from every other organization, even to raw SQL, and the table itself
// refuses a built-in or upper-case host however it is written.
func TestCodeHosts_RoundTripAndIsolation(t *testing.T) {
	t.Parallel()

	pool := newTestPool(t)
	s := New(pool)
	a, b := newOrgID(t, s), newOrgID(t, s)
	ctxA := domain.WithOrg(t.Context(), a)

	want := []domain.CodeHost{
		{Host: "git-b.example.com", Kind: domain.CodeHostGitLab},
		{Host: "git.example.com", Kind: domain.CodeHostGitHub},
	}

	if got, err := s.SetCodeHosts(ctxA, domain.LocalUser, []domain.CodeHost{want[1], want[0]}); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("SetCodeHosts = %+v, %v; want %+v", got, err, want)
	}

	if got, err := s.CodeHosts(ctxA); err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("CodeHosts = %+v, %v; want %+v", got, err, want)
	}

	if got, err := s.CodeHosts(domain.WithOrg(t.Context(), b)); err != nil || len(got) != 0 {
		t.Errorf("another organization's CodeHosts = %+v, %v; want none", got, err)
	}

	var n int
	if err := asOrg(t, pool, b, "SELECT count(*) FROM code_hosts", &n); err != nil || n != 0 {
		t.Errorf("another organization counts %d hosts, %v; want none", n, err)
	}

	if err := asOrg(t, pool, a, "SELECT count(*) FROM code_hosts WHERE created_by = 'user:local'", &n); err != nil || n != 2 {
		t.Errorf("hosts added by the local user = %d, %v; want 2", n, err)
	}

	refuseRawCodeHosts(t, pool, a, "github.com", "GIT.example.org", "localhost")
}

// Two instances seeding one organization at once, each on its own pool, seed it once: exactly
// one call reports the seed, each host is stored once and one audit entry is appended. A host
// an owner removes afterwards is not seeded back.
func TestCodeHosts_SeedOnTwoInstancesAddsEachOnce(t *testing.T) {
	t.Parallel()

	tdb := newTestDB(t, OwnerRole)
	migrate(t, tdb.owner)

	pools := []*pgxpool.Pool{openPool(t, tdb.app), openPool(t, tdb.app)}
	stores := []*Store{New(pools[0]), New(pools[1])}
	org := newOrgID(t, stores[0])
	ctx := domain.WithOrg(t.Context(), org)

	hosts := []domain.CodeHost{
		{Host: "ghe.example.com", Kind: domain.CodeHostGitHub},
		{Host: "gitlab.example.com", Kind: domain.CodeHostGitLab},
	}

	var (
		wg      sync.WaitGroup
		start   = make(chan struct{})
		results = make([]bool, len(stores))
		errs    = make([]error, len(stores))
	)

	for i, s := range stores {
		wg.Go(func() {
			<-start

			results[i], errs[i] = s.SeedCodeHosts(ctx, domain.LocalUser, hosts)
		})
	}

	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("instance %d: SeedCodeHosts: %v", i, err)
		}
	}

	if results[0] == results[1] {
		t.Errorf("seeded = %v; want exactly one instance to seed", results)
	}

	if got, err := stores[1].CodeHosts(ctx); err != nil || !reflect.DeepEqual(got, hosts) {
		t.Errorf("CodeHosts = %+v, %v; want each host once: %+v", got, err, hosts)
	}

	var n int
	if err := asOrg(t, pools[0], org, "SELECT count(*) FROM audit_log WHERE action = 'org.code_hosts_set'", &n); err != nil || n != 1 {
		t.Errorf("org.code_hosts_set entries = %d, %v; want 1", n, err)
	}

	if _, err := stores[0].SetCodeHosts(ctx, domain.LocalUser, hosts[:1]); err != nil {
		t.Fatal(err)
	}

	if seeded, err := stores[1].SeedCodeHosts(ctx, domain.LocalUser, hosts); err != nil || seeded {
		t.Errorf("a later SeedCodeHosts = %v, %v; want no seed", seeded, err)
	}

	if got, err := stores[0].CodeHosts(ctx); err != nil || !reflect.DeepEqual(got, hosts[:1]) {
		t.Errorf("after the removal and a later seed = %+v, %v; want %+v", got, err, hosts[:1])
	}
}

// refuseRawCodeHosts checks that code_hosts refuses each of hosts written straight in SQL as org.
func refuseRawCodeHosts(t *testing.T, pool *pgxpool.Pool, org domain.OrgID, hosts ...string) {
	t.Helper()

	for _, host := range hosts {
		err := inOrgTx(t, pool, org, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), "INSERT INTO code_hosts (host, kind, created_by) VALUES ($1, 'github', 'user:local')", host)

			return err
		})
		if err == nil {
			t.Errorf("inserting %q: no error; want the table to refuse it", host)
		}
	}
}
