package main

import (
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// fixture loads testdata/repo, a small repository whose registry matches its reads, with the real
// schema and the reference generated from its registry.
func fixture(t *testing.T) fstest.MapFS {
	t.Helper()

	m := fstest.MapFS{}

	src := os.DirFS("testdata/repo")

	err := fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		b, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}

		m[p] = &fstest.MapFile{Data: b}

		return nil
	})
	if err != nil {
		t.Fatalf("load the fixture: %v", err)
	}

	schema, err := os.ReadFile("../../" + schemaFile)
	if err != nil {
		t.Fatalf("read the schema: %v", err)
	}

	m[schemaFile] = &fstest.MapFile{Data: schema}

	return m
}

// checkFixture applies edit to the fixture, regenerates its reference unless keepReference, and
// checks it.
func checkFixture(t *testing.T, edit func(m fstest.MapFS), keepReference bool) []string {
	t.Helper()

	m := fixture(t)
	if edit != nil {
		edit(m)
	}

	r := repoOf(m)

	if !keepReference {
		reg, err := loadRegistry(r)
		if err != nil {
			t.Fatalf("load the registry: %v", err)
		}

		m[referenceFile] = &fstest.MapFile{Data: []byte(reference(reg))}
		r = repoOf(m)
	}

	problems, err := check(r)
	if err != nil {
		t.Fatalf("check: %v", err)
	}

	out := make([]string, len(problems))
	for i, p := range problems {
		out[i] = p.String()
	}

	return out
}

func repoOf(m fstest.MapFS) repo {
	files := make([]string, 0, len(m))
	for f := range m {
		files = append(files, f)
	}

	slices.Sort(files)

	return repo{fsys: m, files: files}
}

func set(m fstest.MapFS, name, content string) { m[name] = &fstest.MapFile{Data: []byte(content)} }

func replace(t *testing.T, m fstest.MapFS, name, old, repl string) {
	t.Helper()

	f, ok := m[name]
	if !ok || !strings.Contains(string(f.Data), old) {
		t.Fatalf("%s does not contain %q", name, old)
	}

	set(m, name, strings.Replace(string(f.Data), old, repl, 1))
}

// undeclare removes a variable's entry from the registry.
func undeclare(t *testing.T, m fstest.MapFS, name string) {
	t.Helper()

	re := regexp.MustCompile(`(?s)  - name: ` + name + `\n.*?(\n  - name: |\n*$)`)

	src := string(m[registryFile].Data)
	if !re.MatchString(src) {
		t.Fatalf("the registry does not declare %s", name)
	}

	set(m, registryFile, re.ReplaceAllString(src, "$1"))
}

// declare appends an entry to the registry (a name after every fixture name keeps the order).
func declare(m fstest.MapFS, yaml string) {
	set(m, registryFile, string(m[registryFile].Data)+yaml)
}

