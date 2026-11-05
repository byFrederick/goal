package tenancytest

import (
	"reflect"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The added hosts the code host tests use.
var (
	ghe    = domain.CodeHost{Host: "ghe.example.com", Kind: domain.CodeHostGitHub}
	gitlab = domain.CodeHost{Host: "gitlab.example.com", Kind: domain.CodeHostGitLab}
)

func (f *fixture) codeHosts(o domain.Org) []domain.CodeHost {
	f.t.Helper()

	got, err := f.CodeHosts.CodeHosts(f.in(o))
	if err != nil {
		f.t.Fatalf("CodeHosts: %v", err)
	}

	return got
}

func (f *fixture) setCodeHosts(o domain.Org, hosts ...domain.CodeHost) []domain.CodeHost {
	f.t.Helper()

	got, err := f.CodeHosts.SetCodeHosts(f.in(o), domain.LocalUser, hosts)
	if err != nil {
		f.t.Fatalf("SetCodeHosts: %v", err)
	}

	return got
}

// An organization's added hosts are replaced as a whole and read back sorted by host; a kind
// changes in place, and setting what is stored appends no audit entry. Another organization
// never sees them.
func testCodeHosts(f *fixture) {
	a := f.create(acme, acmeName)
	b := f.create(labs, labsName)

	if got := f.codeHosts(a); len(got) != 0 {
		f.t.Fatalf("hosts before = %+v; want none", got)
	}

	want := []domain.CodeHost{ghe, gitlab}
	if got := f.setCodeHosts(a, gitlab, ghe); !reflect.DeepEqual(got, want) {
		f.t.Errorf("SetCodeHosts = %+v; want %+v", got, want)
	}

	if got := f.codeHosts(a); !reflect.DeepEqual(got, want) {
		f.t.Errorf("CodeHosts = %+v; want %+v", got, want)
	}

	f.setCodeHosts(a, ghe, gitlab) // the same hosts: no change

	asGitLab := domain.CodeHost{Host: ghe.Host, Kind: domain.CodeHostGitLab}
	if got := f.setCodeHosts(a, asGitLab); !reflect.DeepEqual(got, []domain.CodeHost{asGitLab}) {
		f.t.Errorf("after replacing = %+v; want only %+v", got, asGitLab)
	}

	if got := f.codeHosts(b); len(got) != 0 {
		f.t.Errorf("another organization reads %+v; want none", got)
	}

	f.setCodeHosts(b, gitlab)

	if got := f.codeHosts(a); !reflect.DeepEqual(got, []domain.CodeHost{asGitLab}) {
		f.t.Errorf("after another organization's change = %+v; want only %+v", got, asGitLab)
	}

	entries := f.log(a, ports.AuditQuery{Action: domain.AuditOrgCodeHostsSet})
	if len(entries) != 2 {
		f.t.Fatalf("org.code_hosts_set entries = %d; want 2, none for the unchanged set", len(entries))
	}

	last := entries[0]
	if last.TargetKind != domain.AuditTargetOrg || last.TargetID != string(a.ID) || last.Actor.Ref() != domain.LocalUser.Ref() {
		f.t.Errorf("last entry = %+v; want the organization, by the local user", last)
	}

	wantData := map[string]any{
		"hosts": []any{map[string]any{"host": ghe.Host, "kind": "gitlab"}},
		"from":  []any{map[string]any{"host": ghe.Host, "kind": "github"}, map[string]any{"host": gitlab.Host, "kind": "gitlab"}},
	}
	if !reflect.DeepEqual(last.Data, wantData) {
		f.t.Errorf("last entry's data = %v; want %v", last.Data, wantData)
	}

	if got := f.setCodeHosts(a); len(got) != 0 {
		f.t.Errorf("after clearing = %+v; want none", got)
	}
}

func (f *fixture) seedCodeHosts(o domain.Org, hosts ...domain.CodeHost) bool {
	f.t.Helper()

	seeded, err := f.CodeHosts.SeedCodeHosts(f.in(o), domain.LocalUser, hosts)
	if err != nil {
		f.t.Fatalf("SeedCodeHosts: %v", err)
	}

	return seeded
}

// An organization's hosts are seeded once: the first call adds the hosts it lacks, keeping the
// kind of a host already listed, with one audit entry; a later call adds nothing, so a host an
// owner removed after the seed stays removed. Another organization's list is untouched.
func testCodeHostsSeededOnce(f *fixture) {
	a := f.create(acme, acmeName)
	b := f.create(labs, labsName)

	f.setCodeHosts(b, gitlab)
	f.setCodeHosts(a, ghe)
	f.checkFirstSeed(a)

	f.setCodeHosts(a, ghe) // an owner removes the seeded host

	if f.seedCodeHosts(a, gitlab) {
		f.t.Error("second SeedCodeHosts seeded; want nothing")
	}

	if got := f.codeHosts(a); !reflect.DeepEqual(got, []domain.CodeHost{ghe}) {
		f.t.Errorf("after the second seed = %+v; want only %+v, the removed host not back", got, ghe)
	}

	if n := len(f.log(a, ports.AuditQuery{Action: domain.AuditOrgCodeHostsSet})); n != 3 {
		f.t.Errorf("org.code_hosts_set entries = %d; want 3, none for the second seed", n)
	}

	// The first seed of an organization with nothing to add still counts as its seed.
	if !f.seedCodeHosts(b) || f.seedCodeHosts(b, ghe) {
		f.t.Error("an empty first seed did not count as the seed")
	}

	if got := f.codeHosts(b); !reflect.DeepEqual(got, []domain.CodeHost{gitlab}) {
		f.t.Errorf("another organization's hosts = %+v; want only %+v", got, gitlab)
	}
}

// checkFirstSeed seeds a, which lists ghe, with ghe as GitLab and gitlab: gitlab is added, ghe
// keeps its kind, and one entry by the local user says so.
func (f *fixture) checkFirstSeed(a domain.Org) {
	f.t.Helper()

	asGitLab := domain.CodeHost{Host: ghe.Host, Kind: domain.CodeHostGitLab}
	if !f.seedCodeHosts(a, asGitLab, gitlab) {
		f.t.Fatal("first SeedCodeHosts seeded nothing; want it to seed")
	}

	want := []domain.CodeHost{ghe, gitlab}
	if got := f.codeHosts(a); !reflect.DeepEqual(got, want) {
		f.t.Errorf("after the seed = %+v; want %+v, the listed host keeping its kind", got, want)
	}

	entries := f.log(a, ports.AuditQuery{Action: domain.AuditOrgCodeHostsSet})
	if len(entries) != 2 {
		f.t.Fatalf("org.code_hosts_set entries = %d; want 2, the set and the seed", len(entries))
	}

	wantData := domain.CodeHostsAuditData(want, []domain.CodeHost{ghe})
	if seed := entries[0]; !reflect.DeepEqual(seed.Data, wantData) || seed.Actor.Ref() != domain.LocalUser.Ref() || seed.TargetID != string(a.ID) {
		f.t.Errorf("seed entry = %+v; want data %v by the local user on the organization", seed, wantData)
	}
}
