//go:build windows

package platform

import (
	"os/exec"
	"syscall"
)

func configureProcess(cmd *exec.Cmd, headed bool) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: !headed}
}
