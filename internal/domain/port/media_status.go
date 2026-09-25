// Package port — Media status.
package port

import (
	"context"
	"fmt"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// ErrMediaStatusUnsupported is returned by a MediaStatusPort adapter that
// cannot handle the request (e.g. a stub).
var ErrMediaStatusUnsupported = fmt.Errorf("port: media status unsupported")

// MediaStatusPort handles MediaStatus notify messages from downstream devices.
type MediaStatusPort interface {
	// HandleMediaStatus processes a MediaStatus notify from a downstream device.
	HandleMediaStatus(ctx context.Context, report model.MediaStatusReport) error
}

var _ MediaStatusPort = (*noopMediaStatusPort)(nil)

type noopMediaStatusPort struct{}

func (noopMediaStatusPort) HandleMediaStatus(ctx context.Context, report model.MediaStatusReport) error {
	return nil
}
