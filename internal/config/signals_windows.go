//go:build windows

package config

import (
	"os"
)

// reloadSignals returns the OS signals that trigger configuration hot reloading on Windows.
// Note: Windows does not implement POSIX SIGHUP, so file polling and HTTP reload triggers are primary.
func reloadSignals() []os.Signal {
	return []os.Signal{}
}
