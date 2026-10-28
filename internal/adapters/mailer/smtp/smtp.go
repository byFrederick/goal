// Package smtp implements ports.Mailer over SMTP (ADR 0023, decision 4), with the auth service's
// rules: smtps:// is TLS from the start; smtp:// must upgrade with STARTTLS unless Config.Plaintext
// allows clear text, which goald's settings do only for a loopback host or compose's Mailpit. The
// server's password travels only to the configured server, after TLS (net/smtp's PLAIN refuses it
// in clear text off localhost). No error carries an address, a password or the server's reply
// text, which may echo the recipient: each is a ports.MailError with a fixed code.
package smtp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"net/url"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.Mailer = (*Mailer)(nil)

// defaultTimeout bounds one send, from dial to the server's answer to the message.
const defaultTimeout = 30 * time.Second

// Config is the server mail goes through and who sends it.
type Config struct {
	// URL is smtp://[user:password@]host[:port] (port 25 by default) or smtps:// (port 465)
	// (GOAL_SMTP_URL).
	URL string
	// From is the sender, an address or "Name <address>" (GOAL_SMTP_FROM).
	From string
	// Plaintext lets smtp:// stay unencrypted: no STARTTLS is asked for.
	Plaintext bool
	// Timeout bounds one send; 0 is 30 seconds.
	Timeout time.Duration
	// RootCAs verifies the server's certificate; nil is the system's pool.
	RootCAs *x509.CertPool
}

// Mailer sends each message over its own connection.
type Mailer struct {
	addr, host string
	implicit   bool
	plaintext  bool
	auth       smtp.Auth
	from       *mail.Address
	helo       string
	timeout    time.Duration
	tls        *tls.Config
}

// New checks c and returns its mailer. Its errors never repeat the URL, which may hold a
// password.
func New(c Config) (*Mailer, error) {
	u, err := url.Parse(c.URL)
	if err != nil || (u.Scheme != "smtp" && u.Scheme != "smtps") || u.Hostname() == "" {
		return nil, errors.New("smtp: the URL is not an smtp:// or smtps:// URL with a host")
	}

	from, err := mail.ParseAddress(c.From)
	if err != nil {
		return nil, errors.New(`smtp: the sender is not an address or "Name <address>"`)
	}

	m := &Mailer{
		host: u.Hostname(), implicit: u.Scheme == "smtps", plaintext: c.Plaintext && u.Scheme == "smtp", from: from,
		helo: "localhost", timeout: c.Timeout,
	}

	port := u.Port()
	if port == "" {
		port = map[bool]string{true: "465", false: "25"}[m.implicit]
	}

	m.addr = net.JoinHostPort(m.host, port)
	m.tls = &tls.Config{ServerName: m.host, RootCAs: c.RootCAs, MinVersion: tls.VersionTLS12}

	if m.timeout <= 0 {
		m.timeout = defaultTimeout
	}

	if at := strings.LastIndexByte(from.Address, '@'); at >= 0 {
		m.helo = from.Address[at+1:]
	}

	if u.User != nil && u.User.Username() != "" {
		password, _ := u.User.Password()
		m.auth = smtp.PlainAuth("", u.User.Username(), password, m.host)
	}

	return m, nil
}

// Send implements ports.Mailer.
func (m *Mailer) Send(ctx context.Context, msg ports.Message) error {
	to, err := mail.ParseAddress(msg.To)
	if err != nil {
		return &ports.MailError{Code: "invalid-recipient", Permanent: true}
	}

	body, err := m.compose(msg, to)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()

	conn, err := m.dial(ctx)
	if err != nil {
		return err
	}

	// Cancelling ctx ends a send that waits on the server.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	c, err := smtp.NewClient(conn, m.host)
	if err != nil {
		_ = conn.Close()

		return mailError("greeting", err)
	}
	defer func() { _ = c.Close() }()

	if err := m.deliver(c, to.Address, body); err != nil {
		return err
	}

	// The message is accepted; a failed goodbye changes nothing.
	_ = c.Quit()

	return nil
}

