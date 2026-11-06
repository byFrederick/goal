package domain

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// A new CI wait needs its item, station, visit, a repository key, a sha, a phase, a round from 1
// and a start; only the after phase holds an outcome. Its poll's host is the key's first segment.
func TestNewCIWait_Check(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	ok := NewCIWait{Item: "i", Station: "review", Visit: "v1", Repo: "github.com/acme/app", SHA: "c0ffee", Phase: CIPhaseBefore, Round: 1, StartedAt: at}

	if err := ok.Check(); err != nil {
		t.Fatalf("Check() = %v; want nil", err)
	}

	if got, want := ok.PollKey(), (CIPollKey{Host: "github.com", Repo: "github.com/acme/app", SHA: "c0ffee"}); got != want {
		t.Errorf("PollKey() = %+v; want %+v", got, want)
	}

	bad := map[string]func(n *NewCIWait){
		"no visit":          func(n *NewCIWait) { n.Visit = "" },
		"url, not a key":    func(n *NewCIWait) { n.Repo = "https://github.com/acme/app.git" },
		"no sha":            func(n *NewCIWait) { n.SHA = "" },
		"unknown phase":     func(n *NewCIWait) { n.Phase = "during" },
		"round 0":           func(n *NewCIWait) { n.Round = 0 },
		"held before a run": func(n *NewCIWait) { n.Held = json.RawMessage(`{}`) },
		"no start":          func(n *NewCIWait) { n.StartedAt = time.Time{} },
	}

	for name, change := range bad {
		n := ok
		change(&n)

		if err := n.Check(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: Check() = %v; want ErrInvalid", name, err)
		}
	}
}

// A poll result needs a next poll time and an interval between 15 s and 2 min.
func TestCIPollResult_Check(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)

	for _, c := range []struct {
		r  CIPollResult
		ok bool
	}{
		{CIPollResult{NextPollAt: at, Interval: 15 * time.Second}, true},
		{CIPollResult{NextPollAt: at, Interval: 2 * time.Minute}, true},
		{CIPollResult{NextPollAt: at, Interval: 10 * time.Second}, false},
		{CIPollResult{NextPollAt: at, Interval: 3 * time.Minute}, false},
		{CIPollResult{Interval: 15 * time.Second}, false},
	} {
		if err := c.r.Check(); (err == nil) != c.ok {
			t.Errorf("Check(%+v) = %v; want ok %v", c.r, err, c.ok)
		}
	}
}
