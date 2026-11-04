package domain_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

func TestApplyCheck(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	keys := map[string]domain.CheckKey{
		"open":   {Key: "open", Item: "i-open", Number: 7, State: domain.StateQueued},
		"done":   {Key: "done", Item: "i-done", Number: 3, State: domain.StateDone, ClosedAt: time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC), Why: "fixed upstream"},
		"gone":   {Key: "gone", Item: "i-gone", Number: 4, State: domain.StateCancelled, ClosedAt: time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)},
		"noted":  {Key: "noted", Item: "i-noted", Number: 8, State: domain.StateInProgress, NoteAt: now.Add(-10 * time.Minute), NoteText: "earlier"},
		"repeat": {Key: "repeat", Item: "i-repeat", Number: 9, State: domain.StateQueued, NoteAt: now.Add(-2 * time.Hour), NoteText: "same"},
	}

	file := func(key, reopen string) domain.CheckFile {
		return domain.CheckFile{Key: key, Title: "Title " + key, Brief: "Brief " + key, Reopen: reopen}
	}

	item := func(key string) domain.CheckItem {
		return domain.CheckItem{Key: key, Item: domain.NewItem{Title: "Title " + key}}
	}

	for _, tt := range []struct {
		name       string
		answer     domain.CheckAnswer
		items      []domain.CheckItem
		limits     domain.CheckLimits
		filedToday int
		filed      []string
		noted      []string
		skipped    []domain.CheckSkip
	}{
		{
			name:   "a new key is filed",
			answer: domain.CheckAnswer{File: []domain.CheckFile{file("new", "")}},
			items:  []domain.CheckItem{item("new")},
			filed:  []string{"new"},
		},
		{
			name:   "a file on an open key becomes a note",
			answer: domain.CheckAnswer{File: []domain.CheckFile{file("open", "")}},
			items:  []domain.CheckItem{item("open")},
			noted:  []string{"open:#7:Title open\n\nBrief open"},
		},
		{
			name:    "a closed key without reopen is skipped",
			answer:  domain.CheckAnswer{File: []domain.CheckFile{file("done", ""), file("gone", "")}},
			items:   []domain.CheckItem{item("done"), item("gone")},
			skipped: []domain.CheckSkip{{Key: "done", Reason: "closed on Nov 2: fixed upstream"}, {Key: "gone", Reason: "closed on Nov 2: cancelled"}},
		},
		{
			name:   "a closed key with reopen is filed again",
			answer: domain.CheckAnswer{File: []domain.CheckFile{file("done", "it came back")}},
			items:  []domain.CheckItem{item("done")},
			filed:  []string{"done<#3"},
		},
		{
			name:    "a refused item is skipped with why",
			answer:  domain.CheckAnswer{File: []domain.CheckFile{file("bad", "")}},
			items:   []domain.CheckItem{{Key: "bad", Refused: "lane \"x\" is not one the line routes by"}},
			skipped: []domain.CheckSkip{{Key: "bad", Reason: "can't file it: lane \"x\" is not one the line routes by"}},
		},
		{
			name:    "perCheck skips the rest over the limit",
			answer:  domain.CheckAnswer{File: []domain.CheckFile{file("a", ""), file("b", ""), file("c", "")}},
			items:   []domain.CheckItem{item("a"), item("b"), item("c")},
			limits:  domain.CheckLimits{PerCheck: 2},
			filed:   []string{"a", "b"},
			skipped: []domain.CheckSkip{{Key: "c", Reason: domain.CheckOverLimit}},
		},
		{
			name:       "perDay counts what the trigger filed today",
			answer:     domain.CheckAnswer{File: []domain.CheckFile{file("a", ""), file("b", "")}},
			items:      []domain.CheckItem{item("a"), item("b")},
			limits:     domain.CheckLimits{PerDay: 4},
			filedToday: 3,
			filed:      []string{"a"},
			skipped:    []domain.CheckSkip{{Key: "b", Reason: domain.CheckOverLimit}},
		},
		{
			name: "notes go to open items, once an hour, never twice the same",
			answer: domain.CheckAnswer{Note: []domain.CheckNote{
				{Key: "open", Text: "first"}, {Key: "open", Text: "second"}, {Key: "noted", Text: "later"},
				{Key: "repeat", Text: "same"}, {Key: "done", Text: "late"}, {Key: "new", Text: "who"},
			}},
			noted: []string{"open:#7:first"},
			skipped: []domain.CheckSkip{
				{Key: "open", Reason: domain.CheckNoteTooSoon}, {Key: "noted", Reason: domain.CheckNoteTooSoon},
				{Key: "repeat", Reason: domain.CheckNoteRepeated}, {Key: "done", Reason: domain.CheckNoOpenItem}, {Key: "new", Reason: domain.CheckNoOpenItem},
			},
		},
		{
			name:    "skips are kept after the rest",
			answer:  domain.CheckAnswer{File: []domain.CheckFile{file("done", "")}, Skip: []domain.CheckSkip{{Key: "x", Reason: "a test page"}}},
			items:   []domain.CheckItem{item("done")},
			skipped: []domain.CheckSkip{{Key: "done", Reason: "closed on Nov 2: fixed upstream"}, {Key: "x", Reason: "a test page"}},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := domain.ApplyCheck(tt.answer, tt.items, keys, tt.limits, tt.filedToday, now)

			var filed, noted []string

			for _, f := range got.Filed {
				s := f.Key
				if f.Reopens != "" {
					s += "<" + f.Reopens
				}

				filed = append(filed, s)
			}

			for _, n := range got.Noted {
				noted = append(noted, n.Key+":"+n.Ref+":"+n.Text)
			}

			if !reflect.DeepEqual(filed, tt.filed) || !reflect.DeepEqual(noted, tt.noted) {
				t.Errorf("filed %q, noted %q; want %q, %q", filed, noted, tt.filed, tt.noted)
			}

			if len(got.Skipped) != len(tt.skipped) || (len(tt.skipped) > 0 && !reflect.DeepEqual(got.Skipped, tt.skipped)) {
				t.Errorf("skipped %+v; want %+v", got.Skipped, tt.skipped)
			}
		})
	}
}

