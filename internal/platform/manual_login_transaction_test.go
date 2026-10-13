package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A local fixture reports its own webdriver flag and persistent test data.
// No debugging connection is made to the interactive browser by this test.
// No Google page, account, password, token, or real user cookie is accessed.
func TestManualLoginTransaction(t *testing.T) {
	if os.Getenv("AF_TEST_HEADED_BROWSER") != "1" {
		t.Skip("set AF_TEST_HEADED_BROWSER=1 for the isolated interactive browser test")
	}
	type report struct {
		Webdriver bool `json:"webdriver"`
		Cookie    bool `json:"cookie"`
		Storage   bool `json:"storage"`
	}
	reports := make(chan report, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/report" && r.Method == http.MethodPost {
			var result report
			if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&result) != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			select {
			case reports <- result:
			default:
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Path != "/" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><title>Local login-mode diagnostic (no Google)</title><p>Disposable local browser test</p><script>
const result={webdriver:navigator.webdriver,cookie:document.cookie.includes('af_fixture=persisted'),storage:localStorage.getItem('af_fixture')==='persisted'};
document.cookie='af_fixture=persisted; Max-Age=3600; Path=/; SameSite=Strict';localStorage.setItem('af_fixture','persisted');
fetch('/report',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(result)});
</script>`)
	}))
	defer server.Close()
	exe, err := FindChrome("")
	noerr(t, err)
	profile := filepath.Join(t.TempDir(), "profile")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	receive := func() report {
		t.Helper()
		select {
		case r := <-reports:
			return r
		case <-ctx.Done():
			t.Fatal("local browser report timed out")
			return report{}
		}
	}
	b, err := startChrome(ctx, profile, exe, true, server.URL)
	noerr(t, err)
	defer b.Close()
	first := receive()
	debugArgument := false
	for _, arg := range b.cmd.Args {
		debugArgument = debugArgument || strings.HasPrefix(arg, "--remote-debugging")
	}
	_, portErr := os.Stat(filepath.Join(profile, "DevToolsActivePort"))
	noerr(t, b.Close())
	b, err = startChrome(ctx, profile, exe, false, server.URL)
	noerr(t, err)
	defer b.Close()
	second := receive()
	noerr(t, b.Close())
	result := map[string]any{"interactive_remote_debugging": debugArgument, "interactive_webdriver": first.Webdriver, "interactive_debug_port_file": portErr == nil, "headless_webdriver": second.Webdriver, "local_cookie_persisted": second.Cookie, "local_storage_persisted": second.Storage, "google_authentication_tested": false}
	raw, err := json.Marshal(result)
	noerr(t, err)
	fmt.Println("MANUAL_LOGIN_TRANSACTION " + string(raw))
	require(t, second.Cookie && second.Storage, "local fixture profile data was not preserved")
}
