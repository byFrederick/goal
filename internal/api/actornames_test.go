package api_test

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api"
	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
)

// formerID was never a member of acme: it stands for someone who has left.
const formerID domain.UserID = "019a4000-0000-7000-8000-000000000009"

// formerMember is how the API names someone who is no longer a member.
const formerMember = "A former member"

// countedAccess counts the API's MemberNames calls, and fails them with err when it is set.
type countedAccess struct {
	api.Access

	calls atomic.Int32
	err   error
}

func (a *countedAccess) MemberNames(ctx context.Context, users []domain.UserID) (map[domain.UserID]string, error) {
	a.calls.Add(1)

	if a.err != nil {
		return nil, a.err
	}

	return a.Access.MemberNames(ctx, users)
}

// countingNames is the option that puts counted in front of the harness's Access.
func countingNames(counted *countedAccess) func(*harness, *api.Deps) {
	return func(_ *harness, d *api.Deps) {
		counted.Access = d.Access
		d.Access = counted
	}
}

// filedBy files an item of acme through the CLI by a.
func (h signedIn) filedBy(t *testing.T, title string, a domain.Actor) domain.Item {
	t.Helper()

	return h.filedStartingBy(t, title, a, time.Time{})
}

// filedStartingBy is filedBy with the item to start at start (none when zero).
func (h signedIn) filedStartingBy(t *testing.T, title string, a domain.Actor, start time.Time) domain.Item {
	t.Helper()

	return h.filedAt(t, domain.NewItem{Title: title, Origin: domain.OriginPerson, Via: domain.ViaCLI, CreatedBy: a, StartAt: start})
}

func user(id domain.UserID) domain.Actor {
	return domain.Actor{Kind: domain.ActorUser, ID: string(id), Name: string(id)}
}

func (h signedIn) item(t *testing.T, ref string) gen.Item {
	t.Helper()

	return decode[gen.Item](t, h.as(t, ownerToken, http.MethodGet, "/v1/orgs/acme/items/"+url.PathEscape(ref), ""), http.StatusOK)
}

// An item filed through the CLI by a member names them as the sidebar does; its id and kind stay
// the stored ref's.
func TestItem_CreatedByNamesTheMember(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)
	h.filedBy(t, "by the owner", user(ownerID))

	got := h.item(t, "acme#1").CreatedBy
	if got == nil || *got != (gen.Actor{Kind: gen.ActorKindUser, Id: string(ownerID), Name: ownerName}) {
		t.Errorf("created_by = %+v; want %s named %q", got, ownerID, ownerName)
	}
}

// Someone no longer in the organization, removed or never seen, is "A former member", never
// their id.
func TestItem_CreatedByFormerMember(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)
	h.filedBy(t, "by the viewer", user(viewerID))
	h.filedBy(t, "by someone who left", user(formerID))

	if err := h.identity.SetOrgRole(testOrg, viewerID, ""); err != nil {
		t.Fatal(err)
	}

	for ref, id := range map[string]domain.UserID{"acme#1": viewerID, "acme#2": formerID} {
		if got := h.item(t, ref).CreatedBy; got == nil || got.Id != string(id) || got.Name != "A former member" {
			t.Errorf("%s created_by = %+v; want %s named A former member", ref, got, id)
		}
	}
}

// A failed lookup still answers the read: goald logs it and names the person "A former member".
func TestItem_CreatedByLookupFailureFallsBack(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	counted := &countedAccess{err: errors.New("identity down")}
	h := newSignedInWith(t, &logs, countingNames(counted))
	h.filedBy(t, "by the owner", user(ownerID))

	if got := h.item(t, "acme#1").CreatedBy; got == nil || got.Id != string(ownerID) || got.Name != "A former member" {
		t.Errorf("created_by = %+v; want %s named A former member", got, ownerID)
	}

	if !strings.Contains(logs.String(), `"msg":"member names not read"`) || !strings.Contains(logs.String(), "identity down") {
		t.Errorf("log = %s; want member names not read with the error", logs.String())
	}
}

// names is each item's created_by name by title, "-" for none.
func names[T any](items []T, of func(T) (string, *gen.Actor)) map[string]string {
	out := map[string]string{}

	for _, it := range items {
		title, a := of(it)
		out[title] = "-"

		if a != nil {
			out[title] = a.Name
		}
	}

	return out
}

