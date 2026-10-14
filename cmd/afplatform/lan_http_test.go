package main

import "testing"

func TestLANHTTPRequiresExplicitPrivateNetworkOptIn(t *testing.T) {
	for _, tc := range []struct {
		name, listen, origin, cert, key string
		allowHTTP, wantOK               bool
	}{
		{"default local", "127.0.0.1:8788", "http://127.0.0.1:8788", "", "", false, true},
		{"default LAN denied", "0.0.0.0:8788", "http://192.168.1.100:8788", "", "", false, false},
		{"LAN opt in", "0.0.0.0:8788", "http://192.168.1.100:8788", "", "", true, true},
		{"specific private bind", "192.168.1.100:8788", "http://192.168.1.100:8788", "", "", true, true},
		{"10 range", "0.0.0.0:8788", "http://10.1.2.3:8788", "", "", true, true},
		{"172 range", "0.0.0.0:8788", "http://172.16.2.3:8788", "", "", true, true},
		{"IPv6 private", "[::]:8788", "http://[fd12::1]:8788", "", "", true, true},
		{"local proxy", "127.0.0.1:8788", "http://192.168.1.100:8788", "", "", true, true},
		{"no public IP", "0.0.0.0:8788", "http://8.8.8.8:8788", "", "", true, false},
		{"no public bind", "8.8.8.8:8788", "http://192.168.1.100:8788", "", "", true, false},
		{"no unspecified URL", "0.0.0.0:8788", "http://0.0.0.0:8788", "", "", true, false},
		{"no unresolved host", "0.0.0.0:8788", "http://fold.example.org", "", "", true, false},
		{"no mixed https", "0.0.0.0:8788", "https://192.168.1.100:8788", "", "", true, false},
		{"no TLS flags", "0.0.0.0:8788", "http://192.168.1.100:8788", "cert", "key", true, false},
		{"no partial TLS", "0.0.0.0:8788", "http://192.168.1.100:8788", "cert", "", true, false},
		{"https still requires cert", "0.0.0.0:8788", "https://fold.example.org", "", "", false, false},
		{"direct https unchanged", "0.0.0.0:8788", "https://fold.example.org", "cert", "key", false, true},
		{"https proxy unchanged", "127.0.0.1:8788", "https://fold.example.org", "", "", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateServerNetwork(tc.listen, tc.origin, tc.cert, tc.key, tc.allowHTTP)
			if (err == nil) != tc.wantOK {
				t.Fatalf("want accepted=%v, got %v", tc.wantOK, err)
			}
		})
	}
}
