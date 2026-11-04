package domain

import (
	"cmp"
	"fmt"
	"maps"
	"strings"
	"time"
)

// What goald enforces on a check's answer (ADR 0040 decision 19).
const (
	// DefaultCheckPerCheck is limits.perCheck of a check trigger that sets none: the most items
	// one check files.
	DefaultCheckPerCheck = 5
	// CheckNoteEvery is how often a key takes a note at most.
	CheckNoteEvery = time.Hour
)

// The reasons ApplyCheck records for what it does not file or note.
const (
	// CheckOverLimit skips a file past limits.perCheck or limits.perDay.
	CheckOverLimit = "over the limit"
	// CheckNoteTooSoon drops a note on a key that took one within CheckNoteEvery.
	CheckNoteTooSoon = "a note on this key within the hour"
	// CheckNoteRepeated drops a note whose text is the key's last note's.
	CheckNoteRepeated = "the same note again"
	// CheckNoOpenItem skips a note for a key without an open item.
	CheckNoOpenItem = "no open item for this key"
)

// CheckKey is what the trigger knows of a key it filed for (trigger_keys): the key's last item
// with its number and how it stands (its state, and for a closed one when and why it closed,
// Why "" when no reason was given), and when the key's last note was written and its text.
type CheckKey struct {
	Key      string
	Item     ItemID
	Number   int64
	State    ItemState
	ClosedAt time.Time
	Why      string
	NoteAt   time.Time
	NoteText string
}

// Open reports whether k's item is open: neither done nor cancelled.
func (k CheckKey) Open() bool {
	return k.Item != "" && k.State != StateDone && k.State != StateCancelled
}

// Closed reports whether k's item is done or cancelled.
func (k CheckKey) Closed() bool { return k.Item != "" && !k.Open() }

// ref is k's item's ref.
func (k CheckKey) ref() string { return fmt.Sprintf("#%d", k.Number) }

// CheckItem is a check's file entry for Key as domain.File made it into an item on the trigger's
// line, or Refused, why File refused it.
type CheckItem struct {
	Key     string
	Item    NewItem
	Refused string
}

// CheckLimits are a check trigger's limits: PerCheck items a check at most (DefaultCheckPerCheck
// when 0), and PerDay since the UTC day began, counting the items the trigger filed (0: none).
type CheckLimits struct {
	PerCheck int
	PerDay   int
}

// LimitsOfCheck are t's limits as a check applies them.
func (t Trigger) LimitsOfCheck() CheckLimits {
	return CheckLimits{PerCheck: cmp.Or(t.Limits.PerCheck, DefaultCheckPerCheck), PerDay: t.Limits.PerDay}
}

// CheckApplied is what applying a check's answer did, or for a try check would do: the items it
// filed, the notes it wrote, and what it skipped, each with why. The firing keeps it.
type CheckApplied struct {
	Filed   []CheckFiled `json:"filed"`
	Noted   []CheckNoted `json:"noted"`
	Skipped []CheckSkip  `json:"skipped"`
}

// CheckFiled is an item filed for Key: its title, its ref once filed ("" for a try), and for a
// key filed again, Reopens, the ref of the closed item it links to. Item is what to file.
type CheckFiled struct {
	Key     string  `json:"key"`
	Title   string  `json:"title"`
	Ref     string  `json:"ref,omitempty"`
	Reopens string  `json:"reopens,omitempty"`
	Item    NewItem `json:"-"`
}

// CheckNoted is a note with Text on the open item filed for Key (Item, ref Ref).
type CheckNoted struct {
	Key  string `json:"key"`
	Ref  string `json:"ref"`
	Text string `json:"text"`
	Item ItemID `json:"-"`
}

// OverLimit reports whether a skipped anything for being over the limit.
func (a CheckApplied) OverLimit() bool {
	for _, s := range a.Skipped {
		if s.Reason == CheckOverLimit {
			return true
		}
	}

	return false
}

// CheckFailuresShown is how many checks of a trigger fail in a row before Needs you shows it.
const CheckFailuresShown = 3

// CheckAlertKind is what of a trigger's checks Needs you shows.
type CheckAlertKind string

// Check alert kinds.
const (
	// CheckFailing is a trigger whose last CheckFailuresShown checks failed.
	CheckFailing CheckAlertKind = "failing"
	// CheckLimited is a trigger whose check skipped something over the limit that UTC day.
	CheckLimited CheckAlertKind = "limit"
)

// CheckAlert is a trigger's checks as Needs you shows them: of Kind, since At, with the newest
// failure's Reason for CheckFailing.
type CheckAlert struct {
	FactoryID FactoryID
	Trigger   string
	Kind      CheckAlertKind
	At        time.Time
	Reason    string
}

// The data keys an item_note a check writes adds to NoteData's text and agent: the trigger, the
// key and the check's run.
const (
	KeyNoteTrigger = "trigger"
	KeyNoteKey     = "key"
	KeyNoteRun     = "run_id"
)

