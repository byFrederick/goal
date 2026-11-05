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
