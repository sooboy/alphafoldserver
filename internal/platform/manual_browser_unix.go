//go:build !windows

package platform

import (
	"os"
	"runtime"
	"syscall"
)

func manualDesktopAvailable() bool {
	return runtime.GOOS != "linux" || os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}
func presentManualBrowser(pid int) (bool, error) {
	if !manualDesktopAvailable() {
		return false, fail("LOGIN_REQUIRES_SERVER_DESKTOP")
	}
	// The Linux desktop/window manager owns presentation. Unlike Windows, we
	// do not claim native-window visibility verification on this platform.
	return true, nil
}
func closeManualBrowser(process *os.Process) {
	_ = process.Signal(syscall.SIGTERM)
}
