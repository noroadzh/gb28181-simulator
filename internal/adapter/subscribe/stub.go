// Package subscribe — stub SubscribePort adapter.
//
// This is a minimal placeholder; a real implementation would send SUBSCRIBE
// and NOTIFY over SIP MESSAGE to/from downstream devices.
package subscribe

import (
	"context"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// Compile-time check.
var _ port.SubscribePort = (*PortAdapter)(nil)

// PortAdapter is the default adapter; it always returns ErrSubscribeUnsupported.
type PortAdapter struct{}

// NewPortAdapter builds the adapter.
func NewPortAdapter() *PortAdapter { return &PortAdapter{} }

// Subscribe requests a device to start sending event notifications for the
// given event package.
func (*PortAdapter) Subscribe(ctx context.Context, deviceID, channelID, event string) (string, error) {
	return "", port.ErrSubscribeUnsupported
}

// Unsubscribe cancels a previously opened subscription.
func (*PortAdapter) Unsubscribe(ctx context.Context, subscriptionID string) error {
	return port.ErrSubscribeUnsupported
}

// Notify pushes a catalog change notification to a downstream device.
func (*PortAdapter) Notify(ctx context.Context, deviceID, callID string, catalog model.Catalog) error {
	return port.ErrSubscribeUnsupported
}
