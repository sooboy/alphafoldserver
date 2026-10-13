package platform

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestInteractiveBrowserArguments(t *testing.T) {
	args := chromeArguments("server/profiles/account", true, "https://alphafoldserver.com/")
	require(t, len(args) == 5, args)
	for _, arg := range args {
		for _, forbidden := range []string{"remote-debugging", "headless", "automation", "disable-", "no-sandbox", "user-agent"} {
			require(t, !strings.Contains(arg, forbidden), args)
		}
	}
	require(t, args[0] == "--user-data-dir=server/profiles/account", args)
	automated := strings.Join(chromeArguments("server/profiles/account", false, "https://alphafoldserver.com/"), " ")
	require(t, strings.Contains(automated, "--headless=new") && strings.Contains(automated, "--remote-debugging-port=0"), automated)
}

func TestManualLoginThenSeparateVerification(t *testing.T) {
	for _, rpcError := range []string{"", "AUTH_REQUIRED"} {
		t.Run("verification-"+rpcError, func(t *testing.T) {
			s := newTestStore(t)
			f := &remoteFixture{}
			if rpcError != "" {
				f.failAt, f.errorCode = "EU26qc", rpcError
			}
			interactive := &loginFixtureBrowser{}
			automatedLaunches := 0
			pool := NewBrowserPool(1, time.Minute, func(ctx context.Context, id string, headed bool) (Browser, error) {
				if headed {
					return interactive, nil
				}
				automatedLaunches++
				return f.launcher(ctx, id, false)
			})
			t.Cleanup(pool.Close)
			e := NewEngine(s, pool)
			noerr(t, s.Update(func(s *State) error {
				s.Accounts["test-account"] = Account{ID: "test-account", Enabled: true, Status: "needs_login"}
				return nil
			}))
			ctx := context.Background()
			noerr(t, e.AccountAction(ctx, "test-account", "login"))
			for _, action := range []string{"finish-login", "refresh"} {
				err := e.AccountAction(ctx, "test-account", action)
				require(t, err != nil && err.Error() == "LOGIN_WINDOW_OPEN", err)
			}
			require(t, automatedLaunches == 0 && f.count("EU26qc") == 0, "automation ran during human authentication")
			require(t, s.Snapshot().Accounts["test-account"].Status == "needs_login", "open window falsely marked authenticated")
			noerr(t, interactive.Close())
			err := e.AccountAction(ctx, "test-account", "finish-login")
			if rpcError == "" {
				noerr(t, err)
				require(t, s.Snapshot().Accounts["test-account"].Status == "ready", "verified fixture not ready")
			} else {
				require(t, err != nil && s.Snapshot().Accounts["test-account"].Status != "ready", "authentication failure accepted")
			}
			require(t, automatedLaunches == 1 && f.count("EU26qc") == 1, "separate verification was not launched")
			require(t, pool.Stats()["active"] == 0, "verification browser was not released")
		})
	}
}
