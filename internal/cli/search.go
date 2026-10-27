package cli

import (
	"context"
	"strconv"
	"strings"

	"github.com/byFrederick/goal/internal/api/gen"
)

// search is goal search <query>: what the organization's search finds among its items, agents,
// factories and connections, by group; --json writes goald's answer as it came.
func search(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("search")
	orgFlag := addOrgFlag(fs)
	limit := fs.Int("limit", 0, "how many results per group, 1 to 20 (default 5)")
	asJSON := fs.Bool("json", false, "print goald's answer as it came")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	params := &gen.SearchOrgParams{Q: pos[0]}
	if *limit > 0 {
		params.Limit = limit
	}

	res, err := c.SearchOrgWithResponse(ctx, c.org, params)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	if *asJSON {
		out.printf("%s", res.Body)

		return nil
	}

	printSearch(out, res.JSON200)

	return nil
}

// printSearch prints each group that found something under its heading, in the order Items,
// Agents, Factories, Connections, or "No results" when none did.
func printSearch(out *printer, r *gen.OrgSearch) {
	var groups []string

	group := func(heading string, lines []string) {
		if len(lines) > 0 {
			groups = append(groups, heading+"\n  "+strings.Join(lines, "\n  ")+"\n")
		}
	}

	group("Items", hitLines(r.Items, func(h gen.SearchItem) []string {
		return []string{h.FactoryName + "#" + strconv.FormatInt(h.Number, 10), h.Title, string(h.State)}
	}))
	group("Agents", hitLines(r.Agents, func(h gen.AgentHit) []string {
		return []string{h.Name, strings.Join(h.Stations, ","), h.FactoryName}
	}))
	group("Factories", hitLines(r.Factories, func(h gen.FactoryHit) []string { return []string{h.Name, h.Title} }))
	group("Connections", hitLines(r.Connections, func(h gen.ConnectionHit) []string { return []string{h.Name, h.Kind} }))

	if len(groups) == 0 {
		out.printf("No results\n")

		return
	}

	out.printf("%s", strings.Join(groups, "\n"))
}

// hitLines is one line per hit, its fields separated by two spaces.
func hitLines[H any](hits []H, fields func(H) []string) []string {
	lines := make([]string, 0, len(hits))

	for _, h := range hits {
		parts := make([]string, 0, 3)

		for _, f := range fields(h) {
			if f != "" {
				parts = append(parts, oneLine(f))
			}
		}

		lines = append(lines, strings.Join(parts, "  "))
	}

	return lines
}