func wantProblems(t *testing.T, got, want []string) {
	t.Helper()

	if !slices.Equal(got, want) {
		t.Errorf("problems:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

func TestCheck_FixtureIsClean(t *testing.T) {
	t.Parallel()

	wantProblems(t, checkFixture(t, nil, false), nil)
}

func TestCheck_UndeclaredRead(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		name string
		want string
	}{
		"go constant":         {"GOAL_LISTEN", "cmd/goald/settings.go:11: GOAL_LISTEN is read but not declared in contracts/env.yaml"},
		"go or closure":       {"GOAL_MODE", "cmd/goald/settings.go:12: GOAL_MODE is read but not declared in contracts/env.yaml"},
		"go os.Getenv":        {"GOAL_OS", "cmd/goald/settings.go:13: GOAL_OS is read but not declared in contracts/env.yaml"},
		"go lookup":           {"GOAL_LOOKUP", "cmd/goald/settings.go:14: GOAL_LOOKUP is read but not declared in contracts/env.yaml"},
		"go env: reference":   {"GOAL_TOKEN", "cmd/goald/settings.go:7: GOAL_TOKEN is read but not declared in contracts/env.yaml"},
		"go internal package": {"GOAL_SHARED", "internal/store/store.go:5: GOAL_SHARED is read but not declared in contracts/env.yaml"},
		"ts get":              {"AUTH_SECRET", "apps/auth/src/settings.ts:2: AUTH_SECRET is read but not declared in contracts/env.yaml"},
		"ts process.env":      {"AUTH_URL", "apps/auth/src/settings.ts:3: AUTH_URL is read but not declared in contracts/env.yaml"},
		"ts required":         {"WORKER_TOKEN", "apps/worker/src/run.ts:1: WORKER_TOKEN is read but not declared in contracts/env.yaml"},
		"ts env index":        {"WORKER_DIR", "apps/worker/src/run.ts:2: WORKER_DIR is read but not declared in contracts/env.yaml"},
		"ts env field":        {"CF_LOG", "deploy/cloudflare/src/env.ts:2: CF_LOG is read but not declared in contracts/env.yaml"},
		"wrangler secret":     {"CF_SECRET", "deploy/cloudflare/wrangler.jsonc secrets_store_secrets: CF_SECRET is read but not declared in contracts/env.yaml"},
		"compose":             {"GOAL_DOCKER_GID", "compose.yaml:3: GOAL_DOCKER_GID is read but not declared in contracts/env.yaml"},
		"taskfile":            {"GOAL_FACTORIES_DIR", "Taskfile.yml:4: GOAL_FACTORIES_DIR is read but not declared in contracts/env.yaml"},
		"included taskfile":   {"GOAL_WEB_PORT", "apps/web/Taskfile.yml:6: GOAL_WEB_PORT is read but not declared in contracts/env.yaml"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := checkFixture(t, func(m fstest.MapFS) {
				undeclare(t, m, tt.name)

				// .env.example shows some of them; that is its own problem, tested on its own.
				src := string(m[envExampleFile].Data)
				set(m, envExampleFile, regexp.MustCompile(`(?m)^.*`+tt.name+`=.*$`).ReplaceAllString(src, ""))
			}, false)

			wantProblems(t, got, []string{tt.want})
		})
	}

	t.Run("wrangler var", func(t *testing.T) {
		t.Parallel()

		got := checkFixture(t, func(m fstest.MapFS) {
			undeclare(t, m, "CF_LOG")
			replace(t, m, "deploy/cloudflare/src/env.ts", "log: env.CF_LOG, ", "")
		}, false)

		wantProblems(t, got, []string{"deploy/cloudflare/wrangler.jsonc vars: CF_LOG is read but not declared in contracts/env.yaml"})
	})
}

func TestCheck_UnreadDeclared(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		edit func(t *testing.T, m fstest.MapFS)
		want string
	}{
		"no read at all": {
			func(_ *testing.T, m fstest.MapFS) {
				declare(m, "  - name: ZZ_GONE\n    readers: [goald]\n    class: config\n    required: false\n    meaning: \"Nothing reads it.\"\n")
			},
			"contracts/env.yaml: ZZ_GONE is declared but nothing reads it",
		},
		"a read in a test does not count": {
			func(_ *testing.T, m fstest.MapFS) {
				declare(m, "  - name: ZZ_TEST\n    readers: [goald]\n    class: config\n    required: false\n    meaning: \"Only a test reads it.\"\n")
				set(m, "cmd/goald/zz_test.go", "package main\n\nimport \"os\"\n\nvar _ = os.Getenv(\"ZZ_TEST\")\n")
				set(m, "apps/auth/src/zz.test.ts", "process.env.ZZ_TEST;\n")
			},
			"contracts/env.yaml: ZZ_TEST is declared but nothing reads it",
		},
		"a removed read": {
			func(t *testing.T, m fstest.MapFS) {
				t.Helper()
				replace(t, m, "cmd/goald/settings.go", `os.Getenv("GOAL_OS")`, `""`)
			},
			"contracts/env.yaml: GOAL_OS is declared but nothing reads it",
		},
		"an indirect name gone from its file": {
			func(t *testing.T, m fstest.MapFS) {
				t.Helper()

				replace(t, m, "deploy/cloudflare/src/env.ts", "['CF_INDIRECT']", "[]")
			},
			"contracts/env.yaml: CF_INDIRECT is declared but nothing reads it (its name does not appear quoted in deploy/cloudflare/src/env.ts)",
		},
		"a compose variable gone": {
			func(t *testing.T, m fstest.MapFS) {
				t.Helper()

				replace(t, m, composeFile, `group_add: ["${GOAL_DOCKER_GID:-999}"]`, "group_add: []")
			},
			"contracts/env.yaml: GOAL_DOCKER_GID is declared but nothing reads it",
		},
		"a variable gone from an included Taskfile": {
			func(t *testing.T, m fstest.MapFS) {
				t.Helper()

				replace(t, m, "apps/web/Taskfile.yml", `--port "${GOAL_WEB_PORT}"`, "")
			},
			"contracts/env.yaml: GOAL_WEB_PORT is declared but nothing reads it",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			wantProblems(t, checkFixture(t, func(m fstest.MapFS) { tt.edit(t, m) }, false), []string{tt.want})
		})
	}
}