// filedByEveryone files an item, to start at start, by each kind of author: two members, someone
// who left, the local user, an API key, an agent and the engine.
func (h signedIn) filedByEveryone(t *testing.T, start time.Time) map[string]string {
	t.Helper()

	h.filedStartingBy(t, "owner", user(ownerID), start)
	h.filedStartingBy(t, "viewer", user(viewerID), start)
	h.filedStartingBy(t, "former", user(formerID), start)
	h.filedStartingBy(t, "local user", domain.LocalUser, start)
	h.filedStartingBy(t, "key", domain.Actor{Kind: domain.ActorService, ID: "key-1", Name: "key-1"}, start)
	h.filedStartingBy(t, "agent", domain.Actor{Kind: domain.ActorAgent, ID: "investigator", Name: "investigator"}, start)
	h.filedStartingBy(t, "engine", domain.Actor{Kind: domain.ActorSystem, ID: "engine", Name: "engine"}, start)

	return map[string]string{
		"owner": ownerName, "viewer": "M. Okafor", "former": "A former member", "local user": "Local user (before sign-in)",
		"key": "key-1", "agent": "investigator", "engine": "-",
	}
}

func cardName(c gen.ItemCard) (string, *gen.Actor) { return c.Title, c.CreatedBy }

// The items list and the board name every author with one MemberNames call per request.
func TestItems_CreatedByOneLookup(t *testing.T) {
	t.Parallel()

	counted := &countedAccess{}
	h := newSignedInWith(t, nil, countingNames(counted))
	want := h.filedByEveryone(t, time.Time{})

	page := decode[gen.ItemPage](t, h.as(t, ownerToken, http.MethodGet, acmeItems, ""), http.StatusOK)
	if got := names(page.Items, cardName); !maps.Equal(got, want) {
		t.Errorf("items list names = %v; want %v", got, want)
	}

	if n := counted.calls.Swap(0); n != 1 {
		t.Errorf("items list called MemberNames %d times; want 1", n)
	}

	board := decode[gen.Board](t, h.as(t, ownerToken, http.MethodGet, "/v1/orgs/acme/factories/acme/board", ""), http.StatusOK)

	var cards []gen.ItemCard
	for _, st := range board.Stations {
		cards = append(cards, st.Items...)
	}

	if got := names(cards, cardName); !maps.Equal(got, want) {
		t.Errorf("board names = %v; want %v", got, want)
	}

	if n := counted.calls.Load(); n != 1 {
		t.Errorf("board called MemberNames %d times; want 1", n)
	}
}

// Intake's Coming up names every author with one MemberNames call.
func TestIntake_ComingUpOneLookup(t *testing.T) {
	t.Parallel()

	counted := &countedAccess{}
	h := newSignedInWith(t, nil, countingNames(counted))
	want := h.filedByEveryone(t, t0.Add(24*time.Hour))

	in := decode[gen.Intake](t, h.as(t, ownerToken, http.MethodGet, intakePath, ""), http.StatusOK)
	if in.ComingUp == nil {
		t.Fatal("intake has no coming up")
	}

	got := names(*in.ComingUp, func(c gen.IntakeComingUp) (string, *gen.Actor) { return c.Title, c.CreatedBy })
	if !maps.Equal(got, want) {
		t.Errorf("coming up names = %v; want %v", got, want)
	}

	if n := counted.calls.Load(); n != 1 {
		t.Errorf("coming up called MemberNames %d times; want 1", n)
	}
}

// Under GOAL_AUTH=none nobody is looked up: every actor keeps the name its ref gives.
func TestItems_CreatedByNoLookupWithoutSignIn(t *testing.T) {
	t.Parallel()

	counted := &countedAccess{}
	h := newHarnessLogged(t, nil, nil, countingNames(counted))
	h.filedAt(t, domain.NewItem{Title: "by a user", Origin: domain.OriginPerson, Via: domain.ViaCLI, CreatedBy: user(ownerID)})

	if got := h.getItem(t, "acme#1").CreatedBy; got == nil || got.Name != string(ownerID) {
		t.Errorf("created_by = %+v; want the ref's id as its name", got)
	}

	if n := counted.calls.Load(); n != 0 {
		t.Errorf("MemberNames called %d times under GOAL_AUTH=none; want 0", n)
	}
}

