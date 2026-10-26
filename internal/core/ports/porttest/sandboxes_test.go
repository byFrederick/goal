package porttest_test

import (
	"testing"

	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/core/ports/porttest/sandboxtest"
)

func TestSandboxes_Conformance(t *testing.T) {
	t.Parallel()

	sandboxtest.Run(t, func(*testing.T) sandboxtest.Harness {
		return sandboxtest.Harness{Sandboxes: porttest.NewSandboxes(sandboxtest.Script()), Image: "goal-agent"}
	})
}
