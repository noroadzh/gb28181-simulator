package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// PTZ and Records used to return nil, which the HTTP layer rendered as 200:
// the dashboard reported a camera had moved and a channel had no recordings,
// neither of which had been checked. Until the signalling lands they must say
// so with the typed sentinels the HTTP layer maps to 501.
func TestPTZAndRecordsReportUnsupported(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _ := newTestService(t)
	id := mustParse(t, "34020000001310000001")

	var ptzErr port.ErrPTZUnsupported
	if err := svc.PTZ(ctx, id, "ch-1", "up", 128, 0); !errors.As(err, &ptzErr) {
		t.Fatalf("PTZ err = %v, want ErrPTZUnsupported", err)
	} else if ptzErr.NodeID != id || ptzErr.ChannelID != "ch-1" {
		t.Errorf("PTZ error carries (%s,%s), want (%s,ch-1)", ptzErr.NodeID, ptzErr.ChannelID, id)
	}

	var recErr port.ErrRecordUnsupported
	if _, err := svc.Records(ctx, id, "ch-1", time.Now().Add(-time.Hour), time.Now()); !errors.As(err, &recErr) {
		t.Fatalf("Records err = %v, want ErrRecordUnsupported", err)
	}
}
