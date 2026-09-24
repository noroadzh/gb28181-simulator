// Package servicectx provides a hand-written dependency-injection container.
// This file defines the well-known keys used throughout the application.
package servicectx

import (
	"github.com/your-org/gb28181-simulator/internal/domain/port"
	platformconfig "github.com/your-org/gb28181-simulator/internal/platform/config"
	"github.com/your-org/gb28181-simulator/internal/platform/observability/logging"
	"github.com/your-org/gb28181-simulator/internal/platform/observability/tracing"
)

// Well-known service keys for dependency injection.
var (
	// ConfigKey is the key for the application configuration.
	ConfigKey = NewKey[platformconfig.Config]("config")

	// LoggerKey is the key for the logging hub.
	LoggerKey = NewKey[*logging.Hub]("logger")

	// TracingKey is the key for the tracing provider.
	TracingKey = NewKey[*tracing.Provider]("tracing")

	// StorageKey is the key for the storage store. The value type is the
	// domain port rather than the concrete *storage.Store: this package is
	// platform infrastructure and must not depend on an adapter, otherwise
	// the layering it exists to protect is broken (design D10).
	StorageKey = NewKey[port.Storage]("storage")
)