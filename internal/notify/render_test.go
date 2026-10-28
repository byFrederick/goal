package notify

import (
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

const publicURL = "https://goal.example.com"

func itemNotification(event domain.NotificationEvent) ports.Notification {
	return ports.Notification{
		ID: 7, Event: event, Channel: ports.ChannelEmail, Qualifies: true, Address: "ann@example.com",
		OrgSlug: "acme", FactoryID: "f-1", FactoryName: "web-app", FactoryTitle: "Web app",
		ItemNumber: 12, ItemTitle: "Add a login page", Station: "review",
	}
}

// Every event kind renders a plain-text and an HTML body with a link to its page under the
// public URL, and a link to the account's settings.
func TestRender_EveryEventKind(t *testing.T) {
	t.Parallel()

	andon := itemNotification(domain.NotifyAndon)
	andon.ItemNumber, andon.ItemTitle, andon.AndonKind = 0, "", string(domain.AndonManual)

	budget := itemNotification(domain.NotifyBudget)
	budget.ItemNumber, budget.ItemTitle, budget.Station, budget.SpentUSD, budget.WeeklyUSD = 0, "", "", 8.5, 10

	for _, c := range []struct {
		n       ports.Notification
		subject string
		says    string
		link    string
	}{
		{itemNotification(domain.NotifyApproval), "Web app #12 needs your OK", "needs your OK for a write", "/o/acme/f/web-app/items/12"},
		{itemNotification(domain.NotifyQuestion), "Web app #12 has a question for you", "asks you a question", "/o/acme/f/web-app/items/12"},
		{andon, "Web app pulled the andon", "pulled the andon", "/o/acme/f/web-app/board"},
		{budget, "Web app passed 80% of its weekly budget", "$8.50 of its $10.00 weekly budget", "/o/acme/spend"},
	} {
		t.Run(string(c.n.Event), func(t *testing.T) {
			t.Parallel()

			m, err := Render(c.n, publicURL+"/")
			if err != nil {
				t.Fatal(err)
			}

			if m.To != "ann@example.com" || m.Subject != c.subject {
				t.Errorf("to %q, subject %q; want ann@example.com, %q", m.To, m.Subject, c.subject)
			}

			for name, body := range map[string]string{"text": m.Text, "html": m.HTML} {
				for _, want := range []string{c.says, publicURL + c.link, publicURL + "/account"} {
					if !strings.Contains(body, want) {
						t.Errorf("%s body lacks %q:\n%s", name, want, body)
					}
				}
			}
		})
	}
}

// User-supplied names are escaped in the HTML, kept off the subject's header lines, and the HTML
// holds no image, no remote URL but the public URL and no script.
func TestRender_EscapesNamesAndLoadsNothingRemote(t *testing.T) {
	t.Parallel()

	n := itemNotification(domain.NotifyApproval)
	n.ItemTitle = `<script>alert("x")</script> <img src="https://tracker.example.net/p.gif">`
	n.FactoryTitle = "Web & app\r\nBcc: someone@example.net"
	n.Station = `"><a href="https://evil.example.net">`

	m, err := Render(n, publicURL)
	if err != nil {
		t.Fatal(err)
	}

	if strings.ContainsAny(m.Subject, "\r\n") {
		t.Errorf("subject %q holds a line break", m.Subject)
	}

	if !strings.Contains(m.HTML, "&lt;script&gt;") || !strings.Contains(m.HTML, "Web &amp; app") {
		t.Errorf("html does not escape the names:\n%s", m.HTML)
	}

	lower := strings.ToLower(m.HTML)
	for _, banned := range []string{"<script", "<img", "src=\"", "<a href=\"https://evil"} {
		if strings.Contains(lower, banned) {
			t.Errorf("html holds %q:\n%s", banned, m.HTML)
		}
	}

	for _, href := range hrefs(m.HTML) {
		if !strings.HasPrefix(href, publicURL+"/") {
			t.Errorf("html links to %q, outside the public URL", href)
		}
	}
}

// A factory name with characters a path would read differently is escaped in the link.
func TestRender_EscapesPathSegments(t *testing.T) {
	t.Parallel()

	n := itemNotification(domain.NotifyApproval)
	n.FactoryName = "../x?y"

	m, err := Render(n, publicURL)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(m.Text, publicURL+"/o/acme/f/..%2Fx%3Fy/items/12") {
		t.Errorf("text does not escape the factory's segment:\n%s", m.Text)
	}
}

func TestRender_RefusesAnUnknownEvent(t *testing.T) {
	t.Parallel()

	if _, err := Render(itemNotification(domain.NotifyDigest), publicURL); err == nil {
		t.Fatal("rendered the digest, which this sender does not send")
	}
}

// hrefs are the href values of html.
func hrefs(html string) []string {
	var out []string

	for rest := html; ; {
		i := strings.Index(rest, `href="`)
		if i < 0 {
			return out
		}

		rest = rest[i+len(`href="`):]
		out = append(out, rest[:strings.IndexByte(rest, '"')])
	}
}
