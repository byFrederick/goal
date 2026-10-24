package domain

import "time"

// User is a person of the install: a Better Auth user.
type User struct {
	ID          UserID
	Name, Email string
}

// PlatformAdmin is a person who may create organizations and manage platform admins. Inside an
// organization they have only the rights of their membership there.
type PlatformAdmin struct {
	User User
	// AddedBy is who made them one: a platform admin, system:bootstrap (the first sign-in) or
	// system:recovery (goal db recover-admin).
	AddedBy Actor
	AddedAt time.Time
}

// AuthEvent is one event of the install's security log (sign-ins, codes, lockouts, device
// approvals, platform admin changes, recoveries). It never holds an email, code, token or key:
// Subject is base64url(SHA-256) of the lowercase email or user code it names.
type AuthEvent struct {
	ID int64
	At time.Time
	// Kind is <noun>.<past_verb>, as in sign_in.succeeded.
	Kind string
	// User is the person the event is about; "" when unknown.
	User                   UserID
	Subject, IP, UserAgent string
	Data                   map[string]any
}
