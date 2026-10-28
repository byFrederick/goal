## When you change

The judges API: `listJudges`, `getJudge`, `createJudge`, `updateJudge` and `tryJudge` under `/v1/orgs/{org}/factories/{factory}/judges` in `contracts/api/openapi.yaml`, their handlers and `api.JudgeDeps` in `internal/api/judges.go`, or `goal judge list` and `goal judge try` in `internal/cli/judge.go`

## Also change

`task api:gen`'s outputs; `TestJudges_*` in `internal/api/judges_test.go`, the probes in `internal/api/org_test.go` and `internal/api/testdata/access.golden` (`go test ./internal/api -run TwoOrgMatrix -update`); the front matter `judgeFile` renders when `contracts/definitions/evals.schema.json`'s judge changes, so a written judge still validates; `TestJudgeCommand_*` and the golden files `internal/cli/testdata/golden/judge*.txt` and the usage goldens; the `Judges` row of the Ports table and the HTTP API table in `docs/engineering/architecture.md`; the judges screens in `apps/web` once they call it

## Run

`task api:gen`, `go test -race ./internal/api ./internal/cli`, `task lint`
