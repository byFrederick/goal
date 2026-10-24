//go:build integration

package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest/tenancytest"
)

func TestTenancy_Conformance(t *testing.T) {
	t.Parallel()

	tenancytest.Run(t, func(t *testing.T) tenancytest.Harness {
		t.Helper()

		s := New(newTestPool(t))

		return tenancytest.Harness{
			Tenancy: s, Audit: s,
			NewFactory: func(t *testing.T, org domain.OrgID) domain.FactoryID {
				t.Helper()

				return saveFactory(domain.WithOrg(t.Context(), org), t, s)
			},
		}
	})
}

// While another instance's purge is in its transaction, a purge does nothing; once that one has
// committed, the next purge takes the organizations deleted since.
func TestPurgeDeletedOrgs_OneInstanceAtATime(t *testing.T) {
	t.Parallel()

	s := New(newTestPool(t))
	deleted := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	cutoff := deleted.Add(time.Second)
	first := deletedOrg(t, s, "acme", deleted)

	holding := holdPurge(t, s, cutoff)

	if ids, err := s.PurgeDeletedOrgs(t.Context(), cutoff, 10, nil); err != nil || len(ids) != 0 {
		t.Errorf("PurgeDeletedOrgs while another purge holds the lock = %v, %v; want nothing", ids, err)
	}

	second := deletedOrg(t, s, "example-labs", deleted)

	if ids := holding.release(); len(ids) != 1 || ids[0] != first {
		t.Errorf("the holding purge = %v; want [%s]", ids, first)
	}

	if ids, err := s.PurgeDeletedOrgs(t.Context(), cutoff, 10, nil); err != nil || len(ids) != 1 || ids[0] != second {
		t.Errorf("PurgeDeletedOrgs after the lock is released = %v, %v; want [%s]", ids, err, second)
	}
}

// The purge lock belongs to one install (its schema): a purge running in another install on the
// same PostgreSQL does not stop this one.
func TestPurgeDeletedOrgs_InstallsDoNotWaitForEachOther(t *testing.T) {
	t.Parallel()

	deleted := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	cutoff := deleted.Add(time.Second)

	other := New(newTestPool(t))
	deletedOrg(t, other, "acme", deleted)

	holdPurge(t, other, cutoff)

	s := New(newTestPool(t))
	o := deletedOrg(t, s, "acme", deleted)

	if ids, err := s.PurgeDeletedOrgs(t.Context(), cutoff, 10, nil); err != nil || len(ids) != 1 || ids[0] != o {
		t.Errorf("PurgeDeletedOrgs while another install purges = %v, %v; want [%s]", ids, err, o)
	}
}

// deletedOrg creates an organization and deletes it at the given time.
func deletedOrg(t *testing.T, s *Store, slug string, at time.Time) domain.OrgID {
	t.Helper()

	o, err := s.CreateOrg(t.Context(), domain.LocalUser, newOrg(slug, slug))
	if err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteOrg(domain.WithOrg(t.Context(), o.ID), domain.LocalUser, at); err != nil {
		t.Fatal(err)
	}

	return o.ID
}

// heldPurge is a purge stopped inside its transaction, in its hook for the first organization.
type heldPurge struct {
	resume chan struct{}
	done   chan []domain.OrgID
	once   sync.Once
	ids    []domain.OrgID
}

// holdPurge starts a purge of s that holds the purge lock until release, or until the test ends;
// s needs a deleted organization for the purge to reach its hook.
func holdPurge(t *testing.T, s *Store, cutoff time.Time) *heldPurge {
	t.Helper()

	h := &heldPurge{resume: make(chan struct{}), done: make(chan []domain.OrgID, 1)}
	inside := make(chan struct{})

	go func() {
		var once sync.Once

		ids, err := s.PurgeDeletedOrgs(context.WithoutCancel(t.Context()), cutoff, 10, func(context.Context, domain.OrgID) error {
			once.Do(func() { close(inside) })
			<-h.resume

			return nil
		})
		if err != nil {
			t.Errorf("the holding purge: %v", err)
		}

		h.done <- ids
	}()

	select {
	case <-inside:
	case ids := <-h.done:
		t.Fatalf("the holding purge returned %v without reaching its hook", ids)
	}

	t.Cleanup(func() { h.release() })

	return h
}

// release lets the held purge commit and returns what it purged.
func (h *heldPurge) release() []domain.OrgID {
	h.once.Do(func() {
		close(h.resume)
		h.ids = <-h.done
	})

	return h.ids
}

