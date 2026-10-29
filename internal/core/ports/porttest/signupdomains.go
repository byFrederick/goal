package porttest

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.SignupDomains = (*Tenancy)(nil)

// WithSignIn gives t the people's sign-in emails (emailOf, false for no such user) and the public
// webmail domains its sign-up domains are checked against, as the database's auth tables and
// public_email_domains do, and returns t. Without it no domain is anyone's. The fake does not
// check that the actor is an owner: the API's authorizer does, and the PostgreSQL adapter again.
func (t *Tenancy) WithSignIn(emailOf func(domain.UserID) (string, bool), publicDomains ...string) *Tenancy {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.emailOf, t.publicDomains = emailOf, map[string]bool{}
	for _, d := range publicDomains {
		t.publicDomains[strings.ToLower(d)] = true
	}

	return t
}

// SignupDomain implements ports.SignupDomains.
func (t *Tenancy) SignupDomain(ctx context.Context) (domain.SignupDomain, bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	r, err := t.liveLocked(ctx)
	if err != nil || r.signup == nil {
		return domain.SignupDomain{}, false, err
	}

	return *r.signup, true, nil
}

// SetSignupDomain implements ports.SignupDomains.
func (t *Tenancy) SetSignupDomain(ctx context.Context, by domain.Actor, d string, role domain.FactoryRole) (domain.SignupDomain, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	r, err := t.liveLocked(ctx)
	if err != nil {
		return domain.SignupDomain{}, err
	}

	d = strings.ToLower(strings.TrimSpace(d))
	if err := t.checkSignupLocked(r, by, d); err != nil {
		return domain.SignupDomain{}, fmt.Errorf("set sign-up domain %q: %w", d, err)
	}

	if old := r.signup; old != nil && old.Domain == d && old.JoinRole == role {
		return *old, nil
	}

	var from any
	if old := r.signup; old != nil {
		from = map[string]any{"domain": old.Domain, "join_role": string(old.JoinRole)}
	}

	for _, o := range t.orgs {
		if !o.deleted.IsZero() && o.signup != nil && o.signup.Domain == d {
			o.signup = nil
		}
	}

	r.signup = &domain.SignupDomain{Domain: d, JoinRole: role, SetBy: by, SetAt: t.now().UTC().Truncate(time.Microsecond)}
	t.appendLocked(r.org.ID, orgEntry(by, domain.AuditOrgSignupDomainSet, r.org,
		map[string]any{"domain": d, "join_role": string(role), "from": from}))

	return *r.signup, nil
}

// checkSignupLocked is goal_set_signup_domain's refusals of d for by in r.
func (t *Tenancy) checkSignupLocked(r *orgRow, by domain.Actor, d string) error {
	email, ok := "", false
	if t.emailOf != nil && by.Kind == domain.ActorUser {
		email, ok = t.emailOf(domain.UserID(by.ID))
	}

	switch {
	case !ok || domain.EmailDomain(email) != d:
		return domain.ErrSignupDomainNotYours
	case t.publicDomains[d]:
		return domain.ErrSignupDomainPublic
	}

	for id, o := range t.orgs {
		if id != r.org.ID && o.deleted.IsZero() && o.signup != nil && o.signup.Domain == d {
			return domain.ErrSignupDomainTaken
		}
	}

	return nil
}

// ClearSignupDomain implements ports.SignupDomains.
func (t *Tenancy) ClearSignupDomain(ctx context.Context, by domain.Actor) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	r, err := t.liveLocked(ctx)
	if err != nil || r.signup == nil {
		return err
	}

	t.appendLocked(r.org.ID, orgEntry(by, domain.AuditOrgSignupDomainCleared, r.org, map[string]any{"domain": r.signup.Domain}))
	r.signup = nil

	return nil
}
