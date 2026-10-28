package smtp

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"io"
	"math/big"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/ports"
)

// server is a minimal SMTP server on a loopback port: it speaks enough of RFC 5321 for net/smtp
// and records each session.
type server struct {
	ln       net.Listener
	starttls *tls.Config // offered as STARTTLS when set
	rcpt     string      // the reply to RCPT TO; "250 OK" by default

	sessions chan session // each connection's, when it ends
	wg       sync.WaitGroup
}

// session is what one connection did.
type session struct {
	tls        bool
	user, from string
	rcpt       []string
	data       string
}

func newServer(t *testing.T, implicit *tls.Config) *server {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	if implicit != nil {
		ln = tls.NewListener(ln, implicit)
	}

	s := &server{ln: ln, rcpt: "250 OK", sessions: make(chan session, 16)}

	s.wg.Go(func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}

			s.wg.Go(func() { s.serve(conn, implicit != nil) })
		}
	})

	t.Cleanup(func() {
		_ = ln.Close()
		s.wg.Wait()
	})

	return s
}

func (s *server) url(scheme, userinfo string) string {
	return scheme + "://" + userinfo + s.ln.Addr().String()
}

func (s *server) serve(conn net.Conn, secure bool) {
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	tp := textproto.NewConn(conn)
	sess := session{tls: secure}

	defer func() { s.sessions <- sess }()

	reply := func(lines ...string) { _ = tp.PrintfLine("%s", strings.Join(lines, "\r\n")) }

	reply("220 test ESMTP")

	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}

		verb := strings.ToUpper(strings.SplitN(line, " ", 2)[0])

		switch {
		case verb == "EHLO":
			lines := []string{"250-test"}
			if s.starttls != nil && !sess.tls {
				lines = append(lines, "250-STARTTLS")
			}

			reply(append(lines, "250 AUTH PLAIN")...)
		case verb == "STARTTLS":
			reply("220 go ahead")

			tc := tls.Server(conn, s.starttls)
			if err := tc.Handshake(); err != nil {
				return
			}

			conn, sess.tls = tc, true
			tp = textproto.NewConn(tc)
		case verb == "AUTH":
			raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, "AUTH PLAIN "))
			if parts := strings.Split(string(raw), "\x00"); len(parts) == 3 {
				sess.user = parts[1]
			}

			reply("235 OK")
		case strings.HasPrefix(strings.ToUpper(line), "MAIL FROM:"):
			sess.from = line[len("MAIL FROM:"):]

			reply("250 OK")
		case strings.HasPrefix(strings.ToUpper(line), "RCPT TO:"):
			sess.rcpt = append(sess.rcpt, line[len("RCPT TO:"):])

			reply(s.rcpt)
		case verb == "DATA":
			reply("354 go ahead")

			b, err := io.ReadAll(tp.DotReader())
			if err != nil {
				return
			}

			sess.data = string(b)

			reply("250 queued")
		case verb == "QUIT":
			reply("221 bye")

			return
		default:
			reply("250 OK")
		}
	}
}

// await returns the first session once its connection has ended.
func (s *server) await(t *testing.T) session {
	t.Helper()

	select {
	case got := <-s.sessions:
		return got
	case <-t.Context().Done():
		t.Fatal("no session ended")

		return session{}
	}
}

// testCert is a certificate for 127.0.0.1 and the pool that trusts it.
func testCert(t *testing.T) (*tls.Config, *x509.CertPool) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)}, KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}

	pool := x509.NewCertPool()
	pool.AddCert(cert)

	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}, pool
}

func newMailer(t *testing.T, c Config) *Mailer {
	t.Helper()

	if c.From == "" {
		c.From = "Goal <goal@example.com>"
	}

	m, err := New(c)
	if err != nil {
		t.Fatal(err)
	}

	return m
}

var message = ports.Message{
	To: "ann@example.com", Subject: "Web app #12 needs your OK",
	Text: "An agent needs your OK.\n\nOpen it in Goal: https://goal.example.com/o/acme\n",
	HTML: "<p>An agent needs your OK.</p>\n",
}

// A message goes out over a loopback connection as multipart/alternative: the text part, then
// the HTML part, to the recipient, from the sender.
func TestSMTP_SendsMultipartAlternative(t *testing.T) {
	t.Parallel()

	srv := newServer(t, nil)
	m := newMailer(t, Config{URL: srv.url("smtp", ""), Plaintext: true})

	if err := m.Send(t.Context(), message); err != nil {
		t.Fatal(err)
	}

	got := srv.await(t)
	if got.from != "<goal@example.com>" || len(got.rcpt) != 1 || got.rcpt[0] != "<ann@example.com>" {
		t.Fatalf("envelope from %q to %q; want goal@ and ann@example.com", got.from, got.rcpt)
	}

	parsed, err := mail.ReadMessage(strings.NewReader(got.data))
	if err != nil {
		t.Fatal(err)
	}

	if s := parsed.Header.Get("Subject"); s != message.Subject {
		t.Errorf("subject %q; want %q", s, message.Subject)
	}

	kind, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if err != nil || kind != "multipart/alternative" {
		t.Fatalf("content type %q, %v; want multipart/alternative", kind, err)
	}

	mr := multipart.NewReader(parsed.Body, params["boundary"])

	for _, want := range []struct{ kind, body string }{{"text/plain", message.Text}, {"text/html", message.HTML}} {
		p, err := mr.NextPart()
		if err != nil {
			t.Fatal(err)
		}

		b, _ := io.ReadAll(p)
		if !strings.HasPrefix(p.Header.Get("Content-Type"), want.kind) ||
			strings.ReplaceAll(string(b), "\r\n", "\n") != want.body {
			t.Errorf("part %q = %q; want %s %q", p.Header.Get("Content-Type"), b, want.kind, want.body)
		}
	}
}

