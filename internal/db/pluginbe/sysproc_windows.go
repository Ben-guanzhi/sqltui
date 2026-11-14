//go:build windows

package pluginbe

import (
	"os/exec"
	"syscall"
)

// hideWindow keeps a plugin's console from flashing on screen: a driver
// process is a background detail the user never chose to see.
func hideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= 0x08000000 // CREATE_NO_WINDOW
}
