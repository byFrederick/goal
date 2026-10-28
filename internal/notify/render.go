package notify

import (
	"fmt"
	"html"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// ErrUnknownEvent is the render of an event this sender has no message for.
var ErrUnknownEvent = fmt.Errorf("no message for the event: %w", domain.ErrInvalid)

// Render returns the email of n, to n.Address, whose Text is also n's Slack message: one sentence that says what happened, a link to
// the page that shows it and a link to the account's notification settings. Every link is
// publicURL plus a path of escaped segments; the HTML copy escapes every name and loads nothing.
func Render(n ports.Notification, publicURL string) (ports.Message, error) {
	base := strings.TrimRight(publicURL, "/")
	org := base + "/o/" + url.PathEscape(n.OrgSlug)
	factoryPath := org + "/f/" + url.PathEscape(n.FactoryName)
	factory := clean(n.FactoryTitle)
	item := "#" + strconv.FormatInt(n.ItemNumber, 10)
	itemLink := factoryPath + "/items/" + strconv.FormatInt(n.ItemNumber, 10)

	var subject, says, link string

	switch n.Event {
	case domain.NotifyApproval:
		subject = factory + " " + item + " needs your OK"
		says = "An agent" + at(n.Station) + " needs your OK for a write on " + item + " " + clean(n.ItemTitle) + " in " + factory + "."
		link = itemLink
	case domain.NotifyQuestion:
		subject = factory + " " + item + " has a question for you"
		says = "An agent" + at(n.Station) + " asks you a question on " + item + " " + clean(n.ItemTitle) + " in " + factory + "."
		link = itemLink
	case domain.NotifyAndon:
		subject = factory + " pulled the andon"

		says = factory + " pulled the andon" + at(n.Station)
		if n.AndonKind != "" {
			says += " (" + clean(n.AndonKind) + ")"
		}

		says += ". Its work is stopped until someone clears it."
		link = factoryPath + "/board"
	case domain.NotifyBudget:
		subject = factory + " passed 80% of its weekly budget"
		says = fmt.Sprintf("%s has spent $%.2f of its $%.2f weekly budget in the last 7 days.", factory, n.SpentUSD, n.WeeklyUSD)
		link = org + "/spend"
	case domain.NotifyDigest:
		// The digest is its own job's message (decision 6), never an outbox row.
		return ports.Message{}, fmt.Errorf("render %q: %w", n.Event, ErrUnknownEvent)
	default:
		return ports.Message{}, fmt.Errorf("render %q: %w", n.Event, ErrUnknownEvent)
	}

	account := base + "/account"
	footer := "You get this email because of your notification settings."
	if n.Channel == ports.ChannelSlack {
		footer = "You get this message because of your notification settings."
	}

	text := says + "\n\nOpen it in Goal: " + link + "\n\n-- \n" + footer + " Change them at " + account + "\n"
	body := "<!doctype html>\n<html><body>\n" +
		"<p>" + html.EscapeString(says) + "</p>\n" +
		`<p><a href="` + html.EscapeString(link) + `">Open it in Goal</a></p>` + "\n" +
		"<p>" + html.EscapeString(footer) + ` <a href="` + html.EscapeString(account) + `">Change them</a>.</p>` + "\n" +
		"</body></html>\n"

	return ports.Message{To: n.Address, Subject: subject, Text: text, HTML: body}, nil
}

// at is " at <station>", or nothing without one.
func at(station string) string {
	if station == "" {
		return ""
	}

	return " at " + clean(station)
}

// clean turns every control character of a user-supplied name into a space, so a name cannot
// break a line of the subject or the text.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}

		return r
	}, s)
}