// dial connects to the server within ctx, over TLS from the start for smtps://.
func (m *Mailer) dial(ctx context.Context) (net.Conn, error) {
	var d net.Dialer

	conn, err := d.DialContext(ctx, "tcp", m.addr)
	if err != nil {
		return nil, &ports.MailError{Code: "smtp-dial"}
	}

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	if !m.implicit {
		return conn, nil
	}

	tc := tls.Client(conn, m.tls)
	if err := tc.HandshakeContext(ctx); err != nil {
		_ = conn.Close()

		return nil, &ports.MailError{Code: "smtp-tls"}
	}

	return tc, nil
}

// deliver runs the session after the greeting: STARTTLS unless the connection may stay clear,
// the sign-in, the envelope and the message.
func (m *Mailer) deliver(c *smtp.Client, to string, body []byte) error {
	if err := c.Hello(m.helo); err != nil {
		return mailError("ehlo", err)
	}

	if !m.implicit && !m.plaintext {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return &ports.MailError{Code: "smtp-no-starttls"}
		}

		if err := c.StartTLS(m.tls); err != nil {
			return mailError("starttls", err)
		}
	}

	if m.auth != nil {
		if err := c.Auth(m.auth); err != nil {
			return mailError("auth", err)
		}
	}

	if err := c.Mail(m.from.Address); err != nil {
		return mailError("mail", err)
	}

	if err := c.Rcpt(to); err != nil {
		return mailError("rcpt", err)
	}

	w, err := c.Data()
	if err != nil {
		return mailError("data", err)
	}

	if _, err := w.Write(body); err != nil {
		return mailError("data", err)
	}

	if err := w.Close(); err != nil {
		return mailError("data", err)
	}

	return nil
}

// mailError is the failure of step: the reply's code without its text, permanent when the
// server refused the recipient or the message for good (5xx).
func mailError(step string, err error) error {
	if te, ok := errors.AsType[*textproto.Error](err); ok {
		return &ports.MailError{
			Code:      fmt.Sprintf("smtp-%s-%d", step, te.Code),
			Permanent: te.Code >= 500 && (step == "rcpt" || step == "data"),
		}
	}

	return &ports.MailError{Code: "smtp-" + step}
}

// compose writes msg as a multipart/alternative message: the text part first, the HTML part
// last, as mail readers prefer the last one they can show.
func (m *Mailer) compose(msg ports.Message, to *mail.Address) ([]byte, error) {
	id := make([]byte, 16)
	_, _ = rand.Read(id)

	var buf bytes.Buffer

	mw := multipart.NewWriter(&buf)
	header := []string{
		"From: " + m.from.String(),
		"To: " + to.String(),
		"Subject: " + mime.QEncoding.Encode("utf-8", oneLine(msg.Subject)),
		"Date: " + time.Now().UTC().Format(time.RFC1123Z),
		"Message-ID: <" + hex.EncodeToString(id) + "@" + m.helo + ">",
		"MIME-Version: 1.0",
		`Content-Type: multipart/alternative; boundary="` + mw.Boundary() + `"`,
	}

	buf.WriteString(strings.Join(header, "\r\n") + "\r\n\r\n")

	for _, part := range []struct{ kind, body string }{{"text/plain", msg.Text}, {"text/html", msg.HTML}} {
		pw, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {part.kind + "; charset=utf-8"},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return nil, fmt.Errorf("smtp: compose: %w", err)
		}

		qw := quotedprintable.NewWriter(pw)
		if _, err := qw.Write([]byte(part.body)); err != nil {
			return nil, fmt.Errorf("smtp: compose: %w", err)
		}

		if err := qw.Close(); err != nil {
			return nil, fmt.Errorf("smtp: compose: %w", err)
		}
	}

	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("smtp: compose: %w", err)
	}

	return buf.Bytes(), nil
}

// oneLine turns the line breaks of s into spaces, so a subject stays one header.
func oneLine(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
}
