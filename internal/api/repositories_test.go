package api_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
)

// acmeWithRepositories is acme with one entry, for every repository of the acme owner; the
// domain's tests cover several entries and the default entry beside an owner pattern.
var acmeWithRepositories = strings.Replace(acme, `runtime: { modelKey: "{{claude-key}}" }`,
	`runtime: { modelKey: "{{claude-key}}", repositories: [`+
		`{ name: code, url: "https://github.com/acme/*", token: "{{`+repoSecret+`}}", access: write }] }`, 1)

// publishRepositories publishes acmeWithRepositories and refreshes the definitions cache.
func (h *harness) publishRepositories(t *testing.T) {
	t.Helper()

	h.createRepoSecret(t)
	h.openDraft(t)
	h.saveYAML(t, acmeWithRepositories)
	h.publish(t, "Work on acme's repositories")
	h.refreshVersions(t)
}

// reposOf is an item's repositories as repo/joined, in order.
func reposOf(it gen.Item) []string {
	if it.Repositories == nil {
		return nil
	}

	out := make([]string, 0, len(*it.Repositories))
	for _, r := range *it.Repositories {
		out = append(out, r.Repo+" "+string(r.Joined))
	}

	return out
}

// An item filed naming repositories by owner/name, host/owner/name and URL answers with them,
// normalized, once each and in the order named, joined at filing; getItem answers the same, and
// both carry the item's branch.
func TestAPI_SubmitWithRepositories(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.publishRepositories(t)

	var it gen.Item
	if code := h.call(t, http.MethodPost, acmeItems,
		`{"title":"Add a flag","type":"fix","repositories":["Acme/API","github.com/acme/web","https://github.com/acme/api.git"]}`, &it); code != http.StatusCreated {
		t.Fatalf("submit = %d", code)
	}

	want := []string{"github.com/acme/api filing", "github.com/acme/web filing"}
	if got := reposOf(it); !slices.Equal(got, want) {
		t.Fatalf("submit: repositories = %v; want %v", got, want)
	}

	if it.Branch == nil || *it.Branch != "goal/acme/1-add-a-flag" {
		t.Errorf("submit: branch = %v; want goal/acme/1-add-a-flag", deref(it.Branch))
	}

	if r := (*it.Repositories)[0]; r.JoinedAt == nil {
		t.Errorf("submit: %s has no joined_at", r.Repo)
	}

	var got gen.Item
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/"+it.Id, "", &got); code != http.StatusOK {
		t.Fatalf("get item = %d", code)
	}

	if !slices.Equal(reposOf(got), want) || got.Branch == nil || *got.Branch != *it.Branch {
		t.Errorf("get item: repositories %v branch %v; want %v %s", reposOf(got), got.Branch, want, *it.Branch)
	}
}

// An item filed before its factory had code has no rows; getItem shows the default entry's
// repository, joined default without a time, once the factory has one.
func TestGetItem_ShowsTheDefaultRepositoryOfAnOlderItem(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	submitted := h.submit(t)

	if got := reposOf(submitted); len(got) != 0 {
		t.Errorf("without code: repositories = %v; want none", got)
	}

	h.createRepoSecret(t)
	h.publishRepoToken(t)

	var it gen.Item
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/"+submitted.Id, "", &it); code != http.StatusOK {
		t.Fatalf("get item = %d", code)
	}

	if got, want := reposOf(it), []string{"github.com/acme/app default"}; !slices.Equal(got, want) || (*it.Repositories)[0].JoinedAt != nil {
		t.Errorf("repositories = %v (%+v); want %v without joined_at", got, it.Repositories, want)
	}
}

