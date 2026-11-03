package domain

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// secretMarker is a value no test output, error or String may ever contain.
const secretMarker = "sk-secret-marker-value" //nolint:gosec // a made-up value

func TestCheckSecret(t *testing.T) {
	t.Parallel()

	expires := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	factory := FactoryID("00000000-0000-7000-8000-0000000000f1")
	valid := Secret{Name: testCredName, Source: SecretSourceValue, Scope: SecretScope{Factories: []FactoryID{factory}}}

	tests := []struct {
		name string
		edit func(s *Secret)
		ok   bool
	}{
		{"valid", func(*Secret) {}, true},
		{"all factories", func(s *Secret) { s.Scope = SecretScope{All: true} }, true},
		{"no factories", func(s *Secret) { s.Scope = SecretScope{} }, true},
		{"the shortest name", func(s *Secret) { s.Name = "abc" }, true},
		{"the longest name", func(s *Secret) { s.Name = "a" + strings.Repeat("b", 39) }, true},
		{"a name a credential could not have", func(s *Secret) { s.Name = "goal-token" }, true},
		{"a description of 500 characters", func(s *Secret) { s.Description = strings.Repeat("é", MaxSecretDescription) }, true},
		{"expiry with a reminder of 1 day", func(s *Secret) { s.ExpiresAt, s.RemindDays = &expires, 1 }, true},
		{"expiry with a reminder of 90 days", func(s *Secret) { s.ExpiresAt, s.RemindDays = &expires, 90 }, true},
		{"expiry without a reminder", func(s *Secret) { s.ExpiresAt = &expires }, true},
		{"bad name", func(s *Secret) { s.Name = "Sentry_Token" }, false},
		{"short name", func(s *Secret) { s.Name = "ab" }, false},
		{"long name", func(s *Secret) { s.Name = "a" + strings.Repeat("b", 40) }, false},
		{"name ending with a dash", func(s *Secret) { s.Name = "sentry-" }, false},
		{"name starting with a digit", func(s *Secret) { s.Name = "1sentry" }, false},
		{"no source", func(s *Secret) { s.Source = "" }, false},
		{"unknown source", func(s *Secret) { s.Source = "1password" }, false},
		{"long description", func(s *Secret) { s.Description = strings.Repeat("d", MaxSecretDescription+1) }, false},
		{"reminder without expiry", func(s *Secret) { s.RemindDays = 7 }, false},
		{"negative reminder", func(s *Secret) { s.ExpiresAt, s.RemindDays = &expires, -1 }, false},
		{"reminder of 91 days", func(s *Secret) { s.ExpiresAt, s.RemindDays = &expires, 91 }, false},
		{"all and a list", func(s *Secret) { s.Scope.All = true }, false},
		{"empty factory id", func(s *Secret) { s.Scope.Factories = []FactoryID{""} }, false},
		{"factory twice", func(s *Secret) { s.Scope.Factories = []FactoryID{factory, factory} }, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := valid
			s.Scope.Factories = append([]FactoryID(nil), valid.Scope.Factories...)
			tc.edit(&s)

			err := CheckSecret(s)
			if tc.ok && err != nil {
				t.Errorf("CheckSecret = %v, want nil", err)
			}

			if !tc.ok && !errors.Is(err, ErrInvalid) {
				t.Errorf("CheckSecret = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestCheckNewSecret(t *testing.T) {
	t.Parallel()

	n := NewSecret{Name: testCredName, Value: secretMarker}
	if err := n.Check(); err != nil {
		t.Fatalf("Check = %v, want nil", err)
	}

	if got := n.Secret(); got.Source != SecretSourceValue || got.Name != testCredName {
		t.Errorf("Secret = %+v; want the name and the source value", got)
	}

	for name, edit := range map[string]func(n *NewSecret){
		"no value":   func(n *NewSecret) { n.Value = "" },
		"long value": func(n *NewSecret) { n.Value = strings.Repeat("v", MaxSecretValue+1) },
		"bad name":   func(n *NewSecret) { n.Name = "No" },
	} {
		bad := n
		edit(&bad)

		err := bad.Check()
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: Check = %v, want ErrInvalid", name, err)
		}

		if err != nil && strings.Contains(err.Error(), secretMarker) {
			t.Errorf("%s: the error shows the value: %v", name, err)
		}
	}
}

func TestSecretPatch_Apply(t *testing.T) {
	t.Parallel()

	expires := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	factory := FactoryID("00000000-0000-7000-8000-0000000000f1")
	s := Secret{Name: "old-name", Source: SecretSourceValue, Description: "d", Scope: SecretScope{Factories: []FactoryID{factory}}}

	if !(SecretPatch{}).Empty() {
		t.Error("the zero patch is not empty")
	}

	name, desc := "new-name", "new"
	p := SecretPatch{Name: &name, Description: &desc, Scope: &SecretScope{All: true}, Expiry: &SecretExpiry{At: &expires, RemindDays: 3}}

	got := p.Apply(s)
	if got.Name != name || got.Description != desc || !got.Scope.All || len(got.Scope.Factories) != 0 ||
		got.ExpiresAt == nil || !got.ExpiresAt.Equal(expires) || got.RemindDays != 3 || got.Source != SecretSourceValue {
		t.Fatalf("Apply = %+v", got)
	}

	if s.Name != "old-name" || s.Scope.All {
		t.Error("Apply changed its argument")
	}

	if (SecretPatch{Name: &name}).ChangesMetadata() != true || (SecretPatch{Replace: &desc}).ChangesMetadata() {
		t.Error("ChangesMetadata is every change but the value")
	}

	cleared := SecretPatch{Expiry: &SecretExpiry{}}.Apply(got)
	if cleared.ExpiresAt != nil || cleared.RemindDays != 0 {
		t.Errorf("an empty Expiry does not clear the expiry: %+v", cleared)
	}

	replace := secretMarker
	if (SecretPatch{Replace: &replace}).Empty() {
		t.Error("a patch with Replace is empty")
	}

	empty := ""
	if err := (SecretPatch{Replace: &empty}).CheckValue(); !errors.Is(err, ErrInvalid) {
		t.Errorf("an empty Replace: %v, want ErrInvalid", err)
	}
}

// A secret's sealed value is bound to the same bytes a credential's was: the migration that
// renamed credentials to secrets kept every row's id and could not reseal it, so any other bytes
// would leave every migrated value sealed shut.
func TestSecretAAD_MatchesCredentialAAD(t *testing.T) {
	t.Parallel()

	org, id := OrgID("00000000-0000-7000-8000-00000000000a"), "00000000-0000-7000-8000-0000000000c1"

	got, want := SecretAAD(org, SecretID(id), "k1"), CredentialAAD(org, CredentialID(id), "k1")
	if !bytes.Equal(got, want) {
		t.Fatalf("SecretAAD = %q, want CredentialAAD's %q", got, want)
	}

	for what, other := range map[string][]byte{
		"another organization": SecretAAD("00000000-0000-7000-8000-00000000000b", SecretID(id), "k1"),
		"another id":           SecretAAD(org, "00000000-0000-7000-8000-0000000000c2", "k1"),
		"another key id":       SecretAAD(org, SecretID(id), "k2"),
	} {
		if bytes.Equal(got, other) {
			t.Errorf("%s binds the same associated data", what)
		}
	}
}

// The value is in no Secret, and no String of a type that carries it shows it.
func TestSecretValueHidden(t *testing.T) {
	t.Parallel()

	v := secretMarker
	shown := []any{
		NewSecret{Name: testCredName, Value: secretMarker},
		SecretPatch{Replace: &v},
		ResolvedSecret{Secret: Secret{Name: testCredName}, Value: secretMarker},
		[]ResolvedSecret{{Secret: Secret{Name: testCredName}, Value: secretMarker}},
	}

	for _, x := range shown {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
			if s := fmt.Sprintf(verb, x); strings.Contains(s, secretMarker) {
				t.Errorf("%T with %s shows the value: %s", x, verb, s)
			}
		}
	}

	if s := fmt.Sprint(SecretPatch{Replace: &v}); !strings.Contains(s, "replace") {
		t.Errorf("SecretPatch's String does not say a value is replaced: %s", s)
	}
}
