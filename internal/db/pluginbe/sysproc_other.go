//go:build !windows

package pluginbe

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}
