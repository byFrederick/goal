package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

// codeDraftRequestOpen is goald's refusal of a second request while one is queued or running.
const codeDraftRequestOpen = "draft-request-open"

// factoryAsk is goal factory ask <factory> "<text>" [--agent A] [--wait]: queue a draft request
// (ADR 0021), which goald runs in a box that changes the factory's draft and never publishes;
// with --wait, read it every interval until it ends and print what it changed.
func factoryAsk(ctx context.Context, out *printer, getenv func(string) string, args []string, every time.Duration) error {
	fs := newFlags("factory ask")
	org := addOrgFlag(fs)
	agent := fs.String("agent", "", "the agent of the definition the request is about")
	wait := fs.Bool("wait", false, "wait until the request is done or failed and print what it changed")

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

	if res.JSON201 == nil {
		if p := res.ApplicationproblemJSONDefault; problemCode(p) == codeDraftRequestOpen {
			return fmt.Errorf("refused: %s already has a queued or running request, and a factory runs one request at a time; wait for it to end, then ask again", factory)
		}

		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	r := res.JSON201
	out.printf("asked %s: request %s is %s\n", factory, r.Id, r.Status)

	if !*wait {
		out.printf("goald changes the draft when it runs; --wait waits for it\n")

		return nil
	}

	if r, err = awaitDraftRequest(ctx, c, factory, r.Id, every); err != nil {
		return err
	}

	return printDraftRequestEnd(out, factory, r)
}

// awaitDraftRequest reads the request every interval until it is done or failed.
func awaitDraftRequest(ctx context.Context, c orgClient, factory, id string, every time.Duration) (*gen.DraftRequest, error) {
	t := time.NewTicker(every)
	defer t.Stop()

	for {
		res, err := c.GetDraftRequestWithResponse(ctx, c.org, factory, id)
		if err != nil {
			return nil, waitingForDraftRequest(ctx, apiDown(err))
		}

		if res.JSON200 == nil {
			return nil, problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
		}

		if s := res.JSON200.Status; s == gen.DraftRequestStatusDone || s == gen.DraftRequestStatusFailed {
			return res.JSON200, nil
		}

		select {
		case <-ctx.Done():
			return nil, waitingForDraftRequest(ctx, nil)
		case <-t.C:
		}
	}
}

// waitingForDraftRequest ends a wait cut short by Ctrl-C: the request keeps running without us.
func waitingForDraftRequest(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return errors.New("stopped waiting; the request keeps running in goald")
	}

	return err
}

// printDraftRequestEnd prints how the request ended: its summary and the paths it changed in the
// draft, or, for a failed request, its error as the command's.
func printDraftRequestEnd(out *printer, factory string, r *gen.DraftRequest) error {
	out.printf("request %s is %s: %s, %s\n", r.Id, r.Status, plural(r.Turns, "turn"), usd(r.CostUsd))

	if r.Summary != "" {
		out.printf("%s\n", r.Summary)
	}

	if r.Status == gen.DraftRequestStatusFailed {
		return fmt.Errorf("request %s failed: %s", r.Id, cmp.Or(r.Error, "goald gave no reason"))
	}

	if len(r.ChangedPaths) == 0 {
		out.printf("the draft did not change\n")

		return nil
	}

	out.printf("changed in the draft:\n")

	for _, p := range r.ChangedPaths {
		out.printf("  %s\n", p)
	}

	out.printf("review the draft and publish it with: goal factory publish %s\n", factory)

	return nil
}