// A repository no entry covers, an unknown entry name, an owner pattern's entry name and more than 10 repositories are
// 400 problems naming the value as given, and file nothing.
func TestAPI_SubmitRefusesAnUnknownRepository(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.publishRepositories(t)

	eleven := make([]string, 11)
	for i := range eleven {
		eleven[i] = `"acme/r` + string(rune('a'+i)) + `"`
	}

	for body, named := range map[string]string{
		`{"title":"t","repositories":["Acme-Labs/API"]}`:                   "Acme-Labs/API",
		`{"title":"t","repositories":["code"]}`:                            "code",
		`{"title":"t","repositories":["web"]}`:                             "web",
		`{"title":"t","repositories":["gitlab.com/acme/web"]}`:             "gitlab.com/acme/web",
		`{"title":"t","repositories":[` + strings.Join(eleven, ",") + `]}`: "10",
	} {
		res := h.request(t, http.MethodPost, acmeItems, body, nil)
		p := decode[gen.Problem](t, res, http.StatusBadRequest)

		if !strings.Contains(deref(p.Detail), named) {
			t.Errorf("%s: detail %q does not name %s", body, deref(p.Detail), named)
		}
	}

	var page gen.ItemPage
	if code := h.call(t, http.MethodGet, acmeItems, "", &page); code != http.StatusOK || len(page.Items) != 0 {
		t.Errorf("list items = %d with %d items; want none filed", code, len(page.Items))
	}
}

// A card for an item with two repositories lists both in first-joined order with how each
// joined, on the Items list and the Board, read once per page; a card for an item without rows,
// in a factory without a default entry, sends an empty array, never null.
func TestAPI_ItemCardsCarryRepositories(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.publishRepositories(t)

	var two, none gen.Item
	if code := h.call(t, http.MethodPost, acmeItems, `{"title":"Add a flag","type":"fix","repositories":["acme/web","acme/api"]}`, &two); code != http.StatusCreated {
		t.Fatalf("submit with repositories = %d", code)
	}

	if code := h.call(t, http.MethodPost, acmeItems, `{"title":"Fix a typo","type":"fix"}`, &none); code != http.StatusCreated {
		t.Fatalf("submit without repositories = %d", code)
	}

	var (
		list  gen.ItemPage
		board gen.Board
	)

	h.getReadingOnce(t, "ItemsRepositories", acmeItems, &list)
	h.getReadingOnce(t, "ItemsRepositories", "/v1/orgs/acme/factories/acme/board", &board)

	want := []string{"github.com/acme/web filing", "github.com/acme/api filing"}

	for from, cards := range map[string]map[int64]gen.ItemCard{"Items": byNumber(list.Items), "Board": cardsOf(board)} {
		c, ok := cards[two.Number]
		if got := cardRepos(c); !ok || !slices.Equal(got, want) || c.Repositories[0].JoinedAt == nil {
			t.Errorf("%s: card repositories = %v (on it: %t); want %v with joined_at", from, got, ok, want)
		}

		if c, ok := cards[none.Number]; !ok || c.Repositories == nil || len(c.Repositories) != 0 {
			t.Errorf("%s: card without rows has repositories %#v (on it: %t); want an empty array", from, c.Repositories, ok)
		}
	}

	if got := encodedRepositories(t, h, none.Number); got != "[]" {
		t.Errorf("card without rows encodes repositories as %s; want []", got)
	}
}

// encodedRepositories is the repositories of the Items list's card numbered n as encoded.
func encodedRepositories(t *testing.T, h *harness, n int64) string {
	t.Helper()

	var raw struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(h.request(t, http.MethodGet, acmeItems, "", nil).body, &raw); err != nil {
		t.Fatal(err)
	}

	for _, c := range raw.Items {
		if string(c["number"]) == strconv.FormatInt(n, 10) {
			return string(c["repositories"])
		}
	}

	t.Fatalf("no card numbered %d", n)

	return ""
}

// cardRepos is a card's repositories as repo/joined, in order.
func cardRepos(c gen.ItemCard) []string {
	out := make([]string, 0, len(c.Repositories))
	for _, r := range c.Repositories {
		out = append(out, r.Repo+" "+string(r.Joined))
	}

	return out
}
