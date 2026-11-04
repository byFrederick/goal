package porttest

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.KeptTests = (*Store)(nil)

// KeepTest implements ports.KeptTests.
func (s *Store) KeepTest(ctx context.Context, n domain.NewKeptTest) (domain.KeptTest, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.KeptTest{}, err
	}

	if err := n.Check(); err != nil {
		return domain.KeptTest{}, fmt.Errorf("keep test: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	it, ok := s.visible(n.ItemID, org)
	if !ok || it.FactoryID != n.FactoryID {
		return domain.KeptTest{}, fmt.Errorf("keep test: item %s: %w", n.ItemID, ports.ErrNotFound)
	}

	if slices.ContainsFunc(s.jonah.keptTests, func(r jonahRow[domain.KeptTest]) bool {
		return r.org == org && r.row.ItemID == n.ItemID && r.row.Station == n.Station
	}) {
		return domain.KeptTest{}, fmt.Errorf("keep test: item %s at %s: %w", n.ItemID, n.Station, ports.ErrConflict)
	}

	k := domain.KeptTest{
		ID: domain.KeptTestID(s.newID()), FactoryID: n.FactoryID, ItemID: n.ItemID, Station: n.Station, KeptBy: n.KeptBy, KeptAt: n.KeptAt,
	}
	s.jonah.keptTests = append(s.jonah.keptTests, jonahRow[domain.KeptTest]{org: org, row: k})

	return s.keptTestOut(k), nil
}

// keptTestOut is k with its item's number and title. Callers hold s.mu.
func (s *Store) keptTestOut(k domain.KeptTest) domain.KeptTest {
	if it, ok := s.items[k.ItemID]; ok {
		k.ItemNumber, k.ItemTitle = it.Number, it.Title
	}

	return k
}

// KeptTests implements ports.KeptTests.
func (s *Store) KeptTests(ctx context.Context, factory domain.FactoryID, station string) ([]domain.KeptTest, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	out := []domain.KeptTest{}

	for _, r := range s.jonah.keptTests {
		if r.org == org && r.row.FactoryID == factory && (station == "" || r.row.Station == station) {
			out = append(out, s.keptTestOut(r.row))
		}
	}

	// Newest first, ties by id descending, as the adapter orders them.
	slices.SortStableFunc(out, func(a, b domain.KeptTest) int {
		if c := b.KeptAt.Compare(a.KeptAt); c != 0 {
			return c
		}

		return strings.Compare(string(b.ID), string(a.ID))
	})

	return out, nil
}

// RemoveKeptTest implements ports.KeptTests.
func (s *Store) RemoveKeptTest(ctx context.Context, factory domain.FactoryID, id domain.KeptTestID) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	n := len(s.jonah.keptTests)
	s.jonah.keptTests = slices.DeleteFunc(s.jonah.keptTests, func(r jonahRow[domain.KeptTest]) bool {
		return r.org == org && r.row.FactoryID == factory && r.row.ID == id
	})

	if len(s.jonah.keptTests) == n {
		return fmt.Errorf("remove kept test %s: %w", id, ports.ErrNotFound)
	}

	return nil
}
