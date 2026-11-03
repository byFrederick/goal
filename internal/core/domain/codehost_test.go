package domain

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestCodeHostLogin_FromConnection(t *testing.T) {
	t.Parallel()

	const token = "ghp_secret-value"

	resolved := func(k ConnectionKind, values map[string]string) ResolvedConnection {
		return ResolvedConnection{Connection: Connection{Name: stCode}, Kind: k, Values: values}
	}

	got, err := resolved(githubKind(), map[string]string{fieldToken: token, fieldHost: "GHE.Example.com"}).CodeHostLogin()
	if want := (CodeHostLogin{Name: stCode, Host: hostGHE, Token: token}); err != nil || got != want {
		t.Errorf("CodeHostLogin = %v, %v; want %v", got, err, want)
	}

	for name, rc := range map[string]ResolvedConnection{
		"no git login": resolved(apiKeyKind(), map[string]string{
			fieldPrefix: "DOCS", fieldBaseURL: apiExampleURL, fieldKey: token,
		}),
		"no password value": resolved(githubKind(), map[string]string{}),
	} {
		login, err := rc.CodeHostLogin()
		if !errors.Is(err, ErrInvalid) || strings.Contains(err.Error(), token) || !strings.Contains(err.Error(), stCode) {
			t.Errorf("%s: CodeHostLogin = %v; want ErrInvalid naming the connection, without a value", name, err)
		}

		if login != (CodeHostLogin{}) {
			t.Errorf("%s: CodeHostLogin = %v on an error; want the zero login", name, login)
		}
	}
}

func TestCodeHostLogin_Validate(t *testing.T) {
	t.Parallel()

	if err := (CodeHostLogin{Name: stCode, Host: "github.com", Token: "test-token"}).Validate(); err != nil {
		t.Errorf("Validate of a whole login = %v; want nil", err)
	}

	for _, l := range []CodeHostLogin{{Name: stCode, Token: "test-token"}, {Name: stCode, Host: "github.com"}} {
		if err := l.Validate(); !errors.Is(err, ErrInvalid) || strings.Contains(err.Error(), "test-token") {
			t.Errorf("Validate(%v) = %v; want ErrInvalid without the token", l, err)
		}
	}
}

// A login reaches errors and logs through %v and slog; the token never does.
func TestCodeHostLogin_HidesTheToken(t *testing.T) {
	t.Parallel()

	const token = "ghp_secret-value"

	login := CodeHostLogin{Name: stCode, Host: "github.com", Token: token}

	var logged strings.Builder
	slog.New(slog.NewTextHandler(&logged, nil)).Info("login", "login", login)

	for name, s := range map[string]string{
		"String": login.String(), "%v": fmt.Sprintf("%v", login), "%+v": fmt.Sprintf("%+v", login),
		"slog": logged.String(),
	} {
		if strings.Contains(s, token) || !strings.Contains(s, "github.com") {
			t.Errorf("%s = %q; want the host without the token", name, s)
		}
	}
}
