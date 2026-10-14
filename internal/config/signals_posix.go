//go:build !windows

package config

import (
	"os"
	"syscall"
)

// reloadSignals returns the OS signals that trigger configuration hot reloading on POSIX platforms.
func reloadSignals() []os.Signal {
	return []os.Signal{syscall.SIGHUP}
}
