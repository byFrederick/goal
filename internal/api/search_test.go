package api_test

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
)

const orgSearchPath = "/v1/orgs/acme/search"

// searchFor is the search path for q, with extra query parameters after it.
func searchFor(q string, extra ...string) string {
	return orgSearchPath + "?" + strings.Join(append([]string{"q=" + url.QueryEscape(q)}, extra...), "&")
}

// seedTitled creates an item titled title on line.
func (h *harness) seedTitled(t *testing.T, title string) domain.Item {
	t.Helper()

	it, err := h.store.CreateItem(inOrg(t), domain.NewItem{
		FactoryID: lineID, Type: typeFeature, Lane: laneAPI, Title: title, Station: stBuild,
		State: domain.StateQueued, Class: domain.ClassStandard, CreatedAt: t0,
	})
	if err != nil {
		t.Fatal(err)
	}

	return it
}

// searchLine renders a search's results as one line: each group's hits, best first.
func searchLine(s gen.OrgSearch) string {
	var items, agents, factories, conns []string

	for _, it := range s.Items {
		items = append(items, fmt.Sprintf("%s#%d %s", it.FactoryName, it.Number, it.Title))
	}

	for _, a := range s.Agents {
		agents = append(agents, fmt.Sprintf("%s/%s at %s", a.FactoryName, a.Name, strings.Join(a.Stations, ",")))
	}

	for _, f := range s.Factories {
		factories = append(factories, f.Name)
	}

	for _, c := range s.Connections {
		conns = append(conns, c.Name+" ("+c.Kind+")")
	}

	return fmt.Sprintf("items [%s] agents [%s] factories [%s] connections [%s]",
		strings.Join(items, "; "), strings.Join(agents, "; "), strings.Join(factories, "; "), strings.Join(conns, "; "))
}

// A viewer of acme alone finds nothing of line: no item, agent or factory; its owner finds both.
func TestSearchOrg_HidesFactoriesTheCallerCannotRead(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)
	h.seedTitled(t, "Fix the frontend build")

	if _, err := h.store.CreateItem(inOrg(t), domain.NewItem{
		FactoryID: acmeID, Type: typeFeature, Title: "Write the frontend guide", Station: "inbox",
		State: domain.StateQueued, Class: domain.ClassStandard, CreatedAt: t0,
	}); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct{ name, token, q, want string }{
		{
			"the viewer", viewerToken, "e",
			"items [acme#1 Write the frontend guide] agents [acme/writer at ] factories [acme] connections []",
		},
		{
			"the owner", ownerToken, "e",
			"items [line#1 Fix the frontend build; acme#1 Write the frontend guide] agents [acme/writer at ; line/backend at build; line/frontend at build] " +
				"factories [acme; line] connections []",
		},
		{"the viewer, line's agent", viewerToken, "frontend", "items [acme#1 Write the frontend guide] agents [] factories [] connections []"},
	} {
		got := decode[gen.OrgSearch](t, h.as(t, c.token, http.MethodGet, searchFor(c.q), ""), http.StatusOK)
		if line := searchLine(got); line != c.want {
			t.Errorf("%s searching %q:\n  %s\nwant\n  %s", c.name, c.q, line, c.want)
		}
	}
}

// Connections are found only with connection:read: a session member's, not a token without it.
func TestSearchOrg_ConnectionsNeedConnectionRead(t *testing.T) {
	t.Parallel()

	h := keyed(t)

	if _, err := h.conns.Create(domain.WithOrg(t.Context(), testOrg), domain.NewConnection{
		Name: kindGitHub, Kind: kindGitHub, Secrets: map[string]string{"token": githubToken},
	}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	created, _ := h.createToken(t, freshToken, laptop) // org:read and factory:read, no connection:read

	got := decode[gen.OrgSearch](t, h.request(t, http.MethodGet, searchFor("git"), "", bearer(created.Secret)), http.StatusOK)
	if got.Connections == nil || len(got.Connections) != 0 {
		t.Errorf("a token without connection:read finds connections %+v; want []", got.Connections)
	}

	got = decode[gen.OrgSearch](t, h.as(t, viewerToken, http.MethodGet, searchFor("git"), ""), http.StatusOK)
	if !slices.Equal(got.Connections, []gen.ConnectionHit{{Name: kindGitHub, Kind: kindGitHub}}) {
		t.Errorf("a member finds connections %+v; want github", got.Connections)
	}
}

// An exact number, with or without #, puts that item first; a title containing it follows.
func TestSearchOrg_FindsItemByNumber(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	for range 146 {
		h.seedTitled(t, "seeded")
	}

	h.seedTitled(t, "Item one four seven")
	h.seedTitled(t, "Open port 1470")

	for _, c := range []struct{ q, want string }{
		{"147", "line#147 Item one four seven; line#148 Open port 1470"},
		{"#147", "line#147 Item one four seven"},
		{" #147 ", "line#147 Item one four seven"},
	} {
		var got gen.OrgSearch
		if code := h.call(t, http.MethodGet, searchFor(c.q), "", &got); code != http.StatusOK {
			t.Fatalf("search %q = %d; want 200", c.q, code)
		}

		var items []string
		for _, it := range got.Items {
			items = append(items, fmt.Sprintf("%s#%d %s", it.FactoryName, it.Number, it.Title))
		}

		if line := strings.Join(items, "; "); line != c.want {
			t.Errorf("search %q items = %s; want %s", c.q, line, c.want)
		}
	}
}

// A % in q is a percent sign, not a wildcard.
func TestSearchOrg_PercentMatchesLiterally(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.seedTitled(t, "Cut cost by 50%")
	h.seedTitled(t, "Cut cost by 50 dollars")

	var got gen.OrgSearch
	if code := h.call(t, http.MethodGet, searchFor("50%"), "", &got); code != http.StatusOK {
		t.Fatalf("search = %d; want 200", code)
	}

	if len(got.Items) != 1 || got.Items[0].Title != "Cut cost by 50%" || got.Items[0].FactoryId != lineID || got.Items[0].Station != stBuild ||
		got.Items[0].State != gen.ItemStateQueued || got.Items[0].Id == "" {
		t.Errorf("items = %+v; want line's #1 alone", got.Items)
	}
}

// The limit bounds each group, and every group is an array even when nothing matches.
func TestSearchOrg_LimitsEachGroup(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	for range 3 {
		h.seedTitled(t, "seeded")
	}

	var got gen.OrgSearch
	if code := h.call(t, http.MethodGet, searchFor("seed", "limit=2"), "", &got); code != http.StatusOK {
		t.Fatalf("search = %d; want 200", code)
	}

	if len(got.Items) != 2 || got.Items[0].Number != 3 || got.Agents == nil || got.Factories == nil || got.Connections == nil {
		t.Errorf("search = %+v; want #3 and #2 and empty groups", got)
	}
}

func TestSearchOrg_RejectsBadQuery(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for _, path := range []string{
		orgSearchPath,
		searchFor(""),
		searchFor("   "),
		searchFor(strings.Repeat("a", 201)),
		searchFor("a", "limit=21"),
		searchFor("a", "limit=0"),
	} {
		res := h.request(t, http.MethodGet, path, "", nil)
		if p := problem(t, res); res.status != http.StatusBadRequest || p.Status != http.StatusBadRequest {
			t.Errorf("GET %s = %d %s; want a 400 problem", path, res.status, res.body)
		}
	}

	if code := h.call(t, http.MethodGet, searchFor(strings.Repeat("a", 200)), "", nil); code != http.StatusOK {
		t.Errorf("200 characters = %d; want 200", code)
	}
}