// CheckNoteData is the data of the item_note event a check of trigger writes on key's item: the
// text, the trigger, the key, the check's agent and run, and the engine as actor, as NoteData's.
func CheckNoteData(trigger, key, agent string, run RunID, text string) map[string]any {
	return map[string]any{
		"text": text, KeyNoteTrigger: trigger, KeyNoteKey: key, KeyNoteAgent: agent, KeyNoteRun: string(run),
		evKeyActor: EngineActor.Ref(),
	}
}

// ApplyCheck decides what a check's answer a does at now (ADR 0040 decision 19), given the
// items its file entries make (by key), the trigger's keys and limits, and how many items the
// trigger filed since the UTC day began:
//
//   - a file whose key has an open item becomes a note on it, its title and brief;
//   - a file whose key's item closed is filed again only with reopen, linked to the old item, and
//     is otherwise skipped, "closed on Nov 2: <reason>";
//   - any other file is filed, while the check has filed fewer than PerCheck and the day fewer
//     than PerDay; the rest are skipped CheckOverLimit, and one File refused is skipped with why;
//   - a note goes to its key's open item, else is skipped CheckNoOpenItem;
//   - a note on a key whose last note has the same text is dropped, and so is one within
//     CheckNoteEvery of the key's last note, each recorded as skipped;
//   - a skip is kept as it is.
//
// Files come first in answer order, then notes, then skips. It does no I/O.
func ApplyCheck(a CheckAnswer, items []CheckItem, keys map[string]CheckKey, lim CheckLimits, filedToday int, now time.Time) CheckApplied {
	out := CheckApplied{Filed: []CheckFiled{}, Noted: []CheckNoted{}, Skipped: []CheckSkip{}}
	keys = cloneKeys(keys)
	perCheck := cmp.Or(lim.PerCheck, DefaultCheckPerCheck)

	made := make(map[string]CheckItem, len(items))
	for _, it := range items {
		made[it.Key] = it
	}

	for _, f := range a.File {
		k := keys[f.Key]

		switch it, ok := made[f.Key]; {
		case k.Open():
			out.note(keys, f.Key, noteOfFile(f), now)
		case k.Closed() && strings.TrimSpace(f.Reopen) == "":
			out.skip(f.Key, closedReason(k))
		case !ok:
			out.skip(f.Key, "can't file it: no item was made for it")
		case it.Refused != "":
			out.skip(f.Key, "can't file it: "+it.Refused)
		case len(out.Filed) >= perCheck || (lim.PerDay > 0 && filedToday+len(out.Filed) >= lim.PerDay):
			out.skip(f.Key, CheckOverLimit)
		default:
			out.Filed = append(out.Filed, filedOf(it.Item, f, k))
		}
	}

	for _, n := range a.Note {
		if !keys[n.Key].Open() {
			out.skip(n.Key, CheckNoOpenItem)
			continue
		}

		out.note(keys, n.Key, n.Text, now)
	}

	out.Skipped = append(out.Skipped, a.Skip...)

	return out
}

// filedOf is item filed for f. A key filed again links its new item to the closed one, k's, with
// a linked edge, which a closed item never holds up.
func filedOf(item NewItem, f CheckFile, k CheckKey) CheckFiled {
	out := CheckFiled{Key: f.Key, Title: item.Title, Item: item}

	if k.Closed() {
		out.Reopens = k.ref()
		out.Item.After = append(append([]ItemID{}, item.After...), k.Item)
	}

	return out
}

// note adds a note with text on key's open item unless the key's last note has the same text or
// was written within CheckNoteEvery of now, and then records it as the key's last note.
func (a *CheckApplied) note(keys map[string]CheckKey, key, text string, now time.Time) {
	k := keys[key]

	switch {
	case k.NoteText == text:
		a.skip(key, CheckNoteRepeated)
	case !k.NoteAt.IsZero() && now.Sub(k.NoteAt) < CheckNoteEvery:
		a.skip(key, CheckNoteTooSoon)
	default:
		a.Noted = append(a.Noted, CheckNoted{Key: key, Ref: k.ref(), Text: text, Item: k.Item})
		k.NoteAt, k.NoteText = now, text
		keys[key] = k
	}
}

func (a *CheckApplied) skip(key, reason string) {
	a.Skipped = append(a.Skipped, CheckSkip{Key: key, Reason: reason})
}

// noteOfFile is the note a file on an open item becomes: its title, then its brief, within a
// note's length.
func noteOfFile(f CheckFile) string {
	return truncate(strings.TrimSpace(f.Title)+"\n\n"+strings.TrimSpace(f.Brief), maxCheckText)
}

// closedReason is why a file on k's closed item is skipped: "closed on Nov 2: <reason>", the
// reason being how it closed when none was given.
func closedReason(k CheckKey) string {
	why := cmp.Or(strings.TrimSpace(k.Why), string(k.State))

	return truncate(fmt.Sprintf("closed on %s: %s", k.ClosedAt.UTC().Format("Jan 2"), why), maxCheckReason)
}

func cloneKeys(keys map[string]CheckKey) map[string]CheckKey {
	out := make(map[string]CheckKey, len(keys))
	maps.Copy(out, keys)

	return out
}
