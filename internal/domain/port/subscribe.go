// Package port — Subscription.
package port

import (
	"context"
	"fmt"
)

// ErrSubscribeUnsupported is returned by a SubscribePort adapter that cannot
// handle the request (e.g. a stub).
var ErrSubscribeUnsupported = fmt.Errorf("port: subscribe unsupported")

// SubscribePort manages MANSCDP event subscriptions with downstream devices.
type SubscribePort interface {
	// Subscribe requests a device to start sending event notifications for
	// the given event package ("catalog", "alarm", "mobileposition", ...).
	// The returned subscription id is used to Unsubscribe later.
	Subscribe(ctx context.Context, deviceID, channelID, event string) (string, error)

	// Unsubscribe cancels a previously opened subscription.
	Unsubscribe(ctx context.Context, subscriptionID string) error
}

var _ SubscribePort = (*noopSubscribePort)(nil)

type noopSubscribePort struct{}

func (noopSubscribePort) Subscribe(ctx context.Context, deviceID, channelID, event string) (string, error) {
	return "", nil
}
func (noopSubscribePort) Unsubscribe(ctx context.Context, subscriptionID string) error { return nil }
