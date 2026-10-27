package porttest_test

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/core/ports/porttest/judgescoretest"
)

// The fake must keep the database's judge score rules, or the judge loop's tests prove nothing.
func TestJudgeScores_Conformance(t *testing.T) {
	t.Parallel()

	judgescoretest.Run(t, func(*testing.T) judgescoretest.Harness {
		s := porttest.NewStore()
		tn := porttest.NewTenancy(time.Now).WithStore(s)
		scores := porttest.NewJudgeScores(s)

		var orgs, factories, versions atomic.Int64

		return judgescoretest.Harness{
			JudgeScores: scores,
			Store:       s,
			NewOrg: func(t *testing.T) domain.OrgID {
				t.Helper()

				o, err := tn.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: fmt.Sprintf("acme-%d", orgs.Add(1)), Name: "Acme"})
				if err != nil {
					t.Fatal(err)
				}

				return o.ID
			},
			NewFactory: func(_ *testing.T, org domain.OrgID) domain.FactoryID {
				id := domain.FactoryID(fmt.Sprintf("00000000-0000-4000-8000-%012d", factories.Add(1)))
				s.AddFactory(org, id)

				return id
			},
			NewVersion: func(*testing.T, domain.OrgID, domain.FactoryID) domain.DefinitionVersionID {
				return domain.DefinitionVersionID(fmt.Sprintf("00000000-0000-4000-a000-%012d", versions.Add(1)))
			},
			Hold: func(_ *testing.T, org domain.OrgID, id domain.JudgeScoreID) func() { return scores.Hold(org, id) },
		}
	})
}
