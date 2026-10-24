package egress

import "net/netip"

// nonPublic are the ranges netip's own predicates leave out that no container may reach: "this
// network" (0.0.0.0/8, where OrbStack puts the host), shared address space (carrier-grade NAT),
// IETF protocol assignments, documentation, benchmarking (Docker Desktop's host side), reserved
// and broadcast, and the IPv6 forms that embed or translate to an IPv4 address (NAT64, 6to4,
// Teredo) or are deprecated site-local.
var nonPublic = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("fec0::/10"),
}

// Public reports whether a is an address on the public internet: not loopback, private
// (RFC 1918, unique local), link-local (169.254.0.0/16 with the cloud metadata services,
// fe80::/10), multicast, unspecified, or any range in nonPublic. An IPv4-mapped IPv6 address is
// judged as its IPv4 address.
func Public(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || !a.IsGlobalUnicast() || a.IsPrivate() {
		return false
	}

	for _, p := range nonPublic {
		if p.Contains(a) {
			return false
		}
	}

	return true
}
