package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

type loginFixtureBrowser struct {
	mockBrowser
	shown     int
	showError error
}

func (b *loginFixtureBrowser) Alive() bool { return !b.closed.Load() }
func (b *loginFixtureBrowser) ShowLogin(context.Context) error {
	if !b.Alive() {
		return fail("BROWSER_DISCONNECTED")
	}
	if b.showError != nil {
		return b.showError
	}
	b.shown++
	return nil
}
func loginFixture(t *testing.T) (*Engine, *[]*loginFixtureBrowser) {
	t.Helper()
	s := newTestStore(t)
	windows := []*loginFixtureBrowser{}
	pool := NewBrowserPool(1, time.Minute, func(_ context.Context, _ string, headed bool) (Browser, error) {
		require(t, headed, "login must be headed")
		b := &loginFixtureBrowser{}
		windows = append(windows, b)
		return b, nil
	})
	t.Cleanup(pool.Close)
	noerr(t, s.Update(func(s *State) error {
		s.Accounts["test-account"] = Account{ID: "test-account", Enabled: true, Status: "needs_login", LastError: "BROWSER_DISCONNECTED"}
		return nil
	}))
	return NewEngine(s, pool), &windows
}
func TestLoginWindowTransaction(t *testing.T) {
	e, windows := loginFixture(t)
	ctx := context.Background()
	noerr(t, e.AccountAction(ctx, "test-account", "login"))
	noerr(t, (*windows)[0].Close())
	noerr(t, e.AccountAction(ctx, "test-account", "login"))
	noerr(t, e.AccountAction(ctx, "test-account", "login"))
	last := (*windows)[len(*windows)-1]
	a := e.Store.Snapshot().Accounts["test-account"]
	result := map[string]any{"launches": len(*windows), "cached_browser_dead": !last.Alive(), "show_calls_on_current": last.shown, "last_error": a.LastError, "active_slots": e.Browsers.Stats()["active"]}
	b, _ := json.Marshal(result)
	fmt.Println("LOGIN_TRANSACTION " + string(b))
}

func TestLoginWindowRecoversAndRejectsMissingWindow(t *testing.T) {
	e, windows := loginFixture(t)
	ctx := context.Background()
	noerr(t, e.AccountAction(ctx, "test-account", "login"))
	require(t, (*windows)[0].shown == 1, "login not presented")
	noerr(t, (*windows)[0].Close())
	noerr(t, e.AccountAction(ctx, "test-account", "login"))
	require(t, len(*windows) == 2 && (*windows)[1].Alive(), "dead cached browser reused")
	require(t, e.Store.Snapshot().Accounts["test-account"].LastError == "", "stale error")
	(*windows)[1].showError = fail("LOGIN_WINDOW_UNAVAILABLE")
	launch := e.Browsers.launch
	e.Browsers.launch = func(ctx context.Context, id string, headed bool) (Browser, error) {
		b, err := launch(ctx, id, headed)
		if err == nil {
			b.(*loginFixtureBrowser).showError = fail("LOGIN_WINDOW_UNAVAILABLE")
		}
		return b, err
	}
	err := e.AccountAction(ctx, "test-account", "login")
	require(t, err != nil && err.Error() == "LOGIN_WINDOW_UNAVAILABLE", err)
	require(t, len(*windows) == 3, "window recovery must be attempted only once")
	require(t, e.Browsers.Stats()["active"] == 0, "failed window still cached")
}

func TestClosedLoginWindowDoesNotBlockOtherAccount(t *testing.T) {
	e, windows := loginFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	noerr(t, e.Store.Update(func(s *State) error { s.Accounts["other"] = Account{ID: "other", Enabled: true}; return nil }))
	noerr(t, e.AccountAction(ctx, "test-account", "login"))
	noerr(t, (*windows)[0].Close())
	noerr(t, e.AccountAction(ctx, "other", "login"))
	require(t, len(*windows) == 2, "dead login consumed the only slot")
	noerr(t, (*windows)[1].Close())
	e.Browsers.Sweep()
	require(t, e.Browsers.Stats()["active"] == 0, "dead browser not swept")
}
