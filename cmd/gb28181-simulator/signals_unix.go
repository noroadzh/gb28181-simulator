//go:build !windows
// +build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/your-org/gb28181-simulator/internal/platform/observability/logging"
)

// pingSignal is the platform-specific operator signal used to drive a sample
// log record over the WebSocket stream. SIGUSR1 on Unix.
var pingSignal os.Signal = syscall.SIGUSR1

// startPingLoop wires the operator signal handler. When the signal arrives it
// emits a structured info log that includes a `password` attribute so the
// redaction path is exercised end-to-end.
func startPingLoop(sig os.Signal) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, sig)
	go func() {
		for range sigCh {
			logging.L().Info("manual ping",
				"reason", sig.String(),
				"password", "should-never-leak",
				"uptime_s", time.Since(startedAt).Seconds(),
			)
		}
	}()
}
