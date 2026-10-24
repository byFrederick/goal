package domain

import (
	"errors"
	"testing"
)

func TestActorRef_RoundTripsEveryKind(t *testing.T) {
	t.Parallel()

	for _, a := range []Actor{
		LocalUser,
		{Kind: ActorAgent, ID: "investigator", Name: "investigator"},
		{Kind: ActorSystem, ID: "engine", Name: "engine"},
	} {
		got, err := ActorFromRef(a.Ref())
		if err != nil || got != a {
			t.Errorf("ActorFromRef(%q) = %+v, %v; want %+v", a.Ref(), got, err, a)
		}
	}

	if LocalUser.Ref() != "user:local" {
		t.Errorf("LocalUser.Ref() = %q; want user:local", LocalUser.Ref())
	}
}

func TestActorFromRef_MalformedIsAnError(t *testing.T) {
	t.Parallel()

	for _, ref := range []string{"", "local", "user:", ":local", "robot:r2", "User:local"} {
		if a, err := ActorFromRef(ref); err == nil {
			t.Errorf("ActorFromRef(%q) = %+v; want an error", ref, a)
		}
	}
}

func TestCheckSlug_FormatAndReservedWords(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{"acme", "example-labs", "a1b", "0rg"} {
		if err := CheckSlug(ok); err != nil {
			t.Errorf("CheckSlug(%q) = %v; want nil", ok, err)
		}
	}

	for _, bad := range []string{
		"", "ab", "Acme", "acme-", "-acme", "ac_me", "settings", "default", "me", "v1",
		"a2345678901234567890123456789012345678901",
	} {
		if err := CheckSlug(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("CheckSlug(%q) = %v; want ErrInvalid", bad, err)
		}
	}
}

func TestDefaultMark_InitialsOrFirstLetters(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]string{
		"Default": "De", "acme": "Ac", "Example Labs": "EL", "  north  wind  co ": "NW", "Ω": "Ω", "": "",
	} {
		if got := DefaultMark(name); got != want {
			t.Errorf("DefaultMark(%q) = %q; want %q", name, got, want)
		}
	}
}

func TestOrgContext_CarriesTheOrgAndTheActor(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	if _, ok := OrgFrom(ctx); ok {
		t.Error("a bare context has an org")
	}

	if _, ok := ActorFrom(ctx); ok {
		t.Error("a bare context has an actor")
	}

	ctx = WithActor(WithOrg(ctx, "019a1c00-0000-7000-8000-000000000001"), LocalUser)

	if org, ok := OrgFrom(ctx); !ok || org != "019a1c00-0000-7000-8000-000000000001" {
		t.Errorf("OrgFrom = %q, %v", org, ok)
	}

	if a, ok := ActorFrom(ctx); !ok || a != LocalUser {
		t.Errorf("ActorFrom = %+v, %v", a, ok)
	}

	if _, ok := OrgFrom(WithOrg(ctx, "")); ok {
		t.Error("an empty org counts as an org")
	}
}
