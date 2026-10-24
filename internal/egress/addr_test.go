package egress

import (
	"net/netip"
	"testing"
)

func TestPublic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		addr string
		want bool
	}{
		{"8.8.8.8", true},
		{"160.79.104.10", true},
		{"2606:4700::6810:84e5", true},
		{"::ffff:8.8.8.8", true},
		{"127.0.0.1", false},
		{"127.8.9.10", false},
		{"::1", false},
		{"0.0.0.0", false},
		{"::", false},
		{"10.0.0.1", false},
		{"172.16.0.1", false},
		{"172.17.0.1", false}, // Docker's default bridge on Linux
		{"192.168.215.1", false},
		{"169.254.169.254", false}, // cloud metadata
		{"169.254.170.2", false},   // ECS task metadata
		{"fd00:ec2::254", false},   // AWS metadata over IPv6
		{"fe80::1", false},
		{"0.250.250.254", false},  // OrbStack's host.docker.internal
		{"198.19.248.254", false}, // OrbStack's host side
		{"192.168.65.254", false}, // Docker Desktop's host.docker.internal
		{"100.64.0.1", false},
		{"192.0.2.10", false},
		{"203.0.113.9", false},
		{"224.0.0.251", false},
		{"255.255.255.255", false},
		{"240.0.0.1", false},
		{"::ffff:127.0.0.1", false},
		{"::ffff:169.254.169.254", false},
		{"64:ff9b::a00:1", false}, // NAT64 of 10.0.0.1
		{"2002:a00:1::1", false},  // 6to4 of 10.0.0.1
		{"2001:db8::1", false},
		{"ff02::1", false},
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			t.Parallel()

			if got := Public(netip.MustParseAddr(tt.addr)); got != tt.want {
				t.Errorf("Public(%s) = %v, want %v", tt.addr, got, tt.want)
			}
		})
	}
}
