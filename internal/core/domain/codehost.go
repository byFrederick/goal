package domain

import (
	"fmt"
	"log/slog"
)

// CodeHostLogin is what ports.CodeHost signs in to a code host's API with: the host, in lower
// case, and the token sent only to it (ADR 0037 decision 7). Name says where the login came from
// (the connection it was built from today), for errors and logs. String, GoString and LogValue
// hide the token.
type CodeHostLogin struct {
	Name  string
	Host  string
	Token string
}

// String shows l without its token.
func (l CodeHostLogin) String() string {
	return fmt.Sprintf("CodeHostLogin{Name:%s Host:%s Token:%s}", l.Name, l.Host, hiddenValue(l.Token))
}

// GoString is String, so %#v hides the token too.
func (l CodeHostLogin) GoString() string { return l.String() }

// LogValue logs l without its token.
func (l CodeHostLogin) LogValue() slog.Value {
	return slog.GroupValue(slog.String("name", l.Name), slog.String("host", l.Host))
}

// CodeHostLogin is rc's code host login, from its git login (GitLogin): the host it serves and
// the password its env variable holds. It fails as GitLogin does, naming the connection and
// never holding a value.
func (rc ResolvedConnection) CodeHostLogin() (CodeHostLogin, error) {
	host, token, err := rc.GitLogin()
	if err != nil {
		return CodeHostLogin{}, err
	}

	return CodeHostLogin{Name: rc.Connection.Name, Host: host, Token: token}, nil
}

// Validate refuses a login with no host or no token, naming where it came from and never holding
// a value: adapters check it before they send anything.
func (l CodeHostLogin) Validate() error {
	if l.Host == "" || l.Token == "" {
		return fmt.Errorf("code host login %s has no host or no token: %w", l.Name, ErrInvalid)
	}

	return nil
}
