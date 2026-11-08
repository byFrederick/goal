package cli

import (
	"context"
	"fmt"

	"github.com/byFrederick/goal/internal/api/gen"
)

const (
	// codeDraftRequestOpen is goald's refusal of a second request while one is queued or running.
	codeDraftRequestOpen = "draft-request-open"
	// codeNotBuilt is goald's refusal of every draft request until a runner for them is wired.
	codeNotBuilt = "not-built"
)

// factoryAsk is goal factory ask <factory> "<text>" [--agent A] [--wait]: ask for a draft request
// (ADR 0021). goald has no runner for draft requests yet and refuses each with 501 not-built, so
// the command prints that refusal and fails; --wait is accepted and never waits, because nothing
// is queued.
func factoryAsk(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("factory ask")
	org := addOrgFlag(fs)
	agent := fs.String("agent", "", "the agent of the definition the request is about")
	_ = fs.Bool("wait", false, "not built yet: goald refuses the request, so there is nothing to wait for")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 2 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *org)
	if err != nil {
		return err
	}

	factory, body := pos[0], gen.CreateDraftRequestJSONRequestBody{Text: pos[1]}
	if *agent != "" {
		body.Agent = agent
	}

	res, err := c.CreateDraftRequestWithResponse(ctx, c.org, factory, body)
	if err != nil {
		return apiDown(err)
	}

	if r := res.JSON201; r != nil {
		out.printf("asked %s: request %s is %s\n", factory, r.Id, r.Status)

		return nil
	}

	switch p := res.ApplicationproblemJSONDefault; problemCode(p) {
	case codeNotBuilt:
		return fmt.Errorf("refused: draft requests are not built yet; goald does not run them, so goal factory ask cannot change the draft of %s. Change it with goal factory pull and goal factory push", factory)
	case codeDraftRequestOpen:
		return fmt.Errorf("refused: %s already has a queued or running request, and a factory runs one request at a time; wait for it to end, then ask again", factory)
	}

	return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
}
