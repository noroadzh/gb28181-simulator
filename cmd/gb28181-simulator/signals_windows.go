//go:build windows
// +build windows

package main

import "os"

// pingSignal is unused on Windows because syscall.SIGUSR1 does not exist
// there. Declared as os.Interrupt only so the main.go call site compiles;
// startPingLoop ignores the value.
var pingSignal = os.Interrupt

// startPingLoop is a no-op on Windows because syscall.SIGUSR1 does not exist
// and SIGBREAK is not exported from the standard library's syscall package.
// Operators on Windows can still exercise the WebSocket stream by hitting an
// admin endpoint (Change 2+) or by sending any custom log via the API.
func startPingLoop(_ os.Signal) {}
