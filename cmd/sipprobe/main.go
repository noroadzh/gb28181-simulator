// Command sipprobe is the diagnostic CLI used by Change 2's task §7.
//
// Usage:
//
//	sipprobe --bind udp://0.0.0.0:0 --send-to udp://target:5060 --expect-status 200
//	sipprobe --bind udp://0.0.0.0:5060             # receive-only mode
//
// All flag parsing and wiring now live in internal/sipprobe.RunCLI (Change 3
// §9.2) so this file is only an assembly entry point. Exit codes are
// documented in internal/sipprobe.
package main

import (
	"os"

	"github.com/your-org/gb28181-simulator/internal/sipprobe"
)

// Version metadata. Overridden at link time via -ldflags "-X main.version=...".
var (
	version = "0.1.0-dev"
	commit  = "unknown"
	builtAt = "unknown"
)

func main() {
	os.Exit(sipprobe.RunCLI(os.Args[1:], sipprobe.Version{
		Version: version,
		Commit:  commit,
		BuiltAt: builtAt,
	}))
}
