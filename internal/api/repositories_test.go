package api_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
)

// acmeWithRepositories is acme with one entry, for every repository of the acme owner. Publish
// takes only one entry that writes until 4-merges lands (config's notSupportedYet), so the
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