// keyBy stores a key of acme made by a straight in the fake, as if through the API.
func (h signedIn) keyBy(t *testing.T, name string, by domain.Actor) domain.APIKey {
	t.Helper()

	k, err := h.identity.CreateAPIKey(inOrg(t), by, domain.NewAPIKey{
		Name: name, Hint: "goal_sk_" + name, Hash: "hash-" + name, Scopes: []domain.Permission{scopeOrgRead}, Ceiling: domain.FactoryViewer, ExpiresInDays: 30,
	})
	if err != nil {
		t.Fatal(err)
	}

	return k
}

// API keys name who made and revoked them: the list with one MemberNames call for every key, the
// new key's answer with one more.
func TestAPIKeys_NamesPeopleInOneLookup(t *testing.T) {
	t.Parallel()

	counted := &countedAccess{}
	h := newSignedInWith(t, nil, countingNames(counted))
	h.identity.AddSession(freshToken, ownerID, domain.Session{ID: "fresh-owner", CreatedAt: t0, ExpiresAt: t0.Add(7 * 24 * time.Hour), Browser: true})

	made, _ := h.create(t, ciDeploy)
	if got := made.Key.CreatedBy; got != (gen.Actor{Kind: gen.ActorKindUser, Id: string(ownerID), Name: ownerName}) {
		t.Errorf("new key's created_by = %+v; want %s named %q", got, ownerID, ownerName)
	}

	if n := counted.calls.Swap(0); n != 1 {
		t.Errorf("create called MemberNames %d times; want 1", n)
	}

	h.keyBy(t, "by-former", user(formerID))

	revoked := h.keyBy(t, "revoked-by-viewer", user(ownerID))
	must(t, h.identity.RevokeAPIKey(inOrg(t), user(viewerID), revoked.ID))
	must(t, h.identity.SetOrgRole(testOrg, viewerID, ""))

	list := decode[gen.ApiKeyList](t, h.as(t, freshToken, http.MethodGet, keysPath, ""), http.StatusOK)

	got := map[string]string{}
	for _, k := range list.Keys {
		got[k.Name] = k.CreatedBy.Name
		if k.RevokedBy != nil {
			got[k.Name+" revoked"] = k.RevokedBy.Name
		}
	}

	want := map[string]string{
		"ci-deploy": ownerName, "by-former": formerMember, "revoked-by-viewer": ownerName, "revoked-by-viewer revoked": formerMember,
	}
	if !maps.Equal(got, want) {
		t.Errorf("key names = %v; want %v", got, want)
	}

	if n := counted.calls.Load(); n != 1 {
		t.Errorf("list called MemberNames %d times; want 1", n)
	}
}

// The sign-up domain names the owner who set it, in the answer to setting it and in the settings,
// one MemberNames call each.
func TestSignupDomain_SetByNamesTheMember(t *testing.T) {
	t.Parallel()

	counted := &countedAccess{}
	h := newSignedInWith(t, nil, countingNames(counted))
	withPeople(t, h)
	h.tenancy.WithSignIn(h.identity.Email, "gmail.com")

	want := gen.Actor{Kind: gen.ActorKindUser, Id: string(ownerID), Name: ownerName}

	set := decode[gen.SignupDomain](t, h.as(t, freshOwnerToken, http.MethodPut, signupPath, ownDomain), http.StatusOK)
	if set.SetBy != want {
		t.Errorf("set's set_by = %+v; want %+v", set.SetBy, want)
	}

	if n := counted.calls.Swap(0); n != 1 {
		t.Errorf("set called MemberNames %d times; want 1", n)
	}

	if s := ownerSettings(t, h); s.SignupDomain == nil || s.SignupDomain.SetBy != want {
		t.Errorf("settings' signup_domain = %+v; want set by %+v", s.SignupDomain, want)
	}

	if n := counted.calls.Load(); n != 1 {
		t.Errorf("settings called MemberNames %d times; want 1", n)
	}
}

