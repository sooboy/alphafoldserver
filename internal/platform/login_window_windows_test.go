//go:build windows

package platform

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func hasVisibleBrowserWindow(pid int) bool {
	user32 := syscall.NewLazyDLL("user32.dll")
	visible := user32.NewProc("IsWindowVisible")
	owner := user32.NewProc("GetWindowThreadProcessId")
	enum := user32.NewProc("EnumWindows")
	found := false
	cb := syscall.NewCallback(func(hwnd, unused uintptr) uintptr {
		var process uint32
		owner.Call(hwnd, uintptr(unsafe.Pointer(&process)))
		v, _, _ := visible.Call(hwnd)
		if int(process) == pid && v != 0 {
			found = true
			return 0
		}
		return 1
	})
	enum.Call(cb, 0)
	return found
}
func TestRealHeadedLoginWindow(t *testing.T) {
	if os.Getenv("AF_TEST_HEADED_BROWSER") != "1" {
		t.Skip("set AF_TEST_HEADED_BROWSER=1 to show a disposable diagnostic browser")
	}
	exe, err := FindChrome("")
	noerr(t, err)
	s := newTestStore(t)
	profile := filepath.Join(s.Dir, "profile")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var browser *Chrome
	launches := 0
	pool := NewBrowserPool(1, time.Minute, func(ctx context.Context, _ string, headed bool) (Browser, error) {
		require(t, headed, "login unexpectedly headless")
		b, err := startChrome(ctx, profile, exe, headed, "about:blank")
		if err == nil {
			browser = b
			launches++
		}
		return b, err
	})
	defer pool.Close()
	e := NewEngine(s, pool)
	noerr(t, s.Update(func(s *State) error {
		s.Accounts["diagnostic"] = Account{ID: "diagnostic", Status: "needs_login", Enabled: true}
		return nil
	}))
	checkVisible := func() {
		t.Helper()
		until := time.Now().Add(5 * time.Second)
		for time.Now().Before(until) {
			if hasVisibleBrowserWindow(browser.cmd.Process.Pid) {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("No visible native Windows window owned by launched browser")
	}
	noerr(t, e.AccountAction(ctx, "diagnostic", "login"))
	checkVisible()
	// The human authentication browser has no CDP endpoint and rejects Eval.
	require(t, browser.port == 0 && browser.browserWS == "", "interactive browser exposed CDP")
	var value any
	err = browser.Eval(ctx, "1+1", &value)
	require(t, err != nil && err.Error() == "MANUAL_LOGIN_INTERACTIVE_ONLY", "interactive browser accepted script execution")
	require(t, manualBrowserWindows(browser.cmd.Process.Pid, 3) > 0, "window missing before minimize")
	noerr(t, e.AccountAction(ctx, "diagnostic", "login"))
	checkVisible()
	require(t, launches == 1, "duplicate visible window")

	// WM_CLOSE is the same native close request as the window title-bar X.
	user32 := syscall.NewLazyDLL("user32.dll")
	post := user32.NewProc("PostMessageW")
	owner := user32.NewProc("GetWindowThreadProcessId")
	visible := user32.NewProc("IsWindowVisible")
	count := 0
	callback := syscall.NewCallback(func(hwnd, unused uintptr) uintptr {
		var pid uint32
		owner.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		v, _, _ := visible.Call(hwnd)
		if int(pid) == browser.cmd.Process.Pid && v != 0 {
			ok, _, _ := post.Call(hwnd, 0x0010, 0, 0)
			if ok != 0 {
				count++
			}
		}
		return 1
	})
	user32.NewProc("EnumWindows").Call(callback, 0)
	require(t, count > 0, "WM_CLOSE was not delivered")
	deadline := time.Now().Add(10 * time.Second)
	for hasVisibleBrowserWindow(browser.cmd.Process.Pid) && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	require(t, !hasVisibleBrowserWindow(browser.cmd.Process.Pid), "native window did not close")
	t.Log("native WM_CLOSE delivered before authentication; now requesting login again")
	noerr(t, e.AccountAction(ctx, "diagnostic", "login"))
	checkVisible()
	require(t, launches == 2, "closed browser was not restarted")
	t.Log("ordinary Windows browser: no CDP; Eval denied; native visible window -> minimize -> login presents window -> native WM_CLOSE before authentication -> next login launches second visible window; isolated profile; no Google authentication or prediction")
}
