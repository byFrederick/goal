package api_test

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"net/http"
	"net/url"
	"strconv"
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

// localUserName is how the API names the local user from before sign-in.
const localUserName = "Local user (before sign-in)"

// formerName is how the API names a person who is no longer a member.
const formerName = "A former member"

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

// Someone no longer in the organization, removed or never seen, is formerName, never
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
		if got := h.item(t, ref).CreatedBy; got == nil || got.Id != string(id) || got.Name != formerName {
			t.Errorf("%s created_by = %+v; want %s named A former member", ref, got, id)
		}
	}
}

// A failed lookup still answers the read: goald logs it and names the person formerName.
func TestItem_CreatedByLookupFailureFallsBack(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	counted := &countedAccess{err: errors.New("identity down")}
	h := newSignedInWith(t, &logs, countingNames(counted))
	h.filedBy(t, "by the owner", user(ownerID))

	if got := h.item(t, "acme#1").CreatedBy; got == nil || got.Id != string(ownerID) || got.Name != formerName {
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
		"owner": ownerName, "viewer": viewerName, "former": formerName, "local user": localUserName,
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

// asWith is as with header's fields too.
func (h signedIn) asWith(t *testing.T, token, method, path, body string, header http.Header) response {
	t.Helper()

	all := cookie(token)
	maps.Copy(all, header)

	return h.request(t, method, path, body, all)
}

// publishedAs publishes acme, titled title, as token's person: it opens a draft, saves the
// title and publishes it against the version running.
func (h signedIn) publishedAs(t *testing.T, token, title string) gen.Version {
	t.Helper()

	f := decode[gen.Factory](t, h.as(t, token, http.MethodGet, acmeFactory, ""), http.StatusOK)
	decode[gen.Draft](t, h.as(t, token, http.MethodPost, acmeDraft, `{}`), http.StatusCreated)

	file := h.as(t, token, http.MethodGet, acmeDraft+"/files/factory.yaml", "")
	if file.status != http.StatusOK {
		t.Fatalf("read factory.yaml: %d %s", file.status, file.body)
	}

	doc := strings.Replace(acme, "title: Acme }", "title: "+title+" }", 1)
	saved := decode[gen.SaveResult](t, h.asWith(t, token, http.MethodPut, acmeDraft+"/files/factory.yaml", doc,
		http.Header{ifMatchHeader: {file.header.Get("ETag")}}), http.StatusOK)

	return decode[gen.Version](t, h.as(t, token, http.MethodPost, acmeDraft+"/publish", body(t, map[string]any{
		revisionKey: saved.Revision, expectedKey: f.Current.Id, "message": title,
	})), http.StatusCreated)
}

// viewerEdits makes the viewer an admin of acme, who edits every factory.
func (h signedIn) viewerEdits(t *testing.T) {
	t.Helper()

	if err := h.identity.SetOrgRole(testOrg, viewerID, domain.OrgAdmin); err != nil {
		t.Fatal(err)
	}
}

func named(id domain.UserID, name string) gen.Actor {
	return gen.Actor{Kind: gen.ActorKindUser, Id: string(id), Name: name}
}

// A version a member publishes in the web app names them, on the factory and on the version.
func TestVersion_PublishedByNamesTheMember(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)
	v := h.publishedAs(t, ownerToken, "Acme two")
	want := named(ownerID, ownerName)

	if v.PublishedBy == nil || *v.PublishedBy != want {
		t.Errorf("publish published_by = %+v; want %+v", v.PublishedBy, want)
	}

	f := decode[gen.Factory](t, h.as(t, ownerToken, http.MethodGet, acmeFactory, ""), http.StatusOK)
	if f.Current == nil || f.Current.PublishedBy != want {
		t.Errorf("factory current = %+v; want published by %+v", f.Current, want)
	}

	got := decode[gen.VersionDetail](t, h.as(t, ownerToken, http.MethodGet, acmeFactory+"/versions/"+strconv.Itoa(*v.Number), ""), http.StatusOK)
	if got.PublishedBy == nil || *got.PublishedBy != want {
		t.Errorf("version published_by = %+v; want %+v", got.PublishedBy, want)
	}
}

// The version history names every publisher with one MemberNames call.
func TestVersions_PublishedByOneLookup(t *testing.T) {
	t.Parallel()

	counted := &countedAccess{}
	h := newSignedInWith(t, nil, countingNames(counted))
	h.viewerEdits(t)
	h.publishedAs(t, ownerToken, "Acme two")
	h.publishedAs(t, viewerToken, "Acme three")
	h.publishedAs(t, ownerToken, "Acme four")
	counted.calls.Store(0)

	page := decode[gen.VersionPage](t, h.as(t, ownerToken, http.MethodGet, acmeFactory+"/versions", ""), http.StatusOK)

	got := map[int]string{}

	for _, v := range page.Versions {
		if v.Number != nil && v.PublishedBy != nil && *v.Number > 1 {
			got[*v.Number] = v.PublishedBy.Name
		}
	}

	if want := map[int]string{2: ownerName, 3: viewerName, 4: ownerName}; !maps.Equal(got, want) {
		t.Errorf("history publishers = %v; want %v", got, want)
	}

	if n := counted.calls.Load(); n != 1 {
		t.Errorf("history called MemberNames %d times; want 1", n)
	}
}

// The factories list names the actors of every factory with one MemberNames call.
func TestFactories_ActorsOneLookup(t *testing.T) {
	t.Parallel()

	counted := &countedAccess{}
	h := newSignedInWith(t, nil, countingNames(counted))
	h.viewerEdits(t)
	h.publishedAs(t, ownerToken, "Acme two")
	decode[gen.Draft](t, h.as(t, viewerToken, http.MethodPost, factoriesPath+"/line/draft", `{}`), http.StatusCreated)
	counted.calls.Store(0)

	list := decode[gen.FactoryList](t, h.as(t, ownerToken, http.MethodGet, factoriesPath, ""), http.StatusOK)

	got := map[string]string{}

	for _, f := range list.Factories {
		if f.Current != nil {
			got[f.Name+" current"] = f.Current.PublishedBy.Name
		}

		if f.Draft != nil {
			got[f.Name+" draft"] = f.Draft.UpdatedBy.Name
		}
	}

	if got["acme current"] != ownerName || got["line draft"] != viewerName {
		t.Errorf("factory actors = %v; want acme current by %s and line's draft by M. Okafor", got, ownerName)
	}

	if n := counted.calls.Load(); n != 1 {
		t.Errorf("factories list called MemberNames %d times; want 1", n)
	}
}

// A draft names who opened it and who last saved it, and so does its factory.
func TestDraft_ActorsNameTheMember(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)
	want := named(ownerID, ownerName)

	d := decode[gen.Draft](t, h.as(t, ownerToken, http.MethodPost, acmeDraft, `{}`), http.StatusCreated)
	if d.CreatedBy != want || d.UpdatedBy != want {
		t.Errorf("opened draft by %+v, updated by %+v; want %+v", d.CreatedBy, d.UpdatedBy, want)
	}

	d = decode[gen.Draft](t, h.as(t, ownerToken, http.MethodGet, acmeDraft, ""), http.StatusOK)
	if d.CreatedBy != want || d.UpdatedBy != want {
		t.Errorf("draft by %+v, updated by %+v; want %+v", d.CreatedBy, d.UpdatedBy, want)
	}

	f := decode[gen.Factory](t, h.as(t, ownerToken, http.MethodGet, acmeFactory, ""), http.StatusOK)
	if f.Draft == nil || f.Draft.UpdatedBy != want {
		t.Errorf("factory draft = %+v; want updated by %+v", f.Draft, want)
	}
}

