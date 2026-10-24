package api

import (
	"net/netip"
	"testing"
)

// "Same network as this browser" (Device-Approve): the same address, or for IPv6 the same /64,
// which is all the identity service keeps of one; unknown on either side is never the same.
func TestSameNetwork(t *testing.T) {
	t.Parallel()

	const cliIP = "203.0.113.7"

	tests := []struct {
		recorded, browser string
		want              bool
	}{
		{cliIP, cliIP, true},
		{cliIP, "203.0.113.8", false},
		{"::ffff:" + cliIP, cliIP, true},
		{"2001:db8:1:2::", "2001:db8:1:2:aaaa:bbbb:cccc:dddd", true},
		{"2001:db8:1:3::", "2001:db8:1:2:aaaa:bbbb:cccc:dddd", false},
		{cliIP, "2001:db8:1:2::1", false},
		{"", cliIP, false},
		{"not an address", cliIP, false},
		{cliIP, "", false},
	}

	for _, tt := range tests {
		var browser netip.Addr
		if tt.browser != "" {
			browser = netip.MustParseAddr(tt.browser)
		}

		if got := sameNetwork(tt.recorded, browser); got != tt.want {
			t.Errorf("sameNetwork(%q, %q) = %v; want %v", tt.recorded, tt.browser, got, tt.want)
		}
	}
}
