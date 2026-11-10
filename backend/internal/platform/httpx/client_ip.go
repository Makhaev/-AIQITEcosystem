package httpx

import (
	"net"
	"net/http"
	"net/netip"
)

// ClientIP uses only the peer address unless the application is behind a
// trusted proxy that overwrites X-Real-IP. Never use client-supplied forwarding
// chains as rate-limit keys.
func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if ip, err := netip.ParseAddr(r.Header.Get("X-Real-IP")); err == nil {
			return ip.Unmap().String()
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Unmap().String()
	}
	return host
}