// requested files a draft request on acme by a, as the API does, and ends it.
func (h signedIn) requested(t *testing.T, a domain.Actor, text string) domain.DraftRequest {
	t.Helper()

	r, err := h.drafts.Create(domain.WithOrg(t.Context(), testOrg), domain.NewDraftRequest{Factory: acmeID, Requester: a, Text: text})
	if err != nil {
		t.Fatal(err)
	}

	h.finishDraftRequests(t, acmeID)

	return r
}

// A draft request by someone who has left names them formerName.
func TestDraftRequest_RequesterFormerMember(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)
	r := h.requested(t, user(viewerID), "give review a 4th reviewer")

	if err := h.identity.SetOrgRole(testOrg, viewerID, ""); err != nil {
		t.Fatal(err)
	}

	got := decode[gen.DraftRequest](t, h.as(t, ownerToken, http.MethodGet, acmeDraftRequests+"/"+string(r.ID), ""), http.StatusOK)
	if want := named(viewerID, formerName); got.Requester != want {
		t.Errorf("requester = %+v; want %+v", got.Requester, want)
	}
}

// The draft requests list names every requester with one MemberNames call.
func TestDraftRequests_RequesterOneLookup(t *testing.T) {
	t.Parallel()

	counted := &countedAccess{}
	h := newSignedInWith(t, nil, countingNames(counted))
	h.requested(t, user(ownerID), "by the owner")
	h.requested(t, user(viewerID), "by the viewer")
	h.requested(t, user(formerID), "by someone who left")
	h.requested(t, domain.LocalUser, "by the local user")

	list := decode[gen.DraftRequestList](t, h.as(t, ownerToken, http.MethodGet, acmeDraftRequests, ""), http.StatusOK)

	got := map[string]string{}
	for _, r := range list.Requests {
		got[r.Text] = r.Requester.Name
	}

	want := map[string]string{
		"by the owner": ownerName, "by the viewer": viewerName, "by someone who left": formerName,
		"by the local user": localUserName,
	}
	if !maps.Equal(got, want) {
		t.Errorf("requesters = %v; want %v", got, want)
	}

	if n := counted.calls.Load(); n != 1 {
		t.Errorf("draft requests list called MemberNames %d times; want 1", n)
	}
}

// Under GOAL_AUTH=none the definitions routes look nobody up.
func TestVersions_NoLookupWithoutSignIn(t *testing.T) {
	t.Parallel()

	counted := &countedAccess{}
	h := newHarnessLogged(t, nil, nil, countingNames(counted))
	v := h.renamed(t)

	if v.PublishedBy == nil || *v.PublishedBy != (gen.Actor{Kind: gen.ActorKind(domain.LocalUser.Kind), Id: domain.LocalUser.ID, Name: domain.LocalUser.Name}) {
		t.Errorf("published_by = %+v; want the local user", v.PublishedBy)
	}

	for _, path := range []string{factoriesPath, acmeFactory, acmeFactory + "/versions", acmeFactory + "/versions/" + strconv.Itoa(*v.Number), acmeDraftRequests} {
		if res := h.request(t, http.MethodGet, path, "", nil); res.status != http.StatusOK {
			t.Errorf("GET %s = %d %s", path, res.status, res.body)
		}
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
		"ci-deploy": ownerName, "by-former": formerName, "revoked-by-viewer": ownerName, "revoked-by-viewer revoked": formerName,
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
	if s.SignupDomain == nil || s.SignupDomain.SetBy.Id != string(ownerID) || s.SignupDomain.SetBy.Name != formerName {
		t.Errorf("signup_domain = %+v; want set by %s named %s", s.SignupDomain, ownerID, formerName)
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
		string(ownerID): ownerName, string(viewerID): formerName, string(formerID): formerName,
		domain.LocalUser.ID: localUserName,
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
