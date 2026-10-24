package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

// cliConfig is the CLI's own settings file, $XDG_CONFIG_HOME/goal/config.json (default
// ~/.config/goal/config.json, mode 0600). Every key is optional.
type cliConfig struct {
	// APIURL is goald's API when GOAL_API_URL is not set.
	APIURL string `json:"api_url,omitempty"`
	// Org is the organization (slug or id) commands act in when neither --org nor GOAL_ORG says.
	Org string `json:"org,omitempty"`
	// CredentialStore is where goal login keeps credentials: keychain (the OS keychain) or file
	// (credentials.json beside this file). Unset until someone signs in.
	CredentialStore string `json:"credential_store,omitempty"`
}

// configPath is where the CLI keeps its settings.
func configPath(getenv func(string) string) (string, error) {
	dir := strings.TrimSpace(getenv("XDG_CONFIG_HOME"))
	if dir == "" {
		home := strings.TrimSpace(getenv("HOME"))
		if home == "" {
			return "", errors.New("neither XDG_CONFIG_HOME nor HOME is set; the CLI cannot find its config file")
		}

		dir = filepath.Join(home, ".config")
	}

	return filepath.Join(dir, "goal", "config.json"), nil
}

// loadConfig reads the settings file; a missing file is the zero config.
func loadConfig(getenv func(string) string) (cliConfig, error) {
	path, err := configPath(getenv)
	if err != nil {
		return cliConfig{}, err
	}

	raw, err := os.ReadFile(path) //nolint:gosec // the user's own config file, at the path the user's environment names
	if errors.Is(err, fs.ErrNotExist) {
		return cliConfig{}, nil
	}

	if err != nil {
		return cliConfig{}, fmt.Errorf("read %s: %w", path, err)
	}

	var cfg cliConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cliConfig{}, fmt.Errorf("%s is not valid JSON: %w", path, err)
	}

	return cfg, nil
}

// saveConfig writes the settings file readable by its owner only, replacing it atomically.
func saveConfig(getenv func(string) string, cfg cliConfig) (string, error) {
	path, err := configPath(getenv)
	if err != nil {
		return "", err
	}

	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}

	return path, writePrivateFile(path, append(raw, '\n'))
}

// writePrivateFile replaces the file at path atomically with raw, readable by its owner only, in
// a directory only its owner may enter (created if missing).
func writePrivateFile(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}

	// CreateTemp makes the file with mode 0600, so the content is never readable by others.
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	_, werr := tmp.Write(raw)
	cerr := tmp.Close()

	if err := errors.Join(werr, cerr, os.Chmod(tmp.Name(), 0o600), os.Rename(tmp.Name(), path)); err != nil {
		_ = os.Remove(tmp.Name())

		return fmt.Errorf("write %s: %w", path, err)
	}

	return nil
}

// orgClient is goald's API for the organization a command acts in.
type orgClient struct {
	*gen.ClientWithResponses

	// org is the organization's slug or id, as the command resolved it.
	org string
}

// addOrgFlag adds --org to a command's flags.
func addOrgFlag(fs *flag.FlagSet) *string {
	return fs.String("org", "", "the organization's slug or id")
}

// connect opens goald's API and resolves the organization the command acts in.
func connect(ctx context.Context, getenv func(string) string, orgFlag string) (orgClient, error) {
	c, own, err := openAPI(getenv, osKeychain{})
	if err != nil {
		return orgClient{}, err
	}

	org, err := resolveOrg(ctx, c, getenv, orgFlag, own)
	if err != nil {
		return orgClient{}, err
	}

	return orgClient{ClientWithResponses: c, org: org}, nil
}

// envOrg names the organization commands act in, after --org.
const envOrg = "GOAL_ORG"

