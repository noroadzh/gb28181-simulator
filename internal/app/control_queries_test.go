package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/adapter/manscdp"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// waitForSend returns the first message the fake transport delivered.
func waitForSend(t *testing.T, tr *acceptorTransport) model.Message {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sent := tr.delivered(); len(sent) > 0 {
			return sent[0]
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no message was sent")
	return model.Message{}
}

// The query leaves the node, the answer comes back on the same socket, and
// the waiter gets the parsed list.
func TestQueryRecords_RoundTrip(t *testing.T) {
	deviceID := mustParse(t, relayDeviceID)
	h := newDeviceHarness(t, deviceID, nil, nil)
	ctx := context.Background()

	type outcome struct {
		items []model.RecordInfoItem
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		items, err := h.acceptor.QueryRecords(ctx, deviceID, deviceID.String(),
			"127.0.0.1:15060", "ch-1", time.Time{}, time.Time{})
		done <- outcome{items, err}
	}()

	query := waitForSend(t, h.tr)
	if query.Method() != "MESSAGE" {
		t.Fatalf("sent %q, want a MESSAGE", query.Method())
	}
	if got := headerValue(query, "Call-ID"); got == "" {
		t.Fatal("query carries no Call-ID, so its answer could not be matched")
	}

	item := model.NewRecordInfoItem(model.RecordInfoItemParams{
		Name:      "clip-1",
		DeviceID:  deviceID.String(),
		ChannelID: "ch-1",
		StartTime: "2026-10-01T00:00:00",
		EndTime:   "2026-10-01T00:05:00",
	})
	resp, err := model.NewRecordInfoResponse(deviceID.String(), 1, []model.RecordInfoItem{item})
	if err != nil {
		t.Fatalf("NewRecordInfoResponse: %v", err)
	}
	body, err := manscdp.NewMANSCDPCodec().MarshalRecordInfoResponse(resp)
	if err != nil {
		t.Fatalf("MarshalRecordInfoResponse: %v", err)
	}
	answer, err := model.NewResponse(200, "OK", []model.Header{
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-record"),
		model.NewHeader("Call-ID", headerValue(query, "Call-ID")),
		model.NewHeader("CSeq", headerValue(query, "CSeq")),
	}, body)
	if err != nil {
		t.Fatalf("NewResponse: %v", err)
	}
	h.tr.inbound <- answer

	select {
	case out := <-done:
		if out.err != nil {
			t.Fatalf("QueryRecords: %v", out.err)
		}
		if len(out.items) != 1 || out.items[0].Name() != "clip-1" {
			t.Errorf("items = %v, want the one clip", out.items)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("QueryRecords never returned")
	}
}

func TestQueryRecords_TimeoutIsDistinguishable(t *testing.T) {
	deviceID := mustParse(t, relayDeviceID)
	h := newDeviceHarness(t, deviceID, nil, nil)
	h.acceptor.controlWait = 30 * time.Millisecond

	_, err := h.acceptor.QueryRecords(context.Background(), deviceID, deviceID.String(),
		"127.0.0.1:15060", "ch-1", time.Time{}, time.Time{})
	if !errors.Is(err, port.ErrControlTimeout) {
		t.Fatalf("err = %v, want ErrControlTimeout", err)
	}
	if got := h.acceptor.responses.size(); got != 0 {
		t.Errorf("response table kept %d waiters after the timeout", got)
	}
}

func TestQueryRecords_SendFailureIsUnreachable(t *testing.T) {
	deviceID := mustParse(t, relayDeviceID)
	h := newDeviceHarness(t, deviceID, nil, nil)
	h.tr.failSend = errors.New("network down")

	_, err := h.acceptor.QueryRecords(context.Background(), deviceID, deviceID.String(),
		"127.0.0.1:15060", "ch-1", time.Time{}, time.Time{})
	if !errors.Is(err, port.ErrControlTargetUnreachable) {
		t.Fatalf("err = %v, want ErrControlTargetUnreachable", err)
	}
	if got := h.acceptor.responses.size(); got != 0 {
		t.Errorf("response table kept %d waiters after a failed send", got)
	}
}