func TestCheck_WrongReader(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		edit func(t *testing.T, m fstest.MapFS)
		want []string
	}{
		"goald read, entry lists the CLI": {
			func(t *testing.T, m fstest.MapFS) {
				t.Helper()

				replace(t, m, registryFile, "  - name: GOAL_LISTEN\n    readers: [goald]", "  - name: GOAL_LISTEN\n    readers: [goal]")
			},
			[]string{"cmd/goald/settings.go:11: GOAL_LISTEN is read by goald, which its entry in contracts/env.yaml does not list among its readers"},
		},
		"worker read, entry lists auth": {
			func(t *testing.T, m fstest.MapFS) {
				t.Helper()

				replace(t, m, registryFile, "  - name: WORKER_DIR\n    readers: [worker]", "  - name: WORKER_DIR\n    readers: [auth]")
			},
			[]string{"apps/worker/src/run.ts:2: WORKER_DIR is read by worker, which its entry in contracts/env.yaml does not list among its readers"},
		},
		"an internal read needs only an entry": {
			func(t *testing.T, m fstest.MapFS) {
				t.Helper()

				replace(t, m, registryFile, "  - name: GOAL_SHARED\n    readers: [goald]", "  - name: GOAL_SHARED\n    readers: [goal-egress]")
			},
			nil,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			wantProblems(t, checkFixture(t, func(m fstest.MapFS) { tt.edit(t, m) }, false), tt.want)
		})
	}
}

func TestCheck_SecretBindingClass(t *testing.T) {
	t.Parallel()

	got := checkFixture(t, func(m fstest.MapFS) {
		replace(t, m, registryFile, "  - name: CF_SECRET\n    readers: [control-worker]\n    class: secret", "  - name: CF_SECRET\n    readers: [control-worker]\n    class: config")
	}, false)

	wantProblems(t, got, []string{"contracts/env.yaml: CF_SECRET is a Secrets Store binding in deploy/cloudflare/wrangler.jsonc but is classified config, not secret"})
}

func TestCheck_Order(t *testing.T) {
	t.Parallel()

	got := checkFixture(t, func(m fstest.MapFS) {
		declare(m, "  - name: AUTH_URL\n    readers: [auth]\n    class: public\n    required: false\n    meaning: \"Twice.\"\n")
	}, false)

	wantProblems(t, got, []string{
		"contracts/env.yaml: AUTH_URL is declared twice",
		"contracts/env.yaml: AUTH_URL is out of order: the variables are sorted by name",
	})
}

func TestScanGo_ReaderFuncsAndConsts(t *testing.T) {
	t.Parallel()

	src := `package main

import "os"

const (
	nameEnv   = "GOAL_CONST"
	prefix    = "GOAL_"
	joinedEnv = prefix + "JOINED"
	ref       = "env:GOAL_REF"
	notAName  = "env:lower"
)

func f(getenv func(string) string, lookup func(string) (string, bool), name string) {
	or := func(n, d string) string { return d }
	_ = os.Getenv("GOAL_OS")
	_, _ = os.LookupEnv("GOAL_LOOKUPENV")
	_ = getenv(nameEnv)
	_, _ = lookup(joinedEnv)
	_ = or("GOAL_OR", "x")
	_ = getenv(name)               // computed: not seen
	_ = other("GOAL_OTHER")        // not a reader
	_ = getenv("lower_case")       // not a variable's name
	_ = os.Getenv(fmtName("GOAL")) // not a constant
}
`

	reads, err := scanGo(map[string][]byte{"cmd/goald/a.go": []byte(src)})
	if err != nil {
		t.Fatalf("scanGo: %v", err)
	}

	var got []string

	for _, rd := range reads {
		if rd.reader != readerGoald {
			t.Errorf("%s at %s: reader = %q, want goald", rd.name, rd.where, rd.reader)
		}

		got = append(got, rd.name)
	}

	slices.Sort(got)

	want := []string{"GOAL_CONST", "GOAL_JOINED", "GOAL_LOOKUPENV", "GOAL_OR", "GOAL_OS", "GOAL_REF"}
	if !slices.Equal(got, want) {
		t.Errorf("reads = %v, want %v", got, want)
	}
}