// resolveOrg finds the organization a command acts in: --org, then GOAL_ORG, then org in the
// config file (not when ownOrg: an API key or a personal token, from GOAL_API_KEY or goal login
// --token, acts in its own organization, the only one it lists), then the install's only
// organization. Otherwise it fails, listing them.
func resolveOrg(ctx context.Context, c *gen.ClientWithResponses, getenv func(string) string, orgFlag string, ownOrg bool) (string, error) {
	if org := strings.TrimSpace(orgFlag); org != "" {
		return org, nil
	}

	if org := strings.TrimSpace(getenv(envOrg)); org != "" {
		return org, nil
	}

	cfg, err := loadConfig(getenv)
	if err != nil {
		return "", err
	}

	if cfg.Org != "" && !ownOrg {
		return cfg.Org, nil
	}

	orgs, err := listOrgs(ctx, c)
	if err != nil {
		return "", err
	}

	switch len(orgs) {
	case 1:
		return orgs[0].Slug, nil
	case 0:
		return "", errors.New("goald has no organization; create one with `goal org create <slug> --name N`")
	default:
		slugs := make([]string, 0, len(orgs))
		for _, o := range orgs {
			slugs = append(slugs, o.Slug)
		}

		return "", fmt.Errorf("which organization? pass --org, set GOAL_ORG or run `goal org use <slug>`; there are %s",
			strings.Join(slugs, ", "))
	}
}

func listOrgs(ctx context.Context, c *gen.ClientWithResponses) ([]gen.OrgSummary, error) {
	res, err := c.ListOrgsWithResponse(ctx)
	if err != nil {
		return nil, apiDown(err)
	}

	if res.JSON200 == nil {
		return nil, problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	return res.JSON200.Orgs, nil
}

// getOrg reads one organization by slug or id.
func getOrg(ctx context.Context, c *gen.ClientWithResponses, ref string) (*gen.Org, error) {
	res, err := c.GetOrgWithResponse(ctx, ref)
	if err != nil {
		return nil, apiDown(err)
	}

	if res.JSON200 == nil {
		return nil, problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	return res.JSON200, nil
}

// orgCommand runs goal org list | use <slug> | create <slug> --name N [--mark M] |
// update <slug> [--name N] [--slug S] [--mark M] [--max-slots N] | delete <slug> [--yes].
func orgCommand(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) == 0 {
		return errUsage
	}

	c, own, err := openAPI(getenv, osKeychain{})
	if err != nil {
		return err
	}

	switch cmd, rest := args[0], args[1:]; cmd {
	case "list":
		if len(rest) != 0 {
			return errUsage
		}

		return orgList(ctx, out, c, getenv, own)
	case "use":
		if len(rest) != 1 {
			return errUsage
		}

		return orgUse(ctx, out, c, getenv, rest[0])
	case "create":
		return orgCreate(ctx, out, c, rest)
	case "update":
		return orgUpdate(ctx, out, c, rest)
	case "delete":
		return orgDelete(ctx, out, c, rest)
	default:
		return errUsage
	}
}

func orgList(ctx context.Context, out *printer, c *gen.ClientWithResponses, getenv func(string) string, ownOrg bool) error {
	orgs, err := listOrgs(ctx, c)
	if err != nil {
		return err
	}

	// The current organization, as a command without --org would resolve it; none when that
	// needs a choice.
	current, _ := resolveOrg(ctx, c, getenv, "", ownOrg)

	for _, o := range orgs {
		mark := " "
		if current == o.Slug || current == o.Id {
			mark = "*"
		}

		out.printf("%s %-24s %-24s %d factories\n", mark, o.Name, o.Slug, o.FactoryCount)
	}

	return nil
}

func orgUse(ctx context.Context, out *printer, c *gen.ClientWithResponses, getenv func(string) string, ref string) error {
	o, err := getOrg(ctx, c, ref)
	if err != nil {
		return err
	}

	cfg, err := loadConfig(getenv)
	if err != nil {
		return err
	}

	cfg.Org = o.Slug

	path, err := saveConfig(getenv, cfg)
	if err != nil {
		return err
	}

	out.printf("✓ commands act in %s (%s) from now on; saved in %s\n", o.Name, o.Slug, path)

	return nil
}

func orgCreate(ctx context.Context, out *printer, c *gen.ClientWithResponses, args []string) error {
	fs := newFlags("org create")
	name := fs.String("name", "", "the organization's name")
	mark := fs.String("mark", "", "one or two characters for the organization rail (default: the name's initials)")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 || strings.TrimSpace(*name) == "" {
		return errUsage
	}

	body := gen.NewOrg{Slug: pos[0], Name: strings.TrimSpace(*name)}
	if *mark != "" {
		body.Mark = mark
	}

	res, err := c.CreateOrgWithResponse(ctx, body)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON201 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	out.printf("✓ created %s (%s, mark %s); act in it with --org %s or `goal org use %s`\n",
		res.JSON201.Name, res.JSON201.Slug, res.JSON201.Mark, res.JSON201.Slug, res.JSON201.Slug)

	return nil
}

func orgUpdate(ctx context.Context, out *printer, c *gen.ClientWithResponses, args []string) error {
	fs := newFlags("org update")
	name := fs.String("name", "", "the organization's new name")
	slug := fs.String("slug", "", "the organization's new slug (links with the old one stop working)")
	mark := fs.String("mark", "", "one or two characters for the organization rail")
	maxSlots := fs.Int("max-slots", 0, "how many agents may run at once across its factories (0: no cap)")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	var body gen.OrgSettingsPatch

	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "name":
			body.Name = new(strings.TrimSpace(*name))
		case "slug":
			body.Slug = slug
		case "mark":
			body.Mark = mark
		case "max-slots":
			body.Slots = &gen.OrgSlotsPatch{}
			if *maxSlots != 0 {
				body.Slots.Max = maxSlots
			}
		}
	})

	if body == (gen.OrgSettingsPatch{}) {
		return errUsage
	}

	res, err := c.UpdateOrgSettingsWithResponse(ctx, pos[0], body)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	s := res.JSON200

	slots := "no slot cap"
	if s.Slots.Max != nil {
		slots = fmt.Sprintf("at most %d agents at once", *s.Slots.Max)
	}

	out.printf("✓ %s (%s, mark %s): %s\n", s.Name, s.Slug, s.Mark, slots)

	return nil
}