// An owner who set the sign-up domain and has since left is "A former member".
func TestSignupDomain_SetByFormerMember(t *testing.T) {
	t.Parallel()

	h := withSignupDomains(t)
	decode[gen.SignupDomain](t, h.as(t, freshOwnerToken, http.MethodPut, signupPath, ownDomain), http.StatusOK)
	must(t, h.identity.SetOrgRole(testOrg, viewerID, domain.OrgOwner))
	must(t, h.identity.SetOrgRole(testOrg, ownerID, ""))

	s := decode[gen.OrgSettings](t, h.as(t, viewerToken, http.MethodGet, acmeSettings, ""), http.StatusOK)
	if s.SignupDomain == nil || s.SignupDomain.SetBy.Id != string(ownerID) || s.SignupDomain.SetBy.Name != formerMember {
		t.Errorf("signup_domain = %+v; want set by %s named %s", s.SignupDomain, ownerID, formerMember)
	}
}

// noteBy appends a's note on it straight to the store, with a name its event kept that a touch no
// longer shows.
func (h *harness) noteBy(t *testing.T, it domain.Item, a domain.Actor) {
	t.Helper()

	h.noteAt(t, it, a.Ref(), "Name at the time", t0.Add(time.Minute))
}

// touchedByEveryone files acme#1 and touches it by two members, someone who left and the local
// user, the viewer then removed. It returns the names each touch should show by actor id.
func (h signedIn) touchedByEveryone(t *testing.T) map[string]string {
	t.Helper()

	it := h.filedBy(t, "touched", user(ownerID))
	for _, a := range []domain.Actor{user(ownerID), user(viewerID), user(formerID), domain.LocalUser} {
		h.noteBy(t, it, a)
	}

	must(t, h.identity.SetOrgRole(testOrg, viewerID, ""))

	return map[string]string{
		string(ownerID): ownerName, string(viewerID): formerMember, string(formerID): formerMember,
		domain.LocalUser.ID: "Local user (before sign-in)",
	}
}

func touchNames(touches []gen.Touch) map[string]string {
	out := map[string]string{}
	for _, tc := range touches {
		out[tc.Actor.Id] = tc.Actor.Name
	}

	return out
}

// An item names the people of its touches, and who filed it, with one MemberNames call.
func TestItem_TouchesNameMembersInOneLookup(t *testing.T) {
	t.Parallel()

	counted := &countedAccess{}
	h := newSignedInWith(t, nil, countingNames(counted))
	want := h.touchedByEveryone(t)

	it := h.item(t, "acme#1")
	if it.Touches == nil || !maps.Equal(touchNames(it.Touches.List), want) {
		t.Errorf("touches = %+v; want names %v", it.Touches, want)
	}

	if it.CreatedBy == nil || it.CreatedBy.Name != ownerName {
		t.Errorf("created_by = %+v; want %s", it.CreatedBy, ownerName)
	}

	if n := counted.calls.Load(); n != 1 {
		t.Errorf("item called MemberNames %d times; want 1", n)
	}
}

// An item's teardown names the people of its touches with one MemberNames call.
func TestTeardown_TouchesNameMembersInOneLookup(t *testing.T) {
	t.Parallel()

	counted := &countedAccess{}
	h := newSignedInWith(t, nil, countingNames(counted))
	want := h.touchedByEveryone(t)

	td := decode[gen.Teardown](t, h.as(t, ownerToken, http.MethodGet, acme1+"/teardown", ""), http.StatusOK)

	touches := make([]gen.Touch, 0, len(td.Touches))
	for _, tc := range td.Touches {
		touches = append(touches, tc.Touch)
	}

	if got := touchNames(touches); !maps.Equal(got, want) {
		t.Errorf("teardown touch names = %v; want %v", got, want)
	}

	if n := counted.calls.Load(); n != 1 {
		t.Errorf("teardown called MemberNames %d times; want 1", n)
	}
}

// Under GOAL_AUTH=none a touch is not looked up either: a person's is named by its ref.
func TestItem_TouchesNoLookupWithoutSignIn(t *testing.T) {
	t.Parallel()

	counted := &countedAccess{}
	h := newHarnessLogged(t, nil, nil, countingNames(counted))
	it := h.filedAt(t, domain.NewItem{Title: "touched", Origin: domain.OriginPerson, Via: domain.ViaCLI, CreatedBy: user(ownerID)})
	h.noteBy(t, it, user(ownerID))

	if got := touchNames(h.itemTouches(t).List); got[string(ownerID)] != string(ownerID) {
		t.Errorf("touch names = %v; want the ref's id as its name", got)
	}

	if n := counted.calls.Load(); n != 0 {
		t.Errorf("MemberNames called %d times under GOAL_AUTH=none; want 0", n)
	}
}
