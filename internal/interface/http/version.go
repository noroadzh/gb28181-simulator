package httpapi

import "runtime"

func runtimeVersion() string  { return runtime.Version() }
func runtimePlatform() string { return runtime.GOOS + "/" + runtime.GOARCH }
