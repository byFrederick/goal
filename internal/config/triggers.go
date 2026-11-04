package config

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/catalog/senders"
	"github.com/byFrederick/goal/internal/core/domain"
)

// onSchedule is the event key of a trigger that fires on its cron, not on an event.
const onSchedule = "schedule"

// eventField is a placeholder's or dedupe's event field: names joined by dots (check.name).
var eventField = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)

// triggers checks board.triggers (ADR 0026): unique names, references to the definition's own
// work types, stations and lanes, a schedule's cron and time zone, a brief file that exists, and
// the create templates' placeholders, a brief file's included. The schema has already checked
// each trigger's shape.
func (c *checker) triggers() {
	b := c.f.Spec.Board
	dup(c, "trigger", names(b.Triggers, func(t Trigger) string { return t.Name }), "board", "triggers", keyName)

	for i, t := range b.Triggers {
		base := at("spec", "board", "triggers", i)
		create := append(slices.Clone(base), "create")

		if !slices.Contains(b.WorkTypes, t.Create.Type) {
			c.errorf(checkReferences, codeUnknownWorkType, append(slices.Clone(create), "type"),
				"trigger %s: create.type %q is not one of board.workTypes", t.Name, t.Create.Type)
		}

		c.triggerPlace(t, create)

		if t.On == onSchedule {
			c.schedule(t, base)
		}

		brief := t.Create.Brief
		if file, ok := BriefFile(brief); ok {
			brief = c.briefFile(t, file, create)
		}

		for _, tpl := range []struct{ key, text string }{{"title", t.Create.Title}, {"brief", brief}} {
			if bad := badPlaceholder(tpl.text); bad != "" {
				c.errorf(checkTriggers, codePlaceholderInvalid, append(slices.Clone(create), tpl.key),
					"trigger %s: create.%s has a malformed placeholder %q; a placeholder is {{field}} or {{field.sub}}",
					t.Name, tpl.key, bad)
			}
		}

		if t.On != onSchedule {
			c.deliveries(trigger(t), base)
		}
	}
}

// deliveries checks an event trigger's on: in its sender form: it names an event, its filters
// read only fields the sender's event table gives it (DeliveryRefusals), and create.trusted is
// allowed for the sender. Publish checks the webhook form, which needs the organization's
// webhooks.
func (c *checker) deliveries(t domain.Trigger, base []string) {
	sender, _, event := domain.ParseOn(t.On)
	if sender == "" {
		return
	}

	if event == "" {
		c.errorf(CheckDeliveries, codeOnNeedsEvent, append(slices.Clone(base), "on"),
			"trigger %s: on: %s names a sender without an event; write %s.<event>, such as %s.check_run", t.Name, sender, sender, sender)

		return
	}

	for _, r := range DeliveryRefusals(t, sender, event) {
		c.errorf(CheckDeliveries, r.Code, append(slices.Clone(base), r.Key), "trigger %s: %s", t.Name, r.Message)
	}
}

// DeliveryRefusal is one reason a trigger can't take a sender's event as it is written: its
// problem code, the trigger's key it is about (filter or create) and the message.
type DeliveryRefusal struct {
	Code, Key, Message string
}

// filterKeys are the filter keys that read each normalized field, for messages.
var filterKeys = map[string]string{
	domain.FieldBranch: "filter.branch", domain.FieldRepo: "filter.repo", domain.FieldLabel: "filter.label",
	domain.FieldAction: "filter.action", domain.FieldConclusion: "filter.conclusion", domain.FieldPaths: "filter.paths",
	domain.FieldAuthor: "filter.authors or filter.skipBots",
}

// DeliveryRefusals are why t can't take sender's event as written (ADR 0040 decisions 14 and
// 21): each filter that reads a field the sender's event table doesn't give it, so it never
// matches, and create.trusted where it is refused (domain.Trigger.TrustedRefusal).
func DeliveryRefusals(t domain.Trigger, sender domain.WebhookSender, event string) []DeliveryRefusal {
	var out []DeliveryRefusal

	if event != "" {
		tb := eventTable(sender, event)
		for _, f := range tb.Unsupplied(t.Filter.Needs()) {
			out = append(out, DeliveryRefusal{
				Code: CodeFilterUnsupplied, Key: "filter",
				Message: fmt.Sprintf("%s.%s carries no %s, so %s never matches", sender, event, f, filterKeys[f]),
			})
		}
	}

	if why := t.TrustedRefusal(sender); why != "" {
		out = append(out, DeliveryRefusal{Code: CodeTrustedRefused, Key: "create", Message: why})
	}

	return out
}

