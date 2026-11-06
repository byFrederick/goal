package domain

import (
	"errors"
	"testing"
	"time"
)

// A read-only name must be one of the test's tools, and Clone copies both lists.
func TestEnvironmentTest_ReadOnly(t *testing.T) {
	t.Parallel()

	ok := EnvironmentTest{
		Factory: "f", Environment: "default", Server: "linear", At: time.Unix(1, 0), OK: true,
		Tools: []string{"list_issues", "merge_pull_request"}, ReadOnly: []string{"list_issues"},
	}
	if err := ok.Check(); err != nil {
		t.Fatalf("Check = %v; want nil", err)
	}

	stray := ok
	stray.ReadOnly = []string{"search_code"}

	if err := stray.Check(); !errors.Is(err, ErrInvalid) {
		t.Errorf("Check of a read-only name not in Tools = %v; want ErrInvalid", err)
	}

	c := ok.Clone()
	c.ReadOnly[0] = "changed"

	if ok.ReadOnly[0] != "list_issues" {
		t.Errorf("Clone shares ReadOnly with the original")
	}
}
