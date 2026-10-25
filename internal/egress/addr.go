package egress

import "net/netip"

// globalUnicast6 is the only IPv6 space with public hosts (RFC 4291's global unicast, 2000::/3):
// everything else, the forms that embed an IPv4 address (IPv4-compatible, IPv4-translated, NAT64)
// included, is reserved, local or translates to a host no container may reach.
var globalUnicast6 = netip.MustParsePrefix("2000::/3")

// nonPublic are the ranges netip's own predicates and globalUnicast6 leave out that no container
// may reach: "this network" (0.0.0.0/8, where OrbStack puts the host), shared address space
// (carrier-grade NAT), IETF protocol assignments, documentation, benchmarking (Docker Desktop's
// host side), reserved and broadcast, and inside 2000::/3 the IETF protocol assignments (Teredo
// among them), documentation and 6to4.
var nonPublic = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

// Public reports whether a is an address on the public internet: not loopback, private
// (RFC 1918, unique local), link-local (169.254.0.0/16 with the cloud metadata services,
// fe80::/10), multicast, unspecified, an IPv6 address outside 2000::/3, or any range in
// nonPublic. An IPv4-mapped IPv6 address is judged as its IPv4 address.
func Public(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || !a.IsGlobalUnicast() || a.IsPrivate() || (a.Is6() && !globalUnicast6.Contains(a)) {
		return false
	}

	for _, p := range nonPublic {
		if p.Contains(a) {
			return false
		}
	}

	return true
}
