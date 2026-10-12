package app

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// A platform-large with channels but no online downstreams must answer a
// Catalog query with SumNum equal to the number of channels it carries,
// not zero. Before this change the catalog body was the empty-list
// template: the channel aggregation in Catalog happened at a place that
// got bypassed when the device table was empty, so the upstream saw a
// silent aggregator. This test puts that aggregator in its quiet path.
func TestAcceptor_CatalogAnswersPlatformLargeChannelsWhenDevicesEmpty(t *testing.T) {
	h, reg := newMessageHarnessWithRegistry(t)
	channels := []model.Channel{
		mustChannel(t, "34020000001320000001", "entrance-1", "34020000001320000001", model.ChannelStatusOnline),
		mustChannel(t, "34020000001320000002", "entrance-2", "34020000001320000002", model.ChannelStatusOnline),
	}
	base, err := model.NewNodeProfile(h.nodeID.String(), "127.0.0.1:5060", "3402000000", "")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	next, _ := base.WithChannels(channels)
	if _, err := reg.MutateProfile(context.Background(), h.nodeID, func(np model.NodeProfile) (model.NodeProfile, error) {
		return next, nil
	}); err != nil {
		t.Fatalf("MutateProfile: %v", err)
	}
	// No downstream registered; the device table is empty. A Catalog
	// query from upstream must still see the channels.
	h.codec.setNotify(mustNotify(t, model.CmdTypeCatalog, "34020000002000000001", 5))
	resp := h.tr.deliver(t, messageRequest(t, "34020000011310000001", "<Query/>"))
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	rendered := h.codec.rendered()
	if len(rendered) != 1 {
		t.Fatalf("rendered %d catalogs, want 1", len(rendered))
	}
	if rendered[0].SumNum() != 2 {
		t.Fatalf("SumNum = %d, want 2 (no downstreams, only profile channels)", rendered[0].SumNum())
	}
	ids := make([]string, 0, len(rendered[0].Items()))
	for _, it := range rendered[0].Items() {
		ids = append(ids, it.DeviceID())
	}
	sort.Strings(ids)
	want := []string{"34020000001320000001", "34020000001320000002"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("catalog ids = %v, want %v", ids, want)
	}
}

// Catalog items are emitted in ascending device-id order across the device
// table and the channel list. The aggregation order is part of the wire
// format: an upstream platform diffs the response against its previous
// view, and a non-deterministic order would mean each Catalog query looks
// like a fresh state even when nothing moved.
func TestAcceptor_CatalogAnswersPlatformLargeChannelsAscending(t *testing.T) {
	h, reg := newMessageHarnessWithRegistry(t)
	// Deliberately non-monotonic insertion order.
	channels := []model.Channel{
		mustChannel(t, "34020000001320000009", "high", "34020000001320000009", model.ChannelStatusOnline),
		mustChannel(t, "34020000001320000002", "low", "34020000001320000002", model.ChannelStatusOnline),
		mustChannel(t, "34020000001320000005", "mid", "34020000001320000005", model.ChannelStatusOnline),
	}
	base, err := model.NewNodeProfile(h.nodeID.String(), "127.0.0.1:5060", "3402000000", "")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	next, _ := base.WithChannels(channels)
	if _, err := reg.MutateProfile(context.Background(), h.nodeID, func(np model.NodeProfile) (model.NodeProfile, error) {
		return next, nil
	}); err != nil {
		t.Fatalf("MutateProfile: %v", err)
	}
	h.register(t, "34020000011310000004", "3600")
	h.codec.setNotify(mustNotify(t, model.CmdTypeCatalog, "34020000002000000001", 6))
	resp := h.tr.deliver(t, messageRequest(t, "34020000011310000001", "<Query/>"))
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	rendered := h.codec.rendered()
	if len(rendered) != 1 {
		t.Fatalf("rendered %d catalogs, want 1", len(rendered))
	}
	items := rendered[0].Items()
	if len(items) != 4 {
		t.Fatalf("items = %d, want 4", len(items))
	}
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.DeviceID())
	}
	sort.Strings(ids)
	want := []string{
		"34020000001320000002",
		"34020000001320000005",
		"34020000001320000009",
		"34020000011310000004",
	}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("catalog ids = %v, want %v", ids, want)
	}
}