func orgDelete(ctx context.Context, out *printer, c *gen.ClientWithResponses, args []string) error {
	fs := newFlags("org delete")
	yes := fs.Bool("yes", false, "delete without asking to type the name")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	o, err := getOrg(ctx, c, pos[0])
	if err != nil {
		return err
	}

	confirm := o.Name

	if !*yes {
		out.printf("This deletes %s (%s) and its %d factories with their history. Type its name to confirm: ", o.Name, o.Slug, o.FactoryCount)

		if confirm, err = readLine(out.in); err != nil {
			return err
		}
	}

	if confirm != o.Name {
		return fmt.Errorf("%q is not %q; nothing was deleted", confirm, o.Name)
	}

	res, err := c.DeleteOrgWithResponse(ctx, o.Slug, gen.OrgDeletion{Confirm: confirm})
	if err != nil {
		return apiDown(err)
	}

	if res.JSON202 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	out.printf("✓ deleted %s (%s); it is purged after %s\n", o.Name, o.Slug, res.JSON202.PurgeAfter.Local().Format(time.RFC1123))

	return nil
}

// readLine reads one line of the terminal's input, without its line ending.
func readLine(in io.Reader) (string, error) {
	if in == nil {
		return "", errors.New("no input to confirm with; pass --yes")
	}

	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && (!errors.Is(err, io.EOF) || line == "") {
		return "", fmt.Errorf("read the confirmation: %w", err)
	}

	return strings.TrimRight(line, "\r\n"), nil
}
