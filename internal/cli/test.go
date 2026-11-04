package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

// cmdTest is goal test.
const cmdTest = "test"

// testCommand is goal test: the factory's kept tests (ADR 0039 decision 12), past items a person
// keeps at a station they passed the first time, which every change that reaches the station is
// tried on: kept, listed with each one's last result, and removed.
func testCommand(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) > 0 {
		if handler, ok := map[string]command{"keep": testKeep, subList: testList, "rm": testRemove}[args[0]]; ok {
			return handler(ctx, out, getenv, args[1:])
		}
	}

	return errUsage
}

// testConflicts are the one-line messages of the 409 codes keepTest documents.
var testConflicts = map[string]string{
	"not-passed-first-time": "the item did not pass that station the first time, so it can't be kept as a test there",
	"kept-test-exists":      "the item is kept as a test at that station already: goal test list shows it",
}

// testKeep is goal test keep <item> --station S [--factory F]: keep the item as a test at the
// station.
func testKeep(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("test keep")
	orgFlag := addOrgFlag(fs)
	factory := fs.String("factory", "", "the factory (name or id); default the organization's only one")
	station := fs.String("station", "", "the station the item passed the first time")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 || *station == "" {
		return errUsage
	}

	number, err := strconv.ParseInt(pos[0], 10, 64)
	if err != nil || number < 1 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	f, err := commandFactory(ctx, c, *factory)
	if err != nil {
		return err
	}

	res, err := c.KeepTestWithResponse(ctx, c.org, f, gen.KeepTestJSONRequestBody{Item: number, Station: *station})
	if err != nil {
		return apiDown(err)
	}

	if res.JSON201 == nil {
		if res.HTTPResponse != nil && res.HTTPResponse.StatusCode == http.StatusConflict {
			if msg, ok := testConflicts[problemCode(res.ApplicationproblemJSONDefault)]; ok {
				return errors.New(msg)
			}
		}

		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	k := res.JSON201
	out.printf("kept #%d %s at %s as test %s\n", k.Item.Number, oneLine(k.Item.Title), k.Station, k.Id)

	return nil
}

// testList is goal test list [--factory F]: the factory's kept tests by station, each with its
// item, its last result, who kept it and its id.
func testList(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("test list")
	orgFlag := addOrgFlag(fs)
	factory := fs.String("factory", "", "the factory (name or id); default the organization's only one")

	if pos, err := parseInterspersed(fs, args); err != nil || len(pos) != 0 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	f, err := commandFactory(ctx, c, *factory)
	if err != nil {
		return err
	}

	res, err := c.ListKeptTestsWithResponse(ctx, c.org, f)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	if len(res.JSON200.Tests) == 0 {
		out.printf("no kept tests\n")

		return nil
	}

	var b bytes.Buffer

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "STATION\tITEM\tTITLE\tLAST RESULT\tKEPT BY\tID")

	for _, k := range res.JSON200.Tests {
		_, _ = fmt.Fprintf(tw, "%s\t#%d\t%s\t%s\t%s\t%s\n", k.Station, k.Item.Number, oneLine(k.Item.Title), lastResult(k.LastResult), k.KeptBy, k.Id)
	}

	_ = tw.Flush()

	out.printf("%s", b.String())

	return nil
}

// lastResult is a kept test's last result in words: "passed 2026-11-04 09:00", or "never run".
func lastResult(l *gen.KeptTestLastResult) string {
	if l == nil {
		return "never run"
	}

	return string(l.Result) + " " + l.At.UTC().Format(time.DateOnly+" 15:04")
}

// testRemove is goal test rm <id> [--factory F]: remove the kept test; its item stays.
func testRemove(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("test rm")
	orgFlag := addOrgFlag(fs)
	factory := fs.String("factory", "", "the factory (name or id); default the organization's only one")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 || pos[0] == "" {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	f, err := commandFactory(ctx, c, *factory)
	if err != nil {
		return err
	}

	res, err := c.RemoveKeptTestWithResponse(ctx, c.org, f, pos[0])
	if err != nil {
		return apiDown(err)
	}

	if res.StatusCode() != http.StatusNoContent {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	out.printf("removed kept test %s\n", pos[0])

	return nil
}
