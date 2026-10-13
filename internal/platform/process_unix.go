//go:build !windows

package platform

import "os/exec"

func configureProcess(cmd *exec.Cmd, headed bool) {}
