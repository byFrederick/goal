//go:build e2e

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"
)

// membersFlow is spec 3 §15 (3-11) on the real stack: the owner invites someone to the
// organization they created, the invitee signs in with the code Mailpit received, sees who invited
// them (the auth service's get-invitation) and joins; goald then lists them as a member, who reads
// the members but not the invitations; the owner makes them an admin through the identity service
// and hands them the organization through goald, in one change. It returns the invitee, now the
// organization's owner.
func (s *e2eStack) membersFlow(t *testing.T) *e2eStack {
	t.Helper()

	var org struct {
		ID string `json:"id"`
	}
	s.decode(t, s.do(t, http.MethodGet, "/v1/orgs/e2e-labs", ""), &org)

	invitee := &e2eStack{owner: "invitee-" + randomSuffix(t) + "@example.com", client: newE2EClient(t)}

	var invitation struct {
		ID string `json:"id"`
	}
	s.decode(t, s.do(t, http.MethodPost, "/api/auth/organization/invite-member",
		fmt.Sprintf(`{"organizationId":%q,"email":%q,"role":"member"}`, org.ID, invitee.owner)), &invitation)

	invitedBy(t, invitee.owner, invitation.ID)
	invitee.signIn(t)

	var owner struct {
		User struct {
			Name string `json:"name"`
		} `json:"user"`
	}
	s.decode(t, s.do(t, http.MethodGet, "/v1/me", ""), &owner)

	var shown struct {
		OrganizationName string  `json:"organizationName"`
		InviterName      *string `json:"inviterName"`
	}
	invitee.decode(t, invitee.do(t, http.MethodGet, "/api/auth/organization/get-invitation?id="+invitation.ID, ""), &shown)

	if shown.OrganizationName != "E2E Labs" || shown.InviterName == nil || *shown.InviterName != owner.User.Name {
		t.Errorf("the invitation as its recipient sees it = %+v; want E2E Labs, invited by %q", shown, owner.User.Name)
	}

	if res := invitee.do(t, http.MethodPost, "/api/auth/organization/accept-invitation", fmt.Sprintf(`{"invitationId":%q}`, invitation.ID)); res.status != http.StatusOK {
		t.Fatalf("accept = %d %s", res.status, res.body)
	}

	joined := s.memberCalled(t, invitee.owner)
	if joined.Role != "member" || !joined.CanChangeRole {
		t.Fatalf("the invitee as the owner sees them = %+v; want a member they may change", joined)
	}

	if res := invitee.do(t, http.MethodGet, "/v1/orgs/e2e-labs/members", ""); res.status != http.StatusOK {
		t.Errorf("a member listing the members = %d; want 200", res.status)
	}

	if res := invitee.do(t, http.MethodGet, "/v1/orgs/e2e-labs/invitations", ""); res.status != http.StatusForbidden {
		t.Errorf("a member listing the invitations = %d; want 403", res.status)
	}

	s.handOver(t, org.ID, joined, invitee)

	return invitee
}

// handOver makes the member an admin (the identity service), then hands them the organization
// (goald): they are its owner and the owner its admin.
func (s *e2eStack) handOver(t *testing.T, orgID string, m e2eMember, to *e2eStack) {
	t.Helper()

	promote := fmt.Sprintf(`{"organizationId":%q,"memberId":%q,"role":"admin"}`, orgID, m.MemberID)
	if res := s.do(t, http.MethodPost, "/api/auth/organization/update-member-role", promote); res.status != http.StatusOK {
		t.Fatalf("make them an admin = %d %s", res.status, res.body)
	}

	var after struct {
		Role string `json:"role"`
	}
	s.decode(t, s.do(t, http.MethodPost, "/v1/orgs/e2e-labs/transfer", fmt.Sprintf(`{"to":%q,"confirm":"E2E Labs"}`, m.User.ID)), &after)

	if after.Role != "admin" {
		t.Errorf("the former owner's role after the transfer = %q; want admin", after.Role)
	}

	to.decode(t, to.do(t, http.MethodGet, "/v1/orgs/e2e-labs", ""), &after)

	if after.Role != "owner" {
		t.Errorf("the new owner's role = %q; want owner", after.Role)
	}
}

// e2eMember is a member as listMembers answers.
type e2eMember struct {
	MemberID string `json:"member_id"`
	User     struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	} `json:"user"`
	Role          string `json:"role"`
	CanChangeRole bool   `json:"can_change_role"`
}

// memberCalled is the member of e2e-labs with this email, as the caller sees them.
func (s *e2eStack) memberCalled(t *testing.T, email string) e2eMember {
	t.Helper()

	var list struct {
		Members []e2eMember `json:"members"`
	}
	s.decode(t, s.do(t, http.MethodGet, "/v1/orgs/e2e-labs/members", ""), &list)

	for _, m := range list.Members {
		if strings.EqualFold(m.User.Email, email) {
			return m
		}
	}

	t.Fatalf("members = %+v; want %s among them", list.Members, email)

	return e2eMember{}
}

// invitedBy waits for the invitation email Mailpit received for email and checks it links to the
// invitation on the public URL.
func invitedBy(t *testing.T, email, invitation string) {
	t.Helper()

	query := url.QueryEscape(`to:"` + email + `" subject:"invited you"`)
	deadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) {
		var search struct {
			Messages []struct {
				ID string `json:"ID"`
			} `json:"messages"`
		}

		res, err := mailpitGet(t, "/api/v1/search?query="+query)
		if err == nil && json.NewDecoder(res.Body).Decode(&search) == nil && len(search.Messages) > 0 {
			_ = res.Body.Close()

			if text := mailText(t, search.Messages[0].ID); !strings.Contains(text, e2ePublicURL+"/invite/"+invitation) {
				t.Errorf("the invitation email says %q; want a link to %s/invite/%s", text, e2ePublicURL, invitation)
			}

			return
		}

		if res != nil {
			_ = res.Body.Close()
		}

		time.Sleep(500 * time.Millisecond)
	}

	t.Fatalf("no invitation for %s reached Mailpit within 30 s", email)
}

// mailText is the text of a message Mailpit received.
func mailText(t *testing.T, id string) string {
	t.Helper()

	res, err := mailpitGet(t, "/api/v1/message/"+url.PathEscape(id))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	var message struct {
		Text string `json:"Text"`
	}
	if err := json.NewDecoder(res.Body).Decode(&message); err != nil {
		t.Fatal(err)
	}

	return message.Text
}

// newE2EClient is a browser of its own: its own cookies.
func newE2EClient(t *testing.T) *http.Client {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}

	return &http.Client{Jar: jar, Timeout: 30 * time.Second}
}