// Off plaintext, smtp:// needs STARTTLS: a server that does not offer it gets no envelope.
func TestSMTP_RequiresSTARTTLS(t *testing.T) {
	t.Parallel()

	srv := newServer(t, nil)
	m := newMailer(t, Config{URL: srv.url("smtp", "")})

	err := m.Send(t.Context(), message)

	var me *ports.MailError
	if !errors.As(err, &me) || me.Code != "smtp-no-starttls" || errors.Is(err, ports.ErrMailPermanent) {
		t.Fatalf("send = %v; want a temporary smtp-no-starttls", err)
	}

	if got := srv.await(t); got.from != "" || got.data != "" {
		t.Fatalf("the server got an envelope in clear text: %+v", got)
	}
}

// With STARTTLS, the session upgrades before it signs in and sends.
func TestSMTP_UpgradesWithSTARTTLSAndSignsIn(t *testing.T) {
	t.Parallel()

	cfg, pool := testCert(t)
	srv := newServer(t, nil)
	srv.starttls = cfg

	m := newMailer(t, Config{URL: srv.url("smtp", "mailer:test-password@"), RootCAs: pool})
	if err := m.Send(t.Context(), message); err != nil {
		t.Fatal(err)
	}

	if got := srv.await(t); !got.tls || got.user != "mailer" || got.data == "" {
		t.Fatalf("session = tls %v, user %q, data %d bytes; want TLS, mailer and a message", got.tls, got.user, len(got.data))
	}
}

// smtps:// is TLS from the first byte.
func TestSMTP_ImplicitTLS(t *testing.T) {
	t.Parallel()

	cfg, pool := testCert(t)
	srv := newServer(t, cfg)

	m := newMailer(t, Config{URL: srv.url("smtps", ""), RootCAs: pool})
	if err := m.Send(t.Context(), message); err != nil {
		t.Fatal(err)
	}

	if got := srv.await(t); !got.tls || got.data == "" {
		t.Fatalf("session = %+v; want a message over TLS", got)
	}
}

// A certificate the mailer does not trust ends the send before the envelope.
func TestSMTP_RefusesAnUntrustedCertificate(t *testing.T) {
	t.Parallel()

	cfg, _ := testCert(t)
	srv := newServer(t, nil)
	srv.starttls = cfg

	m := newMailer(t, Config{URL: srv.url("smtp", "")})
	if err := m.Send(t.Context(), message); err == nil {
		t.Fatal("sent over a certificate nobody trusts")
	}
}

// A 5xx refusal of the recipient is permanent; a 4xx one is not; neither error quotes the
// server's reply or the address.
func TestSMTP_RecipientRefusals(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		reply     string
		code      string
		permanent bool
	}{
		{"550 5.1.1 <ann@example.com> no such user", "smtp-rcpt-550", true},
		{"451 4.3.0 <ann@example.com> try later", "smtp-rcpt-451", false},
	} {
		srv := newServer(t, nil)
		srv.rcpt = c.reply
		m := newMailer(t, Config{URL: srv.url("smtp", ""), Plaintext: true})

		err := m.Send(t.Context(), message)
		if err == nil || errors.Is(err, ports.ErrMailPermanent) != c.permanent || err.Error() != "mail: "+c.code {
			t.Errorf("reply %q: send = %v (permanent %v); want mail: %s (permanent %v)", c.reply, err,
				errors.Is(err, ports.ErrMailPermanent), c.code, c.permanent)
		}

		if err != nil && strings.Contains(err.Error(), "example.com") {
			t.Errorf("error %q names the address", err)
		}
	}
}

// A subject with a line break cannot add a header.
func TestSMTP_SubjectCannotAddHeaders(t *testing.T) {
	t.Parallel()

	srv := newServer(t, nil)
	m := newMailer(t, Config{URL: srv.url("smtp", ""), Plaintext: true})

	msg := message
	msg.Subject = "Hi\r\nBcc: eve@example.net"

	if err := m.Send(t.Context(), msg); err != nil {
		t.Fatal(err)
	}

	got := srv.await(t)

	parsed, err := mail.ReadMessage(strings.NewReader(got.data))
	if err != nil {
		t.Fatal(err)
	}

	if parsed.Header.Get("Bcc") != "" || len(got.rcpt) != 1 {
		t.Fatalf("the subject added a header: %q", got.data)
	}
}

func TestSMTP_NewRefusesBadSettings(t *testing.T) {
	t.Parallel()

	for _, c := range []Config{
		{URL: "http://mail.example.com", From: "goal@example.com"},
		{URL: "smtp://", From: "goal@example.com"},
		{URL: "smtp://mail.example.com", From: "not an address"},
	} {
		if _, err := New(c); err == nil {
			t.Errorf("New(%+v) accepted it", c)
		}
	}

	if _, err := New(Config{URL: "smtp://user:test-password@%zz", From: "goal@example.com"}); err == nil ||
		strings.Contains(err.Error(), "test-password") {
		t.Errorf("New with a bad URL = %v; want an error without the password", err)
	}
}