// A key filed again links its new item to the closed one; the keys passed in are not changed.
func TestApplyCheck_ReopenLinksTheOldItem(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	keys := map[string]domain.CheckKey{"k": {Key: "k", Item: "old", Number: 2, State: domain.StateDone, ClosedAt: now.Add(-time.Hour)}}
	a := domain.CheckAnswer{File: []domain.CheckFile{{Key: "k", Title: "Back", Brief: "Again", Reopen: "it recurred"}}}

	got := domain.ApplyCheck(a, []domain.CheckItem{{Key: "k", Item: domain.NewItem{Title: "Back"}}}, keys, domain.CheckLimits{}, 0, now)
	if len(got.Filed) != 1 || !reflect.DeepEqual(got.Filed[0].Item.After, []domain.ItemID{"old"}) || got.Filed[0].Reopens != "#2" {
		t.Fatalf("filed = %+v; want one item after the old one", got.Filed)
	}

	if got.OverLimit() || len(keys) != 1 || keys["k"].NoteText != "" {
		t.Errorf("keys = %+v; want them unchanged", keys)
	}
}

func TestApplyCheck_DefaultPerCheck(t *testing.T) {
	t.Parallel()

	var (
		a     domain.CheckAnswer
		items []domain.CheckItem
	)

	for _, k := range strings.Split("a b c d e f g", " ") {
		a.File = append(a.File, domain.CheckFile{Key: k, Title: k, Brief: k})
		items = append(items, domain.CheckItem{Key: k, Item: domain.NewItem{Title: k}})
	}

	got := domain.ApplyCheck(a, items, nil, domain.CheckLimits{}, 0, time.Now())
	if len(got.Filed) != domain.DefaultCheckPerCheck || len(got.Skipped) != 2 || !got.OverLimit() {
		t.Errorf("filed %d, skipped %+v; want %d filed and the rest over the limit", len(got.Filed), got.Skipped, domain.DefaultCheckPerCheck)
	}
}
