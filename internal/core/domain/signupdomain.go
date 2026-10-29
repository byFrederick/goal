package domain

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Errors of sign-up by domain (ADR 0023, decision 7). Each wraps ErrInvalid; the API answers
// ErrSignupDomainInvalid, ErrSignupDomainNotYours and ErrSignupDomainPublic with 400 and
// ErrSignupDomainTaken with 409.
var (
	// ErrSignupDomainInvalid means a malformed domain or an unknown join role.
	ErrSignupDomainInvalid = fmt.Errorf("not a sign-up domain: %w", ErrInvalid)
	// ErrSignupDomainNotYours means the acting owner's own sign-in email is not at the domain.
	ErrSignupDomainNotYours = fmt.Errorf("the domain is not that of your own sign-in email: %w", ErrInvalid)
	// ErrSignupDomainPublic means the domain is a public webmail domain, which would admit anyone.
	ErrSignupDomainPublic = fmt.Errorf("a public email domain cannot be a sign-up domain: %w", ErrInvalid)
	// ErrSignupDomainTaken means another live organization holds the domain.
	ErrSignupDomainTaken = fmt.Errorf("another organization holds the domain: %w", ErrInvalid)
)

// The audit actions of a sign-up domain (goal_set_signup_domain, 00081): set {domain, join_role,
// from: the previous {domain, join_role} or null} and cleared {domain}.
const (
	AuditOrgSignupDomainSet     = "org.signup_domain_set"
	AuditOrgSignupDomainCleared = "org.signup_domain_cleared"
)

// SignupDomain is an organization's sign-up domain: anyone whose verified sign-in email is
// exactly at Domain may sign up without an invitation, joining as a member with JoinRole on
// every factory that exists then.
type SignupDomain struct {
	Domain   string
	JoinRole FactoryRole
	SetBy    Actor
	SetAt    time.Time
}

// domainName is a DNS name of two labels or more: letters, digits and inner hyphens, lower case.
var domainName = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// CheckSignupDomain returns d as a sign-up domain is stored, trimmed and lower case, or an error
// wrapping ErrSignupDomainInvalid when it is no domain name.
func CheckSignupDomain(d string) (string, error) {
	d = strings.ToLower(strings.TrimSpace(d))
	if len(d) > 253 || !domainName.MatchString(d) {
		return "", fmt.Errorf("domain %q: %w", d, ErrSignupDomainInvalid)
	}

	return d, nil
}

// CheckJoinRole is r, or viewer when it is empty; an unknown role wraps ErrSignupDomainInvalid.
func CheckJoinRole(r FactoryRole) (FactoryRole, error) {
	if r == "" {
		return FactoryViewer, nil
	}

	if !r.Includes(FactoryViewer) {
		return "", fmt.Errorf("join role %q: %w", r, ErrSignupDomainInvalid)
	}

	return r, nil
}

// EmailDomain is the domain of an email, after its last @, lower case; "" when it has none.
func EmailDomain(email string) string {
	i := strings.LastIndex(email, "@")
	if i < 0 {
		return ""
	}

	return strings.ToLower(strings.TrimSpace(email[i+1:]))
}
