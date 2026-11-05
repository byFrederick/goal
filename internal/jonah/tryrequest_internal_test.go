package jonah

import (
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
)

// A try starts from its run's first recorded repository with a branch head, at that commit (ADR
// 0041 decision 17): a row recorded without a repository (before 00131) is the factory's. A run
// with no branch head, or whose repository the factory has not, cannot be tried.
func TestTries_RequestFromRecordedRepositories(t *testing.T) {
	t.Parallel()

	const agent, station = "engineer", "build"

	line := domain.Line{
		Runtime:  domain.Runtime{Repository: domain.Repository{URL: "https://github.com/acme/app.git"}},
		Agents:   []domain.AgentSpec{{Name: agent}},
		Stations: []domain.Station{{ID: station, Kind: domain.KindAgent, Agent: agent}},
	}
	try := domain.PastTry{Station: station, Model: "claude-sonnet-5-5"}
	head, other := strings.Repeat("c", 40), strings.Repeat("d", 40)

	for name, c := range map[string]struct {
		repos []domain.RunRepository
		why   string
	}{
		"its repository":     {[]domain.RunRepository{{Repo: "github.com/acme/app", BranchHead: head}}, ""},
		"backfilled row":     {[]domain.RunRepository{{BranchHead: head}}, ""},
		"first with a head":  {[]domain.RunRepository{{Repo: "github.com/acme/app", PushedHead: other}, {BranchHead: head}}, ""},
		"no branch head":     {[]domain.RunRepository{{Repo: "github.com/acme/app", PushedHead: other}}, "recorded no commit"},
		"another repository": {[]domain.RunRepository{{Repo: "github.com/acme/web", BranchHead: head}}, "no repository github.com/acme/web"},
	} {
		orig := domain.Run{ID: "run-1", Agent: agent, Input: domain.RunInput{Repositories: c.repos}}

		req, why := tryRequest(line, try, orig)
		if c.why != "" {
			if !strings.Contains(why, c.why) {
				t.Errorf("%s: why = %q; want it to say %q", name, why, c.why)
			}

			continue
		}

		if why != "" || req.Commit != head || req.Repository.URL != line.Runtime.Repository.URL {
			t.Errorf("%s: request %+v, why %q; want the factory's repository at %s", name, req, why, head)
		}
	}
}
