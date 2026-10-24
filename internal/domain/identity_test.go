package domain

import "testing"

func TestFactoryRole_IncludesTheRolesBelow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		r, other FactoryRole
		want     bool
	}{
		{r: FactoryEditor, other: FactoryApprover, want: true},
		{r: FactoryEditor, other: FactoryViewer, want: true},
		{r: FactoryApprover, other: FactoryApprover, want: true},
		{r: FactoryApprover, other: FactoryEditor, want: false},
		{r: FactoryViewer, other: FactoryApprover, want: false},
		{r: "", other: FactoryViewer, want: false},
		{r: FactoryEditor, other: "", want: false},
		{r: "owner", other: FactoryViewer, want: false},
	}

	for _, tt := range tests {
		if got := tt.r.Includes(tt.other); got != tt.want {
			t.Errorf("%q.Includes(%q) = %v, want %v", tt.r, tt.other, got, tt.want)
		}
	}
}

func TestPrincipal_ActorRecordsWhoActed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		p    Principal
		want Actor
	}{
		{name: "local user", p: LocalPrincipal, want: LocalUser},
		{name: "user", p: Principal{Kind: PrincipalUser, User: "u1", Name: "Ada"}, want: Actor{Kind: ActorUser, ID: "u1", Name: "Ada"}},
		{name: "api key", p: Principal{Kind: PrincipalService, Key: "k1", Name: "ci"}, want: Actor{Kind: ActorService, ID: "k1", Name: "ci (API key)"}},
	}

	for _, tt := range tests {
		if got := tt.p.Actor(); got != tt.want {
			t.Errorf("%s: Actor() = %+v, want %+v", tt.name, got, tt.want)
		}
	}

	if a, err := ActorFromRef("service:k1"); err != nil || a.Kind != ActorService || a.ID != "k1" {
		t.Errorf(`ActorFromRef("service:k1") = %+v, %v; want the service actor`, a, err)
	}
}

func TestPrincipal_RoleOnAFactory(t *testing.T) {
	t.Parallel()

	const f, g FactoryID = "f", "g"

	member := Principal{Kind: PrincipalUser, User: "u", OrgRole: OrgMember, FactoryRoles: map[FactoryID]FactoryRole{f: FactoryApprover}}

	tests := []struct {
		name string
		p    Principal
		on   FactoryID
		want FactoryRole
	}{
		{name: "owner edits every factory", p: Principal{Kind: PrincipalUser, User: "u", OrgRole: OrgOwner}, on: g, want: FactoryEditor},
		{name: "admin edits every factory", p: Principal{Kind: PrincipalUser, User: "u", OrgRole: OrgAdmin}, on: g, want: FactoryEditor},
		{name: "member's own role", p: member, on: f, want: FactoryApprover},
		{name: "member without a role sees nothing", p: member, on: g, want: ""},
		{name: "non-member sees nothing", p: Principal{Kind: PrincipalUser, User: "u", FactoryRoles: member.FactoryRoles}, on: f, want: ""},
		{name: "key of every factory", p: Principal{Kind: PrincipalService, Key: "k", KeyCeiling: FactoryViewer}, on: g, want: FactoryViewer},
		{name: "key restricted to it", p: Principal{Kind: PrincipalService, Key: "k", KeyCeiling: FactoryEditor, KeyFactories: []FactoryID{f}}, on: f, want: FactoryEditor},
		{name: "key restricted elsewhere", p: Principal{Kind: PrincipalService, Key: "k", KeyCeiling: FactoryEditor, KeyFactories: []FactoryID{f}}, on: g, want: ""},
		{name: "key restricted to none", p: Principal{Kind: PrincipalService, Key: "k", KeyCeiling: FactoryEditor, KeyFactories: []FactoryID{}}, on: f, want: ""},
	}

	for _, tt := range tests {
		if got := tt.p.RoleOn(tt.on); got != tt.want {
			t.Errorf("%s: RoleOn(%s) = %q, want %q", tt.name, tt.on, got, tt.want)
		}
	}
}
