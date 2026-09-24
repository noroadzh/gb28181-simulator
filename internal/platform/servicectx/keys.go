// Package servicectx provides a hand-written dependency-injection container.
// This file defines the well-known keys used throughout the application.
package servicectx

import (
	platformconfig "github.com/your-org/gb28181-simulator/internal/platform/config"
	"github.com/your-org/gb28181-simulator/internal/platform/observability/logging"
	"github.com/your-org/gb28181-simulator/internal/platform/observability/tracing"
	"github.com/your-org/gb28181-simulator/internal/storage"
)

// Well-known service keys for dependency injection.
var (
	// ConfigKey is the key for the application configuration.
	ConfigKey = NewKey[platformconfig.Config]("config")

	// LoggerKey is the key for the logging hub.
	LoggerKey = NewKey[*logging.Hub]("logger")

	// TracingKey is the key for the tracing provider.
	TracingKey = NewKey[*tracing.Provider]("tracing")

	// StorageKey is the key for the storage store.
	StorageKey = NewKey[*storage.Store]("storage")
)