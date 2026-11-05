package domain

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Origin is where an item came from (ADR 0040 decision 2). Values match work_items.origin.
type Origin string

// Origins.
const (
	// OriginPerson is an item a person filed: through the API, the CLI, New item or onboarding.
	OriginPerson Origin = "person"
	// OriginPlan is a part of an assembly, from its plan (ADR 0012) or added to it.
	OriginPlan Origin = "plan"
	// OriginTrigger is an item a trigger filed from a schedule or an event.
	OriginTrigger Origin = "trigger"
	// OriginFollowUp is an item filed from agents' follow-ups.
	OriginFollowUp Origin = "follow-up"
)

// Origins are every origin, in the order Intake lists them: the one list of them in Go.
var Origins = []Origin{OriginPerson, OriginPlan, OriginTrigger, OriginFollowUp}

// Via is how a person filed an item. Values match work_items.via.
type Via string

// Ways a person files an item.
const (
	ViaWeb Via = "web"
	ViaCLI Via = "cli"
	ViaAPI Via = "api"
)

// FileRequest asks to file an item on a line: what domain.File checks and turns into a NewItem.
type FileRequest struct {
	// FactoryID names the line the caller looks up; File files on the line it is given.
	FactoryID FactoryID
	Origin    Origin
	// Via is how a person filed it, only for OriginPerson; "" when the request does not say.
	Via Via
	// By is who files it: the person or API key, the planning agent, the engine for a trigger.
	By    Actor
	Type  string
	Lane  string
	Title string
	Body  string
	// Priority is ignored for a part, which takes its assembly's.
	Priority int
	// Station, State and Class are where a trigger places its item; the line's first station,
	// its arrival state and the type's class when empty. Only a trigger sets them, and a
	// follow-up its Class (always standard, ADR 0040 decision 8).
	Station string
	State   ItemState
	Class   ClassOfService
	// Trigger names the trigger that files it and Source where the item came from, for Intake.
	Trigger string
	Source  ItemSource
	// Generation is 0 for people's and triggers' items and the assembly's for a part.
	Generation int
	// Untrusted marks work made from a webhook delivery; a part has its assembly's.
	Untrusted bool
	// Assembly is the item a part belongs to, required for OriginPlan and refused otherwise.
	Assembly *Item
	// Start is when the item may start (ADR 0040 decision 3); nil starts it as soon as it can.
	Start *StartRequest
	// Repositories are the repositories the item works on, each an entry's name, owner/name, an
	// https URL or host/owner/name that one of the line's runtime.repositories entries covers;
	// none gives it Inherited's, else the default entry's (ADR 0041 decision 4).
	Repositories []string
	// Inherited are the repositories, as keys (RepositoryKey), of the items this one comes from:
	// its assembly's for a part, its source's for a follow-up's item, its sources' for a triage
	// item. File uses them only when Repositories names none.
	Inherited []string
	At           time.Time
}

// File turns a filing request into a checked NewItem the same way for every origin (ADR 0040
// decision 1). The title is trimmed and required. The type is one of the line's work types, the
// first when the request gives none, and anything when the line declares none; a part takes its
// assembly's. A lane must be one the line's stations route by, when any does. A person's, a
// follow-up's and a part's item enters at the line's first station, a part at its assembly's,
// and a trigger's at the station it names, which must be on the line. Via is only a person's,
// the generation is never negative and 0 for a person and a trigger, a person's item is never
// untrusted, and a part's generation and untrusted are its assembly's. A start is resolved at
// r.At (StartRequest.Resolve): its time becomes StartAt and its open after items After. An item
// placed at a later station than the line's first has started when it is filed. Its repositories
// are those the request names, resolved against the line's entries by entry name, URL,
// host/owner/name or owner/name (case, .git and owner patterns included) and joined filing in the
// order named, once each and at most MaxItemRepositories. When it names none, they are the
// Inherited ones the line's entries cover, joined filing in order, once each and cut at
// MaxItemRepositories; else the default entry's, joined default; none without a default entry. The
// item's branch takes the line's name (NewItem.FactoryName). Every refusal is ErrInvalid.
func File(l Line, r FileRequest) (NewItem, error) {
	n := NewItem{
		FactoryID: l.FactoryID, Lane: strings.TrimSpace(r.Lane), Title: strings.TrimSpace(r.Title), Body: r.Body,
		Priority: r.Priority, CreatedAt: r.At, Source: r.Source, Trigger: r.Trigger,
		Origin: r.Origin, Via: r.Via, CreatedBy: r.By, Generation: r.Generation, Untrusted: r.Untrusted,
		FactoryName: l.Name,
	}

	if err := checkOrigin(r); err != nil {
		return NewItem{}, fmt.Errorf("file an item: %w", err)
	}

	if n.Title == "" {
		return NewItem{}, fmt.Errorf("file an item: an item needs a title: %w", ErrInvalid)
	}

	if routes := l.RoutedLanes(); n.Lane != "" && len(routes) > 0 && !slices.Contains(routes, n.Lane) {
		return NewItem{}, fmt.Errorf("file an item: lane %q is not one the line routes by (%s): %w", n.Lane, strings.Join(routes, ", "), ErrInvalid)
	}

	var err error
	if r.Origin == OriginPlan {
		err = filePart(&n, r)
	} else {
		err = fileOnLine(&n, l, r)
	}

	if err != nil {
		return NewItem{}, fmt.Errorf("file an item: %w", err)
	}

	if n.Repositories, err = fileRepositories(l, r.Repositories, r.Inherited); err != nil {
		return NewItem{}, fmt.Errorf("file an item: %w", err)
	}

	if r.Start != nil {
		s, err := r.Start.Resolve(l.FactoryID, r.At)
		if err != nil {
			return NewItem{}, fmt.Errorf("file an item: start: %w", err)
		}

		n.StartAt, n.After = s.At, s.After
	}

	if first, err := l.First(); err == nil && n.Station != first.ID {
		n.StartedAt = r.At
	}

	return n, nil
}

