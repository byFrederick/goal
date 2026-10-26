package porttest_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/core/ports/porttest/definitionstest"
)

// The fake must pass the same suite as the PostgreSQL adapter, or authoring, engine and API
// tests built on it prove nothing.
func TestDefinitions_Conformance(t *testing.T) {
	t.Parallel()

	definitionstest.Run(t, func(*testing.T) definitionstest.Harness {
		objects, items, notices := porttest.NewObjectStore(), porttest.NewStore(), porttest.NewDefinitionNotices()
		tenancy := porttest.NewTenancy(time.Now)
		defs := porttest.NewDefinitions(porttest.DefinitionsDeps{
			Objects: objects, Items: items, Audit: tenancy, Notices: notices, Now: time.Now,
		})

		return definitionstest.Harness{
			Definitions: defs, Notices: notices, Objects: objects, Items: items, Audit: tenancy,
			NewOrg: func(t *testing.T) domain.OrgID {
				t.Helper()

				o, err := tenancy.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: "acme-" + uuid.NewString()[:8], Name: "Acme"})
				if err != nil {
					t.Fatal(err)
				}

				return o.ID
			},
		}
	})
}
