package porttest_test

import (
	"testing"

	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/core/ports/porttest/objectstoretest"
)

// The fake bucket must read ranges and take streams as the S3 adapter does.
func TestObjectStore_Conformance(t *testing.T) {
	t.Parallel()

	objectstoretest.Run(t, func(*testing.T) ports.ObjectStore { return porttest.NewObjectStore() })
}