func TestMigrate_CreatesTheDefaultOrg(t *testing.T) {
	t.Parallel()

	s := New(newTestPool(t))

	o, err := s.Org(t.Context(), "default")
	if err != nil || o.ID != DefaultOrgID || o.Name != "Default" || o.Mark != "De" {
		t.Errorf("Org(default) = %+v, %v; want the default organization %s", o, err, DefaultOrgID)
	}
}

// goal_app reaches organizations and audit_log only through the transaction's organization:
// without one every statement fails, with one the other organizations' rows do not exist.
func TestRLS_TenancyTablesNeedTheOrg(t *testing.T) {
	t.Parallel()

	pool := newTestPool(t)
	s := New(pool)

	acme, err := s.CreateOrg(t.Context(), domain.LocalUser, newOrg("acme", "Acme"))
	if err != nil {
		t.Fatal(err)
	}

	for _, sql := range []string{"SELECT count(*) FROM organizations", "SELECT count(*) FROM audit_log"} {
		var n int
		if err := pool.QueryRow(t.Context(), sql).Scan(&n); err == nil {
			t.Errorf("%s without an org = %d; want an error", sql, n)
		}
	}

	var orgs, entries int

	err = asOrg(t, pool, acme.ID, "SELECT (SELECT count(*) FROM organizations), (SELECT count(*) FROM audit_log)", &orgs, &entries)
	if err != nil || orgs != 1 || entries != 1 {
		t.Errorf("as acme: %d orgs, %d audit entries, %v; want acme's own row and its org.created", orgs, entries, err)
	}

	if _, err := pool.Exec(t.Context(), "UPDATE audit_log SET action = 'org.renamed'"); err == nil {
		t.Error("goal_app updated the audit log; want permission denied")
	}

	if err := s.Append(domain.WithOrg(t.Context(), DefaultOrgID), domain.AuditEntry{
		Actor: domain.LocalUser, Action: andonPulled, TargetKind: "factory", TargetID: "f",
	}); err != nil {
		t.Fatal(err)
	}

	if err := asOrg(t, pool, acme.ID, "SELECT count(*) FROM audit_log", &entries); err != nil || entries != 1 {
		t.Errorf("acme sees %d audit entries, %v; want only its own", entries, err)
	}

	if _, err := s.Org(t.Context(), "acme"); err != nil {
		t.Errorf("Org(acme) through goal_org_by_ref = %v", err)
	}

	if err := s.Append(t.Context(), domain.AuditEntry{Actor: domain.LocalUser, Action: andonPulled}); !errors.Is(err, domain.ErrNoOrg) {
		t.Errorf("Append without an org = %v; want ErrNoOrg", err)
	}
}

func TestAudit_RecordsTheRequestID(t *testing.T) {
	t.Parallel()

	pool := newTestPool(t)
	s := New(pool)

	acme, err := s.CreateOrg(domain.WithRequestID(t.Context(), "req-acme-1"), domain.LocalUser, newOrg("acme", "Acme"))
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Append(domain.WithOrg(t.Context(), acme.ID), domain.AuditEntry{
		Actor: domain.Actor{Kind: domain.ActorSystem, ID: "engine", Name: "engine"}, Action: andonPulled, TargetKind: "factory", TargetID: "f",
	}); err != nil {
		t.Fatal(err)
	}

	var withID, without int

	err = asOrg(t, pool, acme.ID, `SELECT count(*) FILTER (WHERE request_id = 'req-acme-1'), count(*) FILTER (WHERE request_id IS NULL)
		FROM audit_log`, &withID, &without)
	if err != nil || withID != 1 || without != 1 {
		t.Errorf("entries with the request id %d, without one %d (%v); want org.created with it, the job's entry without", withID, without, err)
	}
}

// andonPulled is an audit action the tests append on their own.
const andonPulled = "andon.pulled"

// asOrg runs one query in a transaction bound to org, as the store does, and scans its row.
func asOrg(t *testing.T, pool *pgxpool.Pool, org domain.OrgID, sql string, dest ...any) error {
	t.Helper()

	return inOrgTx(t, pool, org, func(tx pgx.Tx) error { return tx.QueryRow(t.Context(), sql).Scan(dest...) })
}

// inOrgTx runs fn in a transaction bound to org, as the store does.
func inOrgTx(t *testing.T, pool *pgxpool.Pool, org domain.OrgID, fn func(pgx.Tx) error) error {
	t.Helper()

	return pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(t.Context(), "SELECT set_config('goal.org_id', $1, true)", string(org)); err != nil {
			return err
		}

		return fn(tx)
	})
}

func newOrg(slug, name string) ports.NewOrg { return ports.NewOrg{Slug: slug, Name: name} }