func TestScanTS_Patterns(t *testing.T) {
	t.Parallel()

	src := `// env.IN_COMMENT
 * env.IN_DOC
const a = process.env.TS_PROCESS;
const b = env.TS_FIELD, c = env["TS_INDEX"];
const d = get('TS_GET');
const e = required(this.env, 'TS_REQUIRED');
const f = get('NOUNDERSCORE'), g = headers.get('x-goal'), h = env.get('TS_NOT');
`

	var got []string
	for _, rd := range scanTS("apps/worker/src/a.ts", []byte(src)) {
		got = append(got, rd.name)
	}

	slices.Sort(got)

	want := []string{"TS_FIELD", "TS_GET", "TS_INDEX", "TS_PROCESS", "TS_REQUIRED"}
	if !slices.Equal(got, want) {
		t.Errorf("reads = %v, want %v", got, want)
	}

	for _, p := range []string{"apps/worker/src/a.test.ts", "apps/auth/src/testing/db.ts", "apps/web/src/a.ts", "packages/api-client/src/a.ts"} {
		if isTSSource(p) {
			t.Errorf("isTSSource(%q) = true, want false", p)
		}
	}
}

func TestRegistry_Schema(t *testing.T) {
	t.Parallel()

	schema, err := os.ReadFile("../../" + schemaFile)
	if err != nil {
		t.Fatalf("read the schema: %v", err)
	}

	const valid = "  - name: GOAL_X\n    readers: [goald]\n    class: config\n    required: false\n    meaning: \"A setting.\"\n"

	tests := map[string]struct {
		entry   string
		wantErr string // "" for valid
	}{
		"valid":            {valid, ""},
		"indirect":         {valid + "    indirect: cmd/goald/settings.go\n", ""},
		"a default value":  {valid + "    default: \"8080\"\n", "default"},
		"a value":          {valid + "    value: x\n", "value"},
		"an example":       {valid + "    example: x\n", "example"},
		"unknown class":    {strings.Replace(valid, "class: config", "class: private", 1), "class"},
		"unknown reader":   {strings.Replace(valid, "[goald]", "[web]", 1), "readers"},
		"no readers":       {strings.Replace(valid, "[goald]", "[]", 1), "readers"},
		"lower-case name":  {strings.Replace(valid, "GOAL_X", "goal_x", 1), "name"},
		"no meaning":       {strings.Replace(valid, "    meaning: \"A setting.\"\n", "", 1), "meaning"},
		"required not set": {strings.Replace(valid, "    required: false\n", "", 1), "required"},
		"absolute indirect": {
			valid + "    indirect: /etc/x\n", "indirect",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := parseRegistry(schema, []byte("variables:\n"+tt.entry))

			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("parseRegistry: %v, want no error", err)
			case tt.wantErr != "" && err == nil:
				t.Errorf("parseRegistry: no error, want one naming %q", tt.wantErr)
			case tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr):
				t.Errorf("parseRegistry: %v, want an error naming %q", err, tt.wantErr)
			}
		})
	}

	t.Run("the repository's registry", func(t *testing.T) {
		t.Parallel()

		src, err := os.ReadFile("../../" + registryFile)
		if err != nil {
			t.Fatalf("read the registry: %v", err)
		}

		if _, err := parseRegistry(schema, src); err != nil {
			t.Errorf("parseRegistry: %v", err)
		}
	})
}

// exampleRegistry declares a config and a secret.
var exampleRegistry = registry{Variables: []entry{
	{Name: "GOAL_LISTEN", Readers: []string{readerGoald}, Class: classConfig, Meaning: "x"},
	{Name: "GOAL_TOKEN", Readers: []string{readerGoald}, Class: classSecret, Meaning: "x"},
	{Name: "GOAL_DATABASE_URL", Readers: []string{readerGoald}, Class: classSecret, Meaning: "x"},
}}

