//go:build windows

package platform

import (
	"os"
	"sync"
	"syscall"
	"unsafe"
)

var manualWindows = struct {
	sync.Mutex
	pid, operation, count int
}{}

var manualUser32 = syscall.NewLazyDLL("user32.dll")
var manualWindowOwner = manualUser32.NewProc("GetWindowThreadProcessId")
var manualWindowVisible = manualUser32.NewProc("IsWindowVisible")
var manualWindowShow = manualUser32.NewProc("ShowWindow")
var manualWindowForeground = manualUser32.NewProc("SetForegroundWindow")
var manualWindowPost = manualUser32.NewProc("PostMessageW")

// A single callback avoids allocating a Windows callback on every poll/click.
// Operations are limited to the browser process we launched, not its contents.
var manualWindowCallback = syscall.NewCallback(func(hwnd, unused uintptr) uintptr {
	var pid uint32
	manualWindowOwner.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	visible, _, _ := manualWindowVisible.Call(hwnd)
	if int(pid) != manualWindows.pid || visible == 0 {
		return 1
	}
	manualWindows.count++
	switch manualWindows.operation {
	case 1:
		manualWindowShow.Call(hwnd, 9) // SW_RESTORE
		manualWindowForeground.Call(hwnd)
	case 2:
		manualWindowPost.Call(hwnd, 0x0010, 0, 0) // WM_CLOSE: normal browser shutdown
	case 3:
		manualWindowShow.Call(hwnd, 6) // SW_MINIMIZE, used by the local window test
	}
	return 1
})

func manualBrowserWindows(pid, operation int) int {
	manualWindows.Lock()
	defer manualWindows.Unlock()
	manualWindows.pid, manualWindows.operation, manualWindows.count = pid, operation, 0
	manualUser32.NewProc("EnumWindows").Call(manualWindowCallback, 0)
	return manualWindows.count
}
func manualDesktopAvailable() bool { return true }
func presentManualBrowser(pid int) (bool, error) {
	return manualBrowserWindows(pid, 1) > 0, nil
}
func closeManualBrowser(process *os.Process) {
	manualBrowserWindows(process.Pid, 2)
}
