package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

// The native window can disappear before the process-done signal is observed.
// Keep Alive=true while making window presentation fail to reproduce that race.
func loginCloseRace(t *testing.T) map[string]any {
	t.Helper()
	e, windows := loginFixture(t)
	ctx := context.Background()
	noerr(t, e.AccountAction(ctx, "test-account", "login"))
	(*windows)[0].showError = fail("BROWSER_DISCONNECTED")
	err := e.AccountAction(ctx, "test-account", "login")
	code := ""
	if err != nil {
		code = err.Error()
	}
	a := e.Store.Snapshot().Accounts["test-account"]
	last := (*windows)[len(*windows)-1]
	return map[string]any{"error": code, "launches": len(*windows), "active_slots": e.Browsers.Stats()["active"], "current_alive": last.Alive(), "show_calls_on_current": last.shown, "status": a.Status, "last_error": a.LastError}
}

func TestLoginReopenTransaction(t *testing.T) {
	b, err := json.Marshal(loginCloseRace(t))
	noerr(t, err)
	fmt.Println("LOGIN_REOPEN_TRANSACTION " + string(b))
}

func TestLoginCloseRaceRecoveredInSingleClick(t *testing.T) {
	r := loginCloseRace(t)
	require(t, r["error"] == "" && r["launches"] == 2 && r["active_slots"] == 1 && r["current_alive"] == true, r)
	require(t, r["last_error"] == "" && r["status"] == "needs_login", r)
}

func TestLoginRecoveryBoundaries(t *testing.T) {
	for _, code := range []string{"BROWSER_DISCONNECTED_OR_TIMEOUT", "LOGIN_WINDOW_UNAVAILABLE", "BROWSER_LOGIN_UNSUPPORTED", "OTHER_ERROR"} {
		t.Run(code, func(t *testing.T) {
			e, windows := loginFixture(t)
			ctx := context.Background()
			noerr(t, e.AccountAction(ctx, "test-account", "login"))
			(*windows)[0].showError = fail(code)
			err := e.AccountAction(ctx, "test-account", "login")
			if code == "BROWSER_DISCONNECTED_OR_TIMEOUT" || code == "LOGIN_WINDOW_UNAVAILABLE" {
				noerr(t, err)
				require(t, len(*windows) == 2, "missing replacement browser")
			} else {
				require(t, err != nil && err.Error() == code && len(*windows) == 1, "non-window error retried")
				require(t, e.Browsers.Stats()["active"] == 0, "failed browser retained")
			}
		})
	}
	t.Run("replacement-launch-fails", func(t *testing.T) {
		e, windows := loginFixture(t)
		ctx := context.Background()
		noerr(t, e.AccountAction(ctx, "test-account", "login"))
		(*windows)[0].showError = fail("BROWSER_DISCONNECTED")
		attempts := 0
		e.Browsers.launch = func(context.Context, string, bool) (Browser, error) {
			attempts++
			return nil, fail("BROWSER_START_FAILED")
		}
		err := e.AccountAction(ctx, "test-account", "login")
		require(t, err != nil && err.Error() == "BROWSER_START_FAILED" && attempts == 1, err)
		require(t, e.Browsers.Stats()["active"] == 0, "failed replacement consumed a slot")
	})
}