func TestEnvExample_Undeclared(t *testing.T) {
	t.Parallel()

	src := "# A comment that mentions GOAL_NOT_A_LINE=1 is not a variable.\n" +
		"GOAL_LISTEN=:8080\n" +
		"GOAL_UNKNOWN=x\n" +
		"# GOAL_COMMENTED=\n" +
		"#GOAL_LISTEN=:9090 GOAL_SECOND=1   # two on one line\n"

	got := checkEnvExample(exampleRegistry, []byte(src))

	want := []string{
		".env.example:3: GOAL_UNKNOWN is not declared in contracts/env.yaml",
		".env.example:4: GOAL_COMMENTED is not declared in contracts/env.yaml",
		".env.example:5: GOAL_SECOND is not declared in contracts/env.yaml",
	}

	var lines []string
	for _, p := range got {
		lines = append(lines, p.String())
	}

	wantProblems(t, lines, want)
}

func TestEnvExample_SecretValue(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		line string
		bad  bool
	}{
		"empty":                    {"GOAL_TOKEN=", false},
		"commented empty":          {"# GOAL_TOKEN=", false},
		"dev-only placeholder":     {"GOAL_TOKEN=goal-dev-only-token", false},
		"real-looking":             {"GOAL_TOKEN=ghp_0123456789abcdef", true},
		"commented real-looking":   {"# GOAL_TOKEN=s3cr3t-value", true},
		"url without a password":   {"GOAL_DATABASE_URL=postgres://goal@127.0.0.1:5432/goal", false},
		"url with dev-only":        {"GOAL_DATABASE_URL=postgres://goal:goal-dev-only@127.0.0.1:5432/goal", false},
		"url with a real password": {"GOAL_DATABASE_URL=postgres://goal:hunter2@db.example.com/goal", true},
		"a config value":           {"GOAL_LISTEN=:8080", false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := checkEnvExample(exampleRegistry, []byte(tt.line+"\n"))

			if tt.bad != (len(got) == 1 && strings.Contains(got[0].what, "is a secret")) || (!tt.bad && len(got) != 0) {
				t.Errorf("problems = %v, want a secret-value problem: %v", got, tt.bad)
			}
		})
	}
}

func TestReference_Stale(t *testing.T) {
	t.Parallel()

	t.Run("an edited reference is reported", func(t *testing.T) {
		t.Parallel()

		got := checkFixture(t, func(m fstest.MapFS) {
			set(m, referenceFile, "# Environment variables\n\nEdited by hand.\n")
		}, true)

		wantProblems(t, got, []string{"docs/reference/environment.md: differs from the one contracts/env.yaml generates: run task env:check -- -write"})
	})

	t.Run("a missing reference is reported", func(t *testing.T) {
		t.Parallel()

		got := checkFixture(t, nil, true)

		wantProblems(t, got, []string{"docs/reference/environment.md: differs from the one contracts/env.yaml generates: run task env:check -- -write"})
	})

	t.Run("one table per reader", func(t *testing.T) {
		t.Parallel()

		reg := registry{Variables: []entry{
			{Name: "AUTH_URL", Readers: []string{readerAuth}, Class: classPublic, Meaning: "Where | it is."},
			{Name: "GOAL_LISTEN", Readers: []string{readerGoald, readerCLI}, Class: classConfig, Required: true, Meaning: "The address."},
		}}

		got := reference(reg)

		for _, want := range []string{
			"## goald (`cmd/goald`)\n\n| Name | Class | Required | Meaning |\n| --- | --- | --- | --- |\n| `GOAL_LISTEN` | config | yes | The address. |\n",
			"## The goal CLI (`cmd/goal`)\n\n| Name | Class | Required | Meaning |\n| --- | --- | --- | --- |\n| `GOAL_LISTEN` | config | yes | The address. |\n",
			"| `AUTH_URL` | public | no | Where \\| it is. |\n",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("reference lacks %q:\n%s", want, got)
			}
		}

		if strings.Contains(got, "goal-egress") {
			t.Errorf("reference has a section for a reader with no variables:\n%s", got)
		}
	})
}

// TestRepository checks this checkout, as task env:check does.
func TestRepository(t *testing.T) {
	t.Parallel()

	if _, err := os.Stat("../../.git"); err != nil {
		t.Skip("not a git checkout")
	}

	var out strings.Builder
	if err := run(t.Context(), []string{"-C", "../.."}, &out); err != nil {
		t.Errorf("envcheck on the repository: %v\n%s", err, out.String())
	}
}
