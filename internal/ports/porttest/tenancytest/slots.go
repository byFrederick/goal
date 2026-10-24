package tenancytest

import (
	"errors"
	"maps"
	"reflect"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

func (f *fixture) factorySlots(o domain.Org) map[domain.FactoryID]int {
	f.t.Helper()

	got, err := f.Tenancy.FactorySlots(f.in(o))
	if err != nil {
		f.t.Fatalf("FactorySlots: %v", err)
	}

	return got
}

func testSlotCaps(f *fixture) {
	o := f.create(acme, acmeName)
	small, big := f.NewFactory(f.t, o.ID), f.NewFactory(f.t, o.ID)

	if got, want := f.factorySlots(o), map[domain.FactoryID]int{small: 0, big: 0}; !maps.Equal(got, want) {
		f.t.Errorf("FactorySlots of a new org = %v; want %v, no caps", got, want)
	}

	// Per-factory caps may add up to more than the organization's.
	got := f.update(o, domain.LocalUser, ports.OrgUpdate{MaxSlots: new(6), FactorySlots: map[domain.FactoryID]int{small: 2, big: 5}})

	want := o
	want.MaxSlots = 6

	if got != want {
		f.t.Errorf("UpdateOrg max slots = %+v; want %+v", got, want)
	}

	f.found(acme, want)

	if s, ok := f.listed(o); !ok || s.MaxSlots != 6 {
		f.t.Errorf("Orgs lists %+v; want max slots 6", s)
	}

	if got, want := f.factorySlots(o), map[domain.FactoryID]int{small: 2, big: 5}; !maps.Equal(got, want) {
		f.t.Errorf("FactorySlots = %v; want %v", got, want)
	}

	// Unchanged caps are no change; 0 clears a cap.
	f.update(o, domain.LocalUser, ports.OrgUpdate{MaxSlots: new(6), FactorySlots: map[domain.FactoryID]int{small: 2}})
	f.update(o, domain.LocalUser, ports.OrgUpdate{MaxSlots: new(0), FactorySlots: map[domain.FactoryID]int{big: 0}})

	f.found(acme, o)

	if got, want := f.factorySlots(o), map[domain.FactoryID]int{small: 2, big: 0}; !maps.Equal(got, want) {
		f.t.Errorf("FactorySlots after clearing = %v; want %v", got, want)
	}

	entries := f.log(o, ports.AuditQuery{Action: domain.AuditOrgUpdated})
	data := []map[string]any{
		{"max_slots": change(6, nil), "factory_slots": map[string]any{string(big): change(5, nil)}},
		{"max_slots": change(nil, 6), "factory_slots": map[string]any{string(small): change(nil, 2), string(big): change(nil, 5)}},
	}

	if len(entries) != len(data) {
		f.t.Fatalf("org.updated entries = %+v; want %d, one per change", entries, len(data))
	}

	for i, d := range data {
		if !reflect.DeepEqual(entries[i].Data, d) {
			f.t.Errorf("org.updated %d data = %v; want %v", i, entries[i].Data, d)
		}
	}
}

// change is a changed field in audit data, {from, to}, with a cap as JSON reads it back (a
// float64) and no cap as nil.
func change(from, to any) map[string]any {
	asJSON := func(v any) any {
		if n, ok := v.(int); ok {
			return float64(n)
		}

		return v
	}

	return map[string]any{"from": asJSON(from), "to": asJSON(to)}
}

func testSlotCapRules(f *fixture) {
	o := f.create(acme, acmeName)
	mine := f.NewFactory(f.t, o.ID)
	theirs := f.NewFactory(f.t, f.create(labs, labsName).ID)

	for what, u := range map[string]ports.OrgUpdate{
		"negative org cap":           {MaxSlots: new(-1)},
		"org cap over the limit":     {MaxSlots: new(domain.MaxSlotCap + 1)},
		"factory cap over the limit": {FactorySlots: map[domain.FactoryID]int{mine: domain.MaxSlotCap + 1}},
		"factory of another org":     {MaxSlots: new(3), FactorySlots: map[domain.FactoryID]int{theirs: 1}},
		"factory that is no factory": {FactorySlots: map[domain.FactoryID]int{missingID: 1}},
	} {
		_, err := f.Tenancy.UpdateOrg(f.in(o), domain.LocalUser, u)
		f.refused("UpdateOrg with a "+what, err, domain.ErrInvalid)
	}

	f.found(acme, o)

	if got, want := f.factorySlots(o), map[domain.FactoryID]int{mine: 0}; !maps.Equal(got, want) {
		f.t.Errorf("FactorySlots after refused updates = %v; want %v", got, want)
	}

	if got := f.log(o, ports.AuditQuery{Action: domain.AuditOrgUpdated}); len(got) != 0 {
		f.t.Errorf("refused updates appended %d entries", len(got))
	}

	if _, err := f.Tenancy.FactorySlots(f.t.Context()); !errors.Is(err, domain.ErrNoOrg) {
		f.t.Errorf("FactorySlots without an org = %v; want ErrNoOrg", err)
	}
}
