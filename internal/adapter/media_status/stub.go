// Package media_status — stub MediaStatusPort adapter.
//
// This is a minimal placeholder; a real implementation would store and
// forward media state in the online device table and upstream NOTIFYs.
package media_status

import (
	"context"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// Compile-time check.
var _ port.MediaStatusPort = (*PortAdapter)(nil)

// PortAdapter is the default adapter; it always returns ErrMediaStatusUnsupported.
type PortAdapter struct{}

// NewPortAdapter builds the adapter.
func NewPortAdapter() *PortAdapter { return &PortAdapter{} }

// HandleMediaStatus processes a MediaStatus notify from a downstream device.
func (*PortAdapter) HandleMediaStatus(ctx context.Context, report model.MediaStatusReport) error {
	return port.ErrMediaStatusUnsupported
}
