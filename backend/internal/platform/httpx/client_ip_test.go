package httpx

import (
	"net/http/httptest"
	"testing"
)

func TestClientIPTrustBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, peer, forwarded, want string
		trust                       bool
	}{
		{"direct IPv4", "192.0.2.1:5000", "", "192.0.2.1", false},
		{"ignore spoofed header", "192.0.2.1:5000", "198.51.100.2", "192.0.2.1", false},
		{"trusted proxy", "192.0.2.1:5000", "198.51.100.2", "198.51.100.2", true},
		{"missing header", "192.0.2.1:5000", "", "192.0.2.1", true},
		{"invalid header", "192.0.2.1:5000", "not-an-ip", "192.0.2.1", true},
		{"reject forwarding chain", "192.0.2.1:5000", "198.51.100.2, 198.51.100.3", "192.0.2.1", true},
		{"IPv6", "[2001:db8::1]:5000", "", "2001:db8::1", false},
		{"mapped peer", "[::ffff:192.0.2.1]:5000", "", "192.0.2.1", false},
		{"mapped header", "192.0.2.1:5000", "::ffff:198.51.100.2", "198.51.100.2", true},
		{"bare peer", "192.0.2.1", "", "192.0.2.1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set("X-Real-IP", tc.forwarded)
			if got := ClientIP(r, tc.trust); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
