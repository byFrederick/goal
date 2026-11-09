package porttest_test

import (
	"net/http/httptest"
	"testing"

	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/core/ports/porttest/objectstoretest"
)

// The fake bucket must read ranges, take streams and take signed uploads as the S3 adapter does.
// Each store serves its signed uploads from its own test server.
func TestObjectStore_Conformance(t *testing.T) {
	t.Parallel()

	objectstoretest.Run(t, func(t *testing.T) ports.ObjectStore {
		t.Helper()

		s := porttest.NewObjectStore()
		srv := httptest.NewServer(s)
		t.Cleanup(srv.Close)
		s.ServeUploadsAt(srv.URL + "/")

		return s
	})
}
