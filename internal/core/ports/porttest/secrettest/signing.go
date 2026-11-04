package secrettest

import (
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The secret the suite's webhook is signed by.
const hookSecret = "webhook-github-acme" //nolint:gosec // a secret's name, not a value

// signingTests are the cases of opening a webhook's secret (ADR 0040 decision 15).
var signingTests = []struct {
	name string
	test func(f *fixture)
}{
	{"OpenSigning_NoAuditNoUse", testOpenSigning},
	{"Patch_ReplaceKeepsAWebhookSecretsPreviousValue", testPreviousValue},
}

// OpenSigning opens the value without a secret.used entry or a last use; an unknown id is not
// found, and without a master key nothing opens.
func testOpenSigning(f *fixture) {
	acme := f.NewOrg(f.t)
	sec := f.create(acme, sentryIn(marker))

	got, err := f.s.OpenSigning(f.in(acme), sec.ID)
	if err != nil {
		f.t.Fatalf("OpenSigning: %v", err)
	}

	f.expect(got.Current == marker && got.Previous == "" && got.PreviousUntil == nil, "OpenSigning = %+v; want the value alone", got)
	f.expect(len(f.audit(acme, domain.AuditSecretUsed)) == 0, "OpenSigning wrote a secret.used entry")

	read, err := f.s.Get(f.in(acme), sentry)
	f.expect(err == nil && read.LastUsedAt == nil, "Get after OpenSigning = %+v, %v; want no last use", read, err)

	_, err = f.s.OpenSigning(f.in(acme), "00000000-0000-7000-8000-00000000ffff")
	f.refused("OpenSigning of no secret", err, ports.ErrNotFound)

	_, err = f.Secrets(f.Key()).OpenSigning(f.in(acme), sec.ID)
	f.refused("OpenSigning without a master key", err, ports.ErrNoMasterKey)

	_, err = f.s.OpenSigning(f.t.Context(), sec.ID)
	f.refused("OpenSigning without an org", err, domain.ErrNoOrg)
}

// Replacing a secret a webhook is signed by keeps the old value for 24 hours beside the new one;
// a secret no webhook uses keeps none.
func testPreviousValue(f *fixture) {
	acme := f.NewOrg(f.t)
	hook := f.create(acme, domain.NewSecret{Name: hookSecret, Value: marker})
	plain := f.create(acme, sentryIn(labsVal))
	f.SignWebhook(f.t, acme, hookSecret)

	v := replaced
	before := time.Now()

	for _, name := range []string{hookSecret, sentry} {
		if _, err := f.s.Patch(f.in(acme), name, domain.SecretPatch{Replace: &v}, editor); err != nil {
			f.t.Fatalf("Patch Replace %s: %v", name, err)
		}
	}

	got, err := f.s.OpenSigning(f.in(acme), hook.ID)
	if err != nil {
		f.t.Fatalf("OpenSigning: %v", err)
	}

	f.expect(got.Current == replaced && got.Previous == marker, "OpenSigning after a replace = %+v; want the new value and the old", got)

	if until := got.PreviousUntil; until == nil || until.Before(before.Add(domain.PreviousSecretKept-time.Minute)) ||
		until.After(time.Now().Add(domain.PreviousSecretKept+time.Minute)) {
		f.t.Errorf("previous until %v; want about 24 hours from now", until)
	}

	other, err := f.s.OpenSigning(f.in(acme), plain.ID)
	f.expect(err == nil && other.Current == replaced && other.Previous == "", "OpenSigning of a secret no webhook uses = %+v, %v", other, err)
	f.expect(f.resolve(acme, domain.SecretUse{Factory: factoryA, Agent: agent, Run: run}, sentry)[0].Value == replaced,
		"Resolve does not open the new value")
}
