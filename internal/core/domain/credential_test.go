package domain

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// credMarker is a value no test output, error or String may ever contain.
const credMarker = "sk-credential-marker-value"

func TestCheckCredential(t *testing.T) {
	t.Parallel()

	expires := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	factory := FactoryID("00000000-0000-7000-8000-0000000000f1")
	valid := Credential{Name: "sentry-token", Type: CredentialAPIKey, Scope: CredentialScope{Factories: []FactoryID{factory}}}

	tests := []struct {
		name string
		edit func(c *Credential)
		ok   bool
	}{
		{"valid", func(*Credential) {}, true},
		{"every type", func(c *Credential) { c.Type = CredentialAppKey }, true},
		{"all factories", func(c *Credential) { c.Scope = CredentialScope{All: true} }, true},
		{"no factories", func(c *Credential) { c.Scope = CredentialScope{} }, true},
		{"expiry with a reminder", func(c *Credential) { c.ExpiresAt, c.RemindDays = &expires, 14 }, true},
		{"agents of a factory in scope", func(c *Credential) { c.Agents = map[FactoryID][]string{factory: {"backend"}} }, true},
		{"bad name", func(c *Credential) { c.Name = "Sentry_Token" }, false},
		{"short name", func(c *Credential) { c.Name = "ab" }, false},
		{"reserved env name", func(c *Credential) { c.Name = "goal-token" }, false},
		{"unknown type", func(c *Credential) { c.Type = "password" }, false},
		{"empty type", func(c *Credential) { c.Type = "" }, false},
		{"long description", func(c *Credential) { c.Description = strings.Repeat("d", MaxConnectionDescription+1) }, false},
		{"reminder without expiry", func(c *Credential) { c.RemindDays = 7 }, false},
		{"negative reminder", func(c *Credential) { c.ExpiresAt, c.RemindDays = &expires, -1 }, false},
		{"all and a list", func(c *Credential) { c.Scope.All = true }, false},
		{"empty factory id", func(c *Credential) { c.Scope.Factories = []FactoryID{""} }, false},
		{"factory twice", func(c *Credential) { c.Scope.Factories = []FactoryID{factory, factory} }, false},
		{"agents of a factory out of scope", func(c *Credential) { c.Agents = map[FactoryID][]string{"other": {"backend"}} }, false},
		{"empty agent name", func(c *Credential) { c.Agents = map[FactoryID][]string{factory: {""}} }, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := valid
			c.Scope.Factories = append([]FactoryID(nil), valid.Scope.Factories...)
			tc.edit(&c)

			err := CheckCredential(c)
			if tc.ok && err != nil {
				t.Fatalf("CheckCredential: %v", err)
			}

			if !tc.ok && !errors.Is(err, ErrInvalid) {
				t.Fatalf("CheckCredential = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestCheckNewCredential(t *testing.T) {
	t.Parallel()

	n := NewCredential{Name: "sentry-token", Type: CredentialAPIKey, Value: credMarker}
	if err := n.Check(); err != nil {
		t.Fatalf("Check: %v", err)
	}

	for name, edit := range map[string]func(n *NewCredential){
		"no value":   func(n *NewCredential) { n.Value = "" },
		"huge value": func(n *NewCredential) { n.Value = strings.Repeat("v", MaxCredentialValue+1) },
		"bad name":   func(n *NewCredential) { n.Name = "-x-" },
	} {
		bad := n
		edit(&bad)

		err := bad.Check()
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: Check = %v, want ErrInvalid", name, err)
		}

		if err != nil && strings.Contains(err.Error(), credMarker) {
			t.Errorf("%s: the error shows the value: %v", name, err)
		}
	}
}

func TestCredentialPatch(t *testing.T) {
	t.Parallel()

	expires := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	c := Credential{Name: "old-name", Type: CredentialAPIKey, Description: "d", RemindDays: 0}

	if !(CredentialPatch{}).Empty() {
		t.Error("the zero patch is not empty")
	}

	name, typ, desc := "new-name", CredentialSSHKey, "new"
	p := CredentialPatch{
		Name: &name, Type: &typ, Description: &desc, Scope: &CredentialScope{All: true},
		Agents: map[FactoryID][]string{"f": {"a"}}, Expiry: &CredentialExpiry{At: &expires, RemindDays: 3},
	}

	got := p.Apply(c)
	if got.Name != name || got.Type != typ || got.Description != desc || !got.Scope.All ||
		len(got.Agents["f"]) != 1 || got.ExpiresAt == nil || !got.ExpiresAt.Equal(expires) || got.RemindDays != 3 {
		t.Fatalf("Apply = %+v", got)
	}

	if c.Name != "old-name" {
		t.Error("Apply changed its argument")
	}

	cleared := CredentialPatch{Expiry: &CredentialExpiry{}}.Apply(got)
	if cleared.ExpiresAt != nil || cleared.RemindDays != 0 {
		t.Errorf("an empty Expiry does not clear the expiry: %+v", cleared)
	}

	replace := credMarker
	if (CredentialPatch{Replace: &replace}).Empty() {
		t.Error("a patch with Replace is empty")
	}

	empty := ""
	if err := (CredentialPatch{Replace: &empty}).CheckValue(); !errors.Is(err, ErrInvalid) {
		t.Errorf("an empty Replace: %v, want ErrInvalid", err)
	}
}

func TestCredentialScopeAllows(t *testing.T) {
	t.Parallel()

	f1, f2 := FactoryID("f1"), FactoryID("f2")
	c := Credential{Scope: CredentialScope{Factories: []FactoryID{f1}}, Agents: map[FactoryID][]string{f1: {"backend"}}}

	if !c.Scope.Allows(f1) || c.Scope.Allows(f2) {
		t.Error("a list scope allows exactly its factories")
	}

	if !(CredentialScope{All: true}).Allows(f2) {
		t.Error("an All scope allows any factory")
	}

	if (CredentialScope{}).Allows(f1) {
		t.Error("an empty scope allows a factory")
	}

	if !c.ForAgent(f1, "backend") || c.ForAgent(f1, "frontend") || c.ForAgent(f2, "backend") {
		t.Error("ForAgent is the agent list of a factory in scope")
	}
}

func TestCredentialEnvName(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]string{
		"cloudflare-token-2027": "CLOUDFLARE_TOKEN_2027",
		"sentry-token":          "SENTRY_TOKEN",
		"abc":                   "ABC",
	} {
		got, err := CredentialEnvName(name)
		if err != nil || got != want {
			t.Errorf("CredentialEnvName(%q) = %q, %v, want %q", name, got, err, want)
		}
	}

	for _, name := range []string{"goal-x", "ld-preload", "home", "path", "mise-env", "claude-config-dir", "Bad", "a"} {
		if _, err := CredentialEnvName(name); !errors.Is(err, ErrInvalid) {
			t.Errorf("CredentialEnvName(%q) = %v, want ErrInvalid", name, err)
		}
	}
}

func TestCredentialRef(t *testing.T) {
	t.Parallel()

	name, ok, err := CredentialRef("{{sentry-token}}")
	if err != nil || !ok || name != "sentry-token" {
		t.Fatalf("CredentialRef({{sentry-token}}) = %q, %v, %v", name, ok, err)
	}

	for _, v := range []string{credMarker, "", "a { b } c", "{{ not closed", "closed }}"} {
		name, ok, err := CredentialRef(v)
		if err != nil || ok || name != "" {
			t.Errorf("CredentialRef(%q) = %q, %v, %v, want no reference", v, name, ok, err)
		}
	}

	for _, v := range []string{"Bearer {{t}}", "{{a}}{{b}}", "{{Bad_Name}}", "{{ sentry-token }}", "{{}}", credMarker + "{{sentry-token}}"} {
		_, ok, err := CredentialRef(v)
		if ok || !errors.Is(err, ErrInvalid) {
			t.Errorf("CredentialRef(%q) = %v, %v, want ErrInvalid", v, ok, err)
		}

		if err != nil && strings.Contains(err.Error(), credMarker) {
			t.Errorf("CredentialRef's error shows the value: %v", err)
		}
	}
}

func TestParseCredentialImport(t *testing.T) {
	t.Parallel()

	ok := []struct {
		name, format, text string
		want               map[string]string
	}{
		{"env", CredentialImportEnv, "SENTRY_TOKEN=x\n", map[string]string{"sentry-token": "x"}},
		{"env comments and blanks", CredentialImportEnv, "# tokens\n\nSENTRY_TOKEN=a=b\r\n  # more\nAPI_KEY = v \n", map[string]string{"sentry-token": "a=b", "api-key": "v"}},
		{"env quotes", CredentialImportEnv, "A_KEY=\"q v\"\nB_KEY='s'\nC_KEY=\"'x'\"\n", map[string]string{"a-key": "q v", "b-key": "s", "c-key": "'x'"}},
		{"env empty", CredentialImportEnv, "\n# nothing\n", map[string]string{}},
		{"yaml", CredentialImportYAML, "credentials:\n  sentry-token: x\n  api-key: \"a\\\"b\"\n", map[string]string{"sentry-token": "x", "api-key": `a"b`}},
		{"yaml comments and quotes", CredentialImportYAML, "---\n# top\ncredentials: # all\n\n    one-key: 'it''s' # c\n    two-key: plain value # c\n    three-key: a#b\n",
			map[string]string{"one-key": "it's", "two-key": "plain value", "three-key": "a#b"}},
		{"yaml no entries", CredentialImportYAML, "credentials:\n", map[string]string{}},
	}

	for _, tc := range ok {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseCredentialImport(tc.format, tc.text)
			if err != nil {
				t.Fatalf("ParseCredentialImport: %v", err)
			}

			if len(got) != len(tc.want) {
				t.Fatalf("ParseCredentialImport = %d entries, want %d", len(got), len(tc.want))
			}

			for _, e := range got {
				if v, ok := tc.want[e.Name]; !ok || v != e.Value {
					t.Errorf("entry %s has the wrong value or is not wanted", e.Name)
				}
			}
		})
	}

	bad := []struct{ name, format, text string }{
		{"unknown format", "toml", "a = '" + credMarker + "'"},
		{"env duplicate", CredentialImportEnv, "SENTRY_TOKEN=" + credMarker + "\nsentry_token=" + credMarker},
		{"env invalid name", CredentialImportEnv, "AB=" + credMarker},
		{"env bad key", CredentialImportEnv, "SENTRY-TOKEN=" + credMarker},
		{"env reserved", CredentialImportEnv, "GOAL_TOKEN=" + credMarker},
		{"env no equals", CredentialImportEnv, credMarker},
		{"env no value", CredentialImportEnv, "SENTRY_TOKEN="},
		{"env unclosed quote", CredentialImportEnv, "SENTRY_TOKEN=\"" + credMarker},
		{"yaml no credentials key", CredentialImportYAML, "other:\n  sentry-token: " + credMarker},
		{"yaml other top-level key", CredentialImportYAML, "credentials:\n  sentry-token: " + credMarker + "\nother: " + credMarker},
		{"yaml duplicate", CredentialImportYAML, "credentials:\n  sentry-token: " + credMarker + "\n  sentry-token: " + credMarker},
		{"yaml invalid name", CredentialImportYAML, "credentials:\n  Sentry: " + credMarker},
		{"yaml reserved", CredentialImportYAML, "credentials:\n  ld-preload: " + credMarker},
		{"yaml block scalar", CredentialImportYAML, "credentials:\n  ssh-key: |\n    " + credMarker},
		{"yaml folded scalar", CredentialImportYAML, "credentials:\n  ssh-key: >-\n    " + credMarker},
		{"yaml nested map", CredentialImportYAML, "credentials:\n  api-key:\n    value: " + credMarker},
		{"yaml list", CredentialImportYAML, "credentials:\n  - " + credMarker},
		{"yaml anchor", CredentialImportYAML, "credentials:\n  api-key: &a " + credMarker},
		{"yaml alias", CredentialImportYAML, "credentials:\n  api-key: *a"},
		{"yaml tag", CredentialImportYAML, "credentials:\n  api-key: !!str " + credMarker},
		{"yaml flow map", CredentialImportYAML, "credentials: {api-key: " + credMarker + "}"},
		{"yaml flow value", CredentialImportYAML, "credentials:\n  api-key: [" + credMarker + "]"},
		{"yaml uneven indent", CredentialImportYAML, "credentials:\n  api-key: x\n    sentry-token: " + credMarker},
		{"yaml tab indent", CredentialImportYAML, "credentials:\n\tapi-key: " + credMarker},
		{"yaml unclosed quote", CredentialImportYAML, "credentials:\n  api-key: \"" + credMarker},
		{"yaml stray single quote", CredentialImportYAML, "credentials:\n  api-key: '" + credMarker + "'x'"},
		{"yaml null", CredentialImportYAML, "credentials:\n  api-key: ~"},
		{"yaml colon in plain", CredentialImportYAML, "credentials:\n  api-key: a: " + credMarker},
		{"yaml line without a key", CredentialImportYAML, "credentials:\n  " + credMarker},
	}

	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := ParseCredentialImport(tc.format, tc.text)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("ParseCredentialImport = %v, want ErrInvalid", err)
			}

			if strings.Contains(err.Error(), credMarker) {
				t.Fatalf("the error shows the value: %v", err)
			}
		})
	}
}

// The value is in no Credential, and no String of a type that carries it shows it.
func TestCredentialValueHidden(t *testing.T) {
	t.Parallel()

	v := credMarker
	shown := []any{
		NewCredential{Name: "sentry-token", Type: CredentialAPIKey, Value: credMarker},
		CredentialPatch{Replace: &v},
		ResolvedCredential{Credential: Credential{Name: "sentry-token"}, Value: credMarker},
		CredentialImportEntry{Name: "sentry-token", Value: credMarker},
		[]CredentialImportEntry{{Name: "sentry-token", Value: credMarker}},
	}

	for _, x := range shown {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
			if s := fmt.Sprintf(verb, x); strings.Contains(s, credMarker) {
				t.Errorf("%T with %s shows the value: %s", x, verb, s)
			}
		}
	}

	// The patch's String says a value is replaced without showing it.
	if s := fmt.Sprint(CredentialPatch{Replace: &v}); !strings.Contains(s, "replace") {
		t.Errorf("CredentialPatch's String does not say a value is replaced: %s", s)
	}
}