// checkOrigin checks r's origin, via and class, then its lineage.
func checkOrigin(r FileRequest) error {
	switch {
	case !slices.Contains(Origins, r.Origin):
		return fmt.Errorf("unknown origin %q: %w", r.Origin, ErrInvalid)
	case r.Via != "" && r.Origin != OriginPerson:
		return fmt.Errorf("only a person files through %q, not a %s: %w", r.Via, r.Origin, ErrInvalid)
	case r.Via != "" && !slices.Contains([]Via{ViaWeb, ViaCLI, ViaAPI}, r.Via):
		return fmt.Errorf("unknown via %q: %w", r.Via, ErrInvalid)
	case r.Class != "" && r.Origin != OriginTrigger && r.Origin != OriginFollowUp:
		return fmt.Errorf("only a trigger or a follow-up sets its item's class: %w", ErrInvalid)
	}

	return checkLineage(r)
}

// checkLineage checks r's generation and untrusted against its origin, and that only a part
// names an assembly and only a trigger a station.
func checkLineage(r FileRequest) error {
	switch {
	case r.Generation < 0:
		return fmt.Errorf("generation %d is negative: %w", r.Generation, ErrInvalid)
	case r.Generation != 0 && (r.Origin == OriginPerson || r.Origin == OriginTrigger):
		return fmt.Errorf("a %s's item is generation 0, not %d: %w", r.Origin, r.Generation, ErrInvalid)
	case r.Untrusted && r.Origin == OriginPerson:
		return fmt.Errorf("a person's item is never untrusted: %w", ErrInvalid)
	case (r.Assembly != nil) != (r.Origin == OriginPlan):
		return fmt.Errorf("only a part has an assembly: %w", ErrInvalid)
	case (r.Station != "" || r.State != "") && r.Origin != OriginTrigger:
		return fmt.Errorf("only a trigger places its item: %w", ErrInvalid)
	}

	return nil
}

// filePart fills n, a part of r.Assembly, from its assembly: type, priority, class of service,
// parent and station, queued there.
func filePart(n *NewItem, r FileRequest) error {
	a := r.Assembly

	switch {
	case a.ParentID != "":
		return fmt.Errorf("assembly %s is itself a part: %w", a.ID, ErrInvalid)
	case r.Type != "" && r.Type != a.Type:
		return fmt.Errorf("a part has its assembly's type %q, not %q: %w", a.Type, r.Type, ErrInvalid)
	case r.Generation != a.Generation:
		return fmt.Errorf("a part has its assembly's generation %d, not %d: %w", a.Generation, r.Generation, ErrInvalid)
	case r.Untrusted != a.Untrusted:
		return fmt.Errorf("a part is untrusted when its assembly is (%t), not %t: %w", a.Untrusted, r.Untrusted, ErrInvalid)
	}

	n.ParentID, n.Type, n.Priority, n.Class = a.ID, a.Type, a.Priority, a.Class
	n.Station, n.State = a.Station, StateQueued

	return nil
}

// fileOnLine fills n's type and place on l: the request's type or the line's first, at the
// trigger's station or the line's first.
func fileOnLine(n *NewItem, l Line, r FileRequest) error {
	n.Type = strings.TrimSpace(r.Type)
	if len(l.WorkTypes) > 0 {
		if n.Type == "" {
			n.Type = l.WorkTypes[0]
		}

		if !slices.Contains(l.WorkTypes, n.Type) {
			return fmt.Errorf("type %q is not one of the line's work types %v: %w", n.Type, l.WorkTypes, ErrInvalid)
		}
	}

	st, err := l.First()
	if err != nil {
		return fmt.Errorf("%w: %w", err, ErrInvalid)
	}

	if r.Station != "" {
		var ok bool
		if st, ok = l.Station(r.Station); !ok {
			return fmt.Errorf("station %q is not on the line: %w", r.Station, ErrInvalid)
		}
	}

	n.Station, n.State, n.Class = st.ID, cmp.Or(r.State, st.Arrival()), cmp.Or(r.Class, l.ClassFor(n.Type))

	return nil
}

// FiledBy is who filed n: CreatedBy, or EngineActor when it is unset.
func (n NewItem) FiledBy() Actor {
	if n.CreatedBy == (Actor{}) {
		return EngineActor
	}

	return n.CreatedBy
}

// FiledRecords are what a store writes with an item by filed: its created event (part_created
// for a part, with its assembly and key), whose actor is by with by's ref in its data, and the
// item.filed audit entry, with the item's origin and a person's via as filed_via (an entry's via
// is the credential a write came through, pat:<id>, which the store adds).
func FiledRecords(it Item, by Actor) (Event, AuditEntry) {
	kind, data := EventCreated, map[string]any{"actor": by.Ref()}
	if it.ParentID != "" {
		kind, data["parent"], data["key"] = EventPartCreated, string(it.ParentID), it.PartKey
	}

	audit := map[string]any{"origin": string(it.Origin), "number": it.Number}
	if it.Via != "" {
		audit["filed_via"] = string(it.Via)
	}

	return Event{FactoryID: it.FactoryID, ItemID: it.ID, At: it.CreatedAt, Kind: kind, Station: it.Station, Actor: by.Name, Data: data},
		AuditEntry{
			At: it.CreatedAt, Actor: by, Action: AuditItemFiled, TargetKind: AuditTargetItem, TargetID: string(it.ID),
			FactoryID: it.FactoryID, Data: audit,
		}
}
