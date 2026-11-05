package domain_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// Keys of TestApplyCheck: an open item, a done one, a cancelled one, one noted 10 minutes ago, one
// whose last note is noteSame, and a key without an item.
const (
	kOpen    = "k-open"
	kDone    = "k-done"
	kGone    = "k-gone"
	kNoted   = "k-noted"
	kRepeat  = "k-repeat"
	kNew     = "k-new"
	noteSame = "same"
)

// applyNow is when TestApplyCheck's checks finish.
var applyNow = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

// applyKeys are the trigger's keys TestApplyCheck's checks find.
func applyKeys() map[string]domain.CheckKey {
	closed := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)

	return map[string]domain.CheckKey{
		kOpen:   {Key: kOpen, Item: "i-open", Number: 7, State: domain.StateQueued},
		kDone:   {Key: kDone, Item: "i-done", Number: 3, State: domain.StateDone, ClosedAt: closed, Why: "fixed upstream"},
		kGone:   {Key: kGone, Item: "i-gone", Number: 4, State: domain.StateCancelled, ClosedAt: closed},
		kNoted:  {Key: kNoted, Item: "i-noted", Number: 8, State: domain.StateInProgress, NoteAt: applyNow.Add(-10 * time.Minute), NoteText: "earlier"},
		kRepeat: {Key: kRepeat, Item: "i-repeat", Number: 9, State: domain.StateQueued, NoteAt: applyNow.Add(-2 * time.Hour), NoteText: noteSame},
	}
}

// applyCase is one case of TestApplyCheck: an answer, the items it makes, the limits and the
// trigger's items today, and what it must file (<key>[<reopens]), note (<key>:<ref>:<text>) and
// skip.
type applyCase struct {
	name       string
	answer     domain.CheckAnswer
	items      []domain.CheckItem
	limits     domain.CheckLimits
	filedToday int
	filed      []string
	noted      []string
	skipped    []domain.CheckSkip
}

func file(key, reopen string) domain.CheckFile {
	return domain.CheckFile{Key: key, Title: "Title " + key, Brief: "Brief " + key, Reopen: reopen}
}

func made(key string) domain.CheckItem {
	return domain.CheckItem{Key: key, Item: domain.NewItem{Title: "Title " + key}}
}

func applyCases() []applyCase {
	closedDone := domain.CheckSkip{Key: kDone, Reason: "closed on Nov 2: fixed upstream"}

	return []applyCase{
		{name: "a new key is filed", answer: domain.CheckAnswer{File: []domain.CheckFile{file(kNew, "")}}, items: []domain.CheckItem{made(kNew)}, filed: []string{kNew}},
		{
			name: "a file on an open key becomes a note", answer: domain.CheckAnswer{File: []domain.CheckFile{file(kOpen, "")}},
			items: []domain.CheckItem{made(kOpen)}, noted: []string{kOpen + ":#7:Title " + kOpen + "\n\nBrief " + kOpen},
		},
		{
			name: "a closed key without reopen is skipped", answer: domain.CheckAnswer{File: []domain.CheckFile{file(kDone, ""), file(kGone, "")}},
			items: []domain.CheckItem{made(kDone), made(kGone)}, skipped: []domain.CheckSkip{closedDone, {Key: kGone, Reason: "closed on Nov 2: cancelled"}},
		},
		{
			name: "a closed key with reopen is filed again", answer: domain.CheckAnswer{File: []domain.CheckFile{file(kDone, "it came back")}},
			items: []domain.CheckItem{made(kDone)}, filed: []string{kDone + "<#3"},
		},
		{
			name: "a refused item is skipped with why", answer: domain.CheckAnswer{File: []domain.CheckFile{file(kNew, "")}},
			items:   []domain.CheckItem{{Key: kNew, Refused: `lane "x" is not one the line routes by`}},
			skipped: []domain.CheckSkip{{Key: kNew, Reason: `can't file it: lane "x" is not one the line routes by`}},
		},
		{
			name: "perCheck skips the rest over the limit", answer: domain.CheckAnswer{File: []domain.CheckFile{file("a", ""), file("b", ""), file("c", "")}},
			items: []domain.CheckItem{made("a"), made("b"), made("c")}, limits: domain.CheckLimits{PerCheck: 2},
			filed: []string{"a", "b"}, skipped: []domain.CheckSkip{{Key: "c", Reason: domain.CheckOverLimit}},
		},
		{
			name: "perDay counts what the trigger filed today", answer: domain.CheckAnswer{File: []domain.CheckFile{file("a", ""), file("b", "")}},
			items: []domain.CheckItem{made("a"), made("b")}, limits: domain.CheckLimits{PerDay: 4}, filedToday: 3,
			filed: []string{"a"}, skipped: []domain.CheckSkip{{Key: "b", Reason: domain.CheckOverLimit}},
		},
		{
			name: "notes go to open items, once an hour, never twice the same",
			answer: domain.CheckAnswer{Note: []domain.CheckNote{
				{Key: kOpen, Text: "first"},
				{Key: kOpen, Text: "second"},
				{Key: kNoted, Text: "later"},
				{Key: kRepeat, Text: noteSame},
				{Key: kDone, Text: "late"},
				{Key: kNew, Text: "who"},
			}},
			noted: []string{kOpen + ":#7:first"},
			skipped: []domain.CheckSkip{
				{Key: kOpen, Reason: domain.CheckNoteTooSoon},
				{Key: kNoted, Reason: domain.CheckNoteTooSoon},
				{Key: kRepeat, Reason: domain.CheckNoteRepeated},
				{Key: kDone, Reason: domain.CheckNoOpenItem},
				{Key: kNew, Reason: domain.CheckNoOpenItem},
			},
		},
		{
			name:   "skips are kept after the rest",
			answer: domain.CheckAnswer{File: []domain.CheckFile{file(kDone, "")}, Skip: []domain.CheckSkip{{Key: "x", Reason: "a test page"}}},
			items:  []domain.CheckItem{made(kDone)}, skipped: []domain.CheckSkip{closedDone, {Key: "x", Reason: "a test page"}},
		},
	}
}

func TestApplyCheck(t *testing.T) {
	t.Parallel()

	for _, tt := range applyCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := domain.ApplyCheck(tt.answer, tt.items, applyKeys(), tt.limits, tt.filedToday, applyNow)

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

	keys := map[string]domain.CheckKey{"k": {Key: "k", Item: "old", Number: 2, State: domain.StateDone, ClosedAt: applyNow.Add(-time.Hour)}}
	a := domain.CheckAnswer{File: []domain.CheckFile{{Key: "k", Title: "Back", Brief: "Again", Reopen: "it recurred"}}}

	got := domain.ApplyCheck(a, []domain.CheckItem{{Key: "k", Item: domain.NewItem{Title: "Back"}}}, keys, domain.CheckLimits{}, 0, applyNow)
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

	for k := range strings.SplitSeq("a b c d e f g", " ") {
		a.File = append(a.File, domain.CheckFile{Key: k, Title: k, Brief: k})
		items = append(items, domain.CheckItem{Key: k, Item: domain.NewItem{Title: k}})
	}

	got := domain.ApplyCheck(a, items, nil, domain.CheckLimits{}, 0, applyNow)
	if len(got.Filed) != domain.DefaultCheckPerCheck || len(got.Skipped) != 2 || !got.OverLimit() {
		t.Errorf("filed %d, skipped %+v; want %d filed and the rest over the limit", len(got.Filed), got.Skipped, domain.DefaultCheckPerCheck)
	}
}
