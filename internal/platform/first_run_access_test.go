package platform

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFirstRunLocalHomeCanonicalization(t *testing.T) {
	store := newTestStore(t)
	s, err := NewServer(NewEngine(store, nil), "http://127.0.0.1:8788")
	noerr(t, err)
	cases := []struct {
		name, method, path, host, remote, origin, auth, forwarded string
		status                                                    int
	}{
		{"localhost", "GET", "/", "localhost:8788", "127.0.0.1:4321", "", "", "", 307},
		{"IPv6 alias", "GET", "/", "[::1]:8788", "[::1]:4321", "", "", "", 307},
		{"mapped peer", "GET", "/", "localhost:8788", "[::ffff:127.0.0.1]:4321", "", "", "", 307},
		{"canonical", "GET", "/", "127.0.0.1:8788", "127.0.0.1:4321", "", "", "", 200},
		{"wrong port", "GET", "/", "localhost:9999", "127.0.0.1:4321", "", "", "", 403},
		{"missing port", "GET", "/", "localhost", "127.0.0.1:4321", "", "", "", 403},
		{"external host", "GET", "/", "evil.example:8788", "127.0.0.1:4321", "", "", "", 403},
		{"hostname suffix", "GET", "/", "localhost.evil.example:8788", "127.0.0.1:4321", "", "", "", 403},
		{"nonlocal peer", "GET", "/", "localhost:8788", "192.0.2.1:4321", "", "", "", 403},
		{"unknown peer", "GET", "/", "localhost:8788", "", "", "", "", 403},
		{"forwarded peer untrusted", "GET", "/", "localhost:8788", "192.0.2.1:4321", "", "", "127.0.0.1", 403},
		{"foreign origin", "GET", "/", "localhost:8788", "127.0.0.1:4321", "http://evil.example", "", "", 403},
		{"origin with canonical host", "GET", "/", "127.0.0.1:8788", "127.0.0.1:4321", "http://evil.example", "", "", 403},
		{"authorization", "GET", "/", "localhost:8788", "127.0.0.1:4321", "", "Bearer fixture", "", 403},
		{"setup API", "GET", "/api/v1/setup", "localhost:8788", "127.0.0.1:4321", "", "", "", 403},
		{"assets", "GET", "/assets/app.js", "localhost:8788", "127.0.0.1:4321", "", "", "", 403},
		{"POST", "POST", "/", "localhost:8788", "127.0.0.1:4321", "", "", "", 403},
		{"HEAD", "HEAD", "/", "localhost:8788", "127.0.0.1:4321", "", "", "", 403},
		{"query", "GET", "/?next=https://evil.example", "localhost:8788", "127.0.0.1:4321", "", "", "", 403},
		{"userinfo", "GET", "/", "x@localhost:8788", "127.0.0.1:4321", "", "", "", 403},
		{"path in host", "GET", "/", "localhost:8788/x", "127.0.0.1:4321", "", "", "", 403},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "http://127.0.0.1:8788"+tc.path, nil)
			r.Host, r.RemoteAddr = tc.host, tc.remote
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Authorization", tc.auth)
			r.Header.Set("X-Forwarded-For", tc.forwarded)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			require(t, w.Code == tc.status, w.Code, w.Body.String())
			if tc.status == 307 {
				require(t, w.Header().Get("Location") == s.Origin+"/", w.Header())
			} else {
				require(t, w.Header().Get("Location") == "", w.Header())
			}
			if tc.status == 403 {
				require(t, w.Body.String() == "{\"error\":\"FORBIDDEN\"}\n", w.Body.String())
			}
		})
	}
	require(t, len(store.Snapshot().Users) == 0, "navigation must not initialize an admin")
}

func TestFirstRunCanonicalOriginScope(t *testing.T) {
	for _, tc := range []struct {
		origin, host string
		status       int
	}{
		{"http://localhost:8788", "127.0.0.1:8788", 307},
		{"http://[::1]:8788", "localhost:8788", 307},
		{"http://127.0.0.1", "localhost:80", 307},
		{"http://127.0.0.1:80", "localhost", 307},
		{"http://team.example:8788", "localhost:8788", 403},
		{"https://127.0.0.1:8788", "localhost:8788", 403},
		{"https://team.example", "localhost", 403},
	} {
		t.Run(tc.origin, func(t *testing.T) {
			s, err := NewServer(NewEngine(newTestStore(t), nil), tc.origin)
			noerr(t, err)
			r := httptest.NewRequest(http.MethodGet, "http://localhost/", nil)
			r.Host, r.RemoteAddr = tc.host, "127.0.0.1:4321"
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			require(t, w.Code == tc.status, w.Code, w.Body.String())
		})
	}
}

func TestFirstRunWrongAddressHTMLHint(t *testing.T) {
	s, err := NewServer(NewEngine(newTestStore(t), nil), "http://127.0.0.1:8788")
	noerr(t, err)
	for _, path := range []string{"/", "/api/v1/setup"} {
		r := httptest.NewRequest(http.MethodGet, "http://wrong.example"+path, nil)
		r.Header.Set("Accept", "text/html")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		require(t, w.Code == 403, w.Code)
		require(t, w.Header().Get("Location") == "", "no arbitrary-host redirect")
		require(t, w.Header().Get("Cache-Control") == "no-store", "must not cache error")
		require(t, !strings.Contains(w.Body.String(), s.setupToken), "must not disclose setup token")
		require(t, !strings.Contains(w.Body.String(), "wrong.example"), "must not reflect request host")
		if path == "/" {
			require(t, strings.HasPrefix(w.Header().Get("Content-Type"), "text/html"), w.Header())
			require(t, strings.Contains(w.Body.String(), "-public-url"), "missing configuration hint")
			require(t, strings.Contains(w.Body.String(), "不需要执行"), "must not suggest init")
		} else {
			require(t, w.Body.String() == "{\"error\":\"FORBIDDEN\"}\n", "API error format changed")
		}
	}
}
