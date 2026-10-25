package egress

import (
	"errors"
	"slices"
	"testing"
)

// api.example.com's address in the tests' DNS, and where its https is.
const (
	apiIP   = "93.184.216.34"
	apiDest = apiIP + ":443"
)

// Spec 4 §5.9, ADR 0009 decision 4: goald's own requests for connections reach public addresses
// only, whatever a name resolves to; the one it dials is the address it vetted, never the name.
func TestPublicDialer_DialsOnlyPublicAddresses(t *testing.T) {
	t.Parallel()

	echo := echoServer(t)
	w := &world{
		names: map[string][]string{
			"api.example.com":      {apiIP},
			"mixed.example.com":    {apiIP, "10.0.0.7"},
			"metadata.example.com": {"169.254.169.254"},
			"localhost":            {"127.0.0.2"},
			"rustfs":               {"172.18.0.3"},
		},
		routes: map[string]string{apiDest: echo},
	}
	d := PublicDialer{Lookup: w.lookup, Dial: w.dial}

	for _, dest := range []string{
		"mixed.example.com:443", "metadata.example.com:80", "localhost:8081", "rustfs:9000",
		"127.0.0.9:5432", "[::1]:8080", "169.254.169.254:80", "192.168.1.10:443", "[::ffff:10.0.0.1]:443",
	} {
		if conn, err := d.DialContext(t.Context(), "tcp", dest); !errors.Is(err, ErrNotPublic) {
			if conn != nil {
				_ = conn.Close()
			}

			t.Errorf("DialContext %s = %v; want ErrNotPublic", dest, err)
		}
	}

	if got := w.dials(); len(got) != 0 {
		t.Fatalf("dialed %v before any public destination; want nothing", got)
	}

	conn, err := d.DialContext(t.Context(), "tcp", "api.example.com:443")
	if err != nil {
		t.Fatal(err)
	}

	_ = conn.Close()

	if got := w.dials(); !slices.Equal(got, []string{apiDest}) {
		t.Errorf("dialed %v; want the vetted address of api.example.com only", got)
	}

	if _, err := d.DialContext(t.Context(), "udp", "api.example.com:443"); err == nil {
		t.Error("a udp dial passed; want only tcp")
	}
}

// GOAL_ALLOW_PRIVATE_UPSTREAMS: a laptop's own test servers, dialed as they are.
func TestPublicDialer_AllowPrivate(t *testing.T) {
	t.Parallel()

	echo := echoServer(t)
	w := &world{names: map[string][]string{}, routes: map[string]string{"127.0.0.3:9": echo}}
	d := PublicDialer{AllowPrivate: true, Lookup: w.lookup, Dial: w.dial}

	conn, err := d.DialContext(t.Context(), "tcp", "127.0.0.3:9")
	if err != nil {
		t.Fatalf("DialContext of a loopback address with AllowPrivate = %v", err)
	}

	_ = conn.Close()
}
