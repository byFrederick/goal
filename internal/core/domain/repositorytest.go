package domain

import (
	"fmt"
	"time"
	"unicode/utf8"
)

// MaxRepositoryTestText bounds each text a kept repository test holds (its summary, warning and
// reasons), in characters.
const MaxRepositoryTestText = 2000

// The accesses a repository test judges a token for: a runtime.repositories entry's access.
const (
	RepositoryTestRead  = "read"
	RepositoryTestWrite = "write"
)

// RepositoryTest is the latest test of one repository, or an owner pattern, for a factory (ADR
// 0041 decision 8): what the token in Secret could do there when it ran, judged for Access.
// URL is the repository's RepositoryKey, or host/owner/* in lower case for an owner pattern.
// Tested is false when the host gave no answer. Read and Push are the host's two checks; for an
// owner pattern, Readable and Pushable count the repositories the token can read and push to, and
// Truncated says the count stopped. Every text is scrubbed of the token before it is kept.
type RepositoryTest struct {
	Factory            FactoryID
	URL                string
	Access             string
	Secret             string
	At                 time.Time
	Tested, OK         bool
	Summary, Warning   string
	ReadOK, PushOK     bool
	ReadReason         string
	PushReason         string
	Readable, Pushable int
	Truncated          bool
}

// Check refuses a test without a factory, a URL, a secret or a time, with an access other than
// read or write, an untested one that passed, a negative count or a text over
// MaxRepositoryTestText characters. The error wraps ErrInvalid.
func (t RepositoryTest) Check() error {
	switch {
	case t.Factory == "" || t.URL == "" || t.Secret == "" || t.At.IsZero():
		return fmt.Errorf("a repository test needs a factory, a URL, a secret and a time: %w", ErrInvalid)
	case t.Access != RepositoryTestRead && t.Access != RepositoryTestWrite:
		return fmt.Errorf("a repository test's access is read or write, not %q: %w", t.Access, ErrInvalid)
	case !t.Tested && t.OK:
		return fmt.Errorf("a repository test the host didn't answer can't pass: %w", ErrInvalid)
	case t.Readable < 0 || t.Pushable < 0:
		return fmt.Errorf("a repository test's counts are not negative: %w", ErrInvalid)
	}

	for _, s := range []string{t.Summary, t.Warning, t.ReadReason, t.PushReason} {
		if utf8.RuneCountInString(s) > MaxRepositoryTestText {
			return fmt.Errorf("a repository test's texts have at most %d characters: %w", MaxRepositoryTestText, ErrInvalid)
		}
	}

	return nil
}