// eventTable is the shipped sender catalog's table for sender's event; empty when it has none,
// or the catalog does not load (its own tests keep it loading).
func eventTable(sender domain.WebhookSender, event string) domain.EventTable {
	c, err := senders.Shipped()
	if err != nil {
		return domain.EventTable{}
	}

	tb, _ := c.EventTable(sender, event)

	return tb
}

// briefPath is a create.brief that names a Markdown file of the definition instead of holding the
// brief (ADR 0040 decision 17), as an agent's prompt does.
var briefPath = regexp.MustCompile(`^\./triggers/[A-Za-z0-9][A-Za-z0-9._-]*\.md$`)

// BriefFile is the file brief names, when it is one path of the form ./triggers/<name>.md and
// nothing else; Load reads the item's brief from it.
func BriefFile(brief string) (string, bool) {
	brief = strings.TrimSpace(brief)

	return brief, briefPath.MatchString(brief)
}

// briefFile checks that t's brief file exists, as an agent's prompt must, and returns its text
// ("" when it doesn't).
func (c *checker) briefFile(t Trigger, file string, create []string) string {
	text, ok := c.f.briefText(file)
	if !ok {
		c.errorf(checkReferences, codeBriefNotFound, append(slices.Clone(create), "brief"), "trigger %s brief: %s not found", t.Name, file)
	}

	return text
}

// triggerPlace checks that create.station is a station and create.lane a lane of its route.
func (c *checker) triggerPlace(t Trigger, create []string) {
	i := slices.Index(c.stations, t.Create.Station)
	if i < 0 {
		c.errorf(checkReferences, codeUnknownStation, append(slices.Clone(create), "station"),
			"trigger %s: create.station %q is not a station", t.Name, t.Create.Station)

		return
	}

	st := c.f.Spec.Board.Stations[i]
	if _, ok := st.Route[t.Create.Lane]; t.Create.Lane != "" && !ok {
		c.errorf(checkReferences, codeUnknownLane, append(slices.Clone(create), "lane"),
			"trigger %s: create.lane %q is not a lane of station %s; its lanes are the keys of its route",
			t.Name, t.Create.Lane, st.ID)
	}
}

// schedule checks a schedule trigger's cron and time zone. The schema requires the cron.
func (c *checker) schedule(t Trigger, base []string) {
	if why := domain.CronProblem(t.Cron); why != "" {
		c.errorf(checkTriggers, codeCronInvalid, append(slices.Clone(base), "cron"),
			"trigger %s: cron %q must be five fields (minute, hour, day-of-month, month, day-of-week): %s",
			t.Name, t.Cron, why)
	}

	if t.TimeZone == "" {
		return
	}

	if _, err := time.LoadLocation(t.TimeZone); err != nil || t.TimeZone == "Local" {
		c.errorf(checkTriggers, codeTimeZoneInvalid, append(slices.Clone(base), "timeZone"),
			"trigger %s: timeZone %q is not an IANA time zone this install knows", t.Name, t.TimeZone)
	}
}

// badPlaceholder is the first malformed {{…}} in s, "" when every placeholder is a dotted event
// field (spaces inside the braces are allowed). An unclosed one is the text from its {{, cut short.
func badPlaceholder(s string) string {
	const shown = 40

	for {
		i := strings.Index(s, "{{")
		if i < 0 {
			return ""
		}

		s = s[i:]

		j := strings.Index(s[2:], "}}")
		if j < 0 {
			if r := []rune(s); len(r) > shown {
				return string(r[:shown]) + "…"
			}

			return s
		}

		if !eventField.MatchString(strings.TrimSpace(s[2 : j+2])) {
			return s[:j+4]
		}

		s = s[j+4:]
	}
}
