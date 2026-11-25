// Package version holds build metadata injected with -ldflags at release time.
package version

import "runtime"

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
	Channel = "dev"
)

// String is the one-line version shown by `ptb version`.
func String() string {
	return "ptb " + Version + " (" + Channel + ", " + Commit + ", " + Date + ", " + runtime.GOOS + "/" + runtime.GOARCH + ")"
}
