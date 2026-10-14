package platform

import (
	"net/http/httptest"
	"testing"
)

func TestLANHTTPRejectsNonprivatePeersAndRetainsOriginChecks(t *testing.T) {
	s, err := NewServer(NewEngine(newTestStore(t), nil), "http://192.168.1.100:8788")
	noerr(t, err)
	s.PrivateNetworkOnly = true
	for _, tc := range []struct {
		name, peer, host, origin, forwarded string
		status                              int
	}{
		{"LAN192", "192.168.1.101:1234", s.host, s.Origin, "", 200},
		{"LAN10", "10.2.3.4:1234", s.host, s.Origin, "", 200},
		{"LAN172", "172.31.1.2:1234", s.host, s.Origin, "", 200},
		{"ULA", "[fd12::2]:1234", s.host, s.Origin, "", 200},
		{"loopback", "127.0.0.1:1234", s.host, s.Origin, "", 200},
		{"mapped private", "[::ffff:192.168.1.101]:1234", s.host, s.Origin, "", 200},
		{"public peer", "8.8.8.8:1234", s.host, s.Origin, "", 403},
		{"public mapped", "[::ffff:8.8.8.8]:1234", s.host, s.Origin, "", 403},
		{"not RFC1918", "172.32.1.2:1234", s.host, s.Origin, "", 403},
		{"public IPv6", "[2606:4700:4700::1111]:1234", s.host, s.Origin, "", 403},
		{"unknown peer", "", s.host, s.Origin, "", 403},
		{"forged forwarded", "8.8.8.8:1234", s.host, s.Origin, "192.168.1.101", 403},
		{"foreign host", "192.168.1.101:1234", "evil.example", s.Origin, "", 403},
		{"foreign origin", "192.168.1.101:1234", s.host, "http://evil.example", "", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", s.Origin+"/api/v1/setup", nil)
			r.Host, r.RemoteAddr = tc.host, tc.peer
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("X-Forwarded-For", tc.forwarded)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			require(t, w.Code == tc.status, w.Code, w.Body.String())
		})
	}
}
