package egress

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
)

// PublicDialer connects over TCP to public addresses only (Public): it resolves a name itself,
// refuses it when any address it resolves to is not public (how a rebinding attack looks), and
// dials only the addresses it vetted. The proxy dials through it, and so does every request goald
// makes for a connection (tests, sync, STS, embeddings; spec 4 §5.9, ADR 0009 decision 4), so a
// connection's URL never reaches goald's own network (auth, RustFS, Mailpit) or the cloud's
// metadata service. It is safe for concurrent use.
type PublicDialer struct {
	// AllowPrivate dials any address (GOAL_ALLOW_PRIVATE_UPSTREAMS: a laptop's own test servers).
	AllowPrivate bool
	// Lookup and Dial replace DNS resolution and connecting (tests); nil means the system's.
	Lookup func(ctx context.Context, host string) ([]netip.Addr, error)
	Dial   func(ctx context.Context, network, address string) (net.Conn, error)
}

// DialContext connects to address (host:port) within DialTimeout, as net.Dialer's does; its
// signature fits http.Transport.DialContext. Only tcp, tcp4 and tcp6 are dialed.
func (d PublicDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("dial %s %s: only tcp is dialed", network, address)
	}

	ctx, cancel := context.WithTimeout(ctx, DialTimeout)
	defer cancel()

	dial := d.Dial
	if dial == nil {
		dial = (&net.Dialer{Timeout: DialTimeout}).DialContext
	}

	if d.AllowPrivate {
		return dial(ctx, network, address)
	}

	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	addrs, err := d.resolve(ctx, host)
	if err != nil {
		return nil, err
	}

	var errs []error

	for _, a := range addrs {
		conn, err := dial(ctx, network, net.JoinHostPort(a.Unmap().String(), port))
		if err == nil {
			return conn, nil
		}

		errs = append(errs, err)
	}

	return nil, errors.Join(errs...)
}

// resolve is host's addresses, every one of them public.
func (d PublicDialer) resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	lookup := d.Lookup
	if lookup == nil {
		lookup = systemLookup
	}

	var addrs []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		addrs = []netip.Addr{ip}
	} else if addrs, err = lookup(ctx, host); err != nil {
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}

	if len(addrs) == 0 {
		return nil, fmt.Errorf("resolve %s: no addresses", host)
	}

	for _, a := range addrs {
		if !Public(a) {
			return nil, fmt.Errorf("%w: %s is %s", ErrNotPublic, host, a.Unmap())
		}
	}

	return addrs, nil
}
