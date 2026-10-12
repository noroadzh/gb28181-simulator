package app

import (
	"context"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// TestChannelAddRemove_PlatformLarge exercises channel CRUD on a platform-large
// node via the NodeService API.  This covers spec task 4.1 for the platform-large
// variant: the channel store has no node-kind guard on Add/Remove, so a
// platform-large node can host channels exactly like a device node.
//
// Note: automatic ID generation (empty id → ${nodeID}_N) is handled by the HTTP
// handler layer (handleChannelAdd); the app-layer AddChannel requires a non-empty id.
func TestChannelAddRemove_PlatformLarge(t *testing.T) {
	svc, _, _ := registrationFixture(t, testPlatformLarge, nil)

	id := mustParse(t, testPlatformLarge)
	if _, err := svc.Create(context.Background(), largeProfile(t, testPlatformLarge, nil)); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// 4.1 Scenario A: add a channel with an explicit id.
	ch, err := svc.AddChannel(context.Background(), id, "entrance-1", "entrance-1", "", model.ChannelStatusOnline)
	if err != nil {
		t.Fatalf("AddChannel: %v", err)
	}
	if ch.ID() != "entrance-1" {
		t.Fatalf("channel id = %q, want %q", ch.ID(), "entrance-1")
	}
	if ch.Name() != "entrance-1" {
		t.Fatalf("channel name = %q, want %q", ch.Name(), "entrance-1")
	}

	// 4.1 Scenario B: add a second channel with a different id.
	ch2, err := svc.AddChannel(context.Background(), id, "lobby-1", "lobby-1", "", model.ChannelStatusOnline)
	if err != nil {
		t.Fatalf("AddChannel (second): %v", err)
	}
	if ch2.ID() == "" || ch2.ID() == "entrance-1" {
		t.Fatalf("second channel id = %q, must be distinct and non-empty", ch2.ID())
	}

	// 4.1 Scenario C: add a channel with a duplicate id → error.
	_, err = svc.AddChannel(context.Background(), id, "entrance-1", "duplicate", "", model.ChannelStatusOnline)
	if err == nil {
		t.Fatal("AddChannel (duplicate id): expected error, got nil")
	}

	// List returns both channels.
	channels := svc.ListChannels(context.Background(), id)
	if len(channels) != 2 {
		t.Fatalf("ListChannels = %d channels, want 2", len(channels))
	}

	// 4.1 Scenario D: remove a channel → success.
	if err := svc.RemoveChannel(context.Background(), id, "entrance-1"); err != nil {
		t.Fatalf("RemoveChannel: %v", err)
	}

	channels = svc.ListChannels(context.Background(), id)
	if len(channels) != 1 {
		t.Fatalf("after removal ListChannels = %d, want 1", len(channels))
	}

	// Remove non-existent → error.
	if err := svc.RemoveChannel(context.Background(), id, "does-not-exist"); err == nil {
		t.Fatal("RemoveChannel (missing): expected error, got nil")
	}
}

// TestChannelAdd_PlatformLargeIDVariants covers explicit id scenarios from
// spec task 4.1: short, long, and non-20-digit strings are all valid channel ids.
// Auto-generation (empty → ${nodeID}_N) lives in the HTTP handler, not here.
func TestChannelAdd_PlatformLargeIDVariants(t *testing.T) {
	svc, _, _ := registrationFixture(t, testPlatformLarge, nil)
	id := mustParse(t, testPlatformLarge)
	if _, err := svc.Create(context.Background(), largeProfile(t, testPlatformLarge, nil)); err != nil {
		t.Fatalf("Create: %v", err)
	}

	cases := []struct {
		name string
		id   string
	}{
		{"short 5-char id", "CH001"},
		{"20-digit GB/T id", "34020000001320000001"},
		{"long alphanum id", "entrance-cam-01-building-A"},
		{"non-20-digit id with underscore", "cam_building_A_1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch, err := svc.AddChannel(context.Background(), id, tc.id, tc.name, "", model.ChannelStatusOnline)
			if err != nil {
				t.Fatalf("AddChannel(id=%q): %v", tc.id, err)
			}
			if ch.ID() != tc.id {
				t.Fatalf("returned id = %q, want %q", ch.ID(), tc.id)
			}
			// Clean up.
			if err := svc.RemoveChannel(context.Background(), id, ch.ID()); err != nil {
				t.Fatalf("RemoveChannel: %v", err)
			}
		})
	}

	// App-layer rejection of empty id (auto-gen lives in HTTP layer).
	_, err := svc.AddChannel(context.Background(), id, "", "unnamed", "", model.ChannelStatusOnline)
	if err == nil {
		t.Fatal("AddChannel(id=\"\"): expected error at app layer, got nil")
	}
}
