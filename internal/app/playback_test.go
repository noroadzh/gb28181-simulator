package app

import (
	"context"
	"strings"
	"testing"
	"time"
)

func playbackBodies(t *testing.T, tr *scriptedTransport) []string {
	t.Helper()
	sent := tr.messages()
	out := make([]string, 0, len(sent))
	for _, m := range sent {
		out = append(out, m.msg.Body())
	}
	return out
}

// One session per channel: the second play must tear the first down, not
// leave a port and a dialog behind.
func TestPlayback_DuplicatePlayTearsDownOldSession(t *testing.T) {
	svc, tr, id := ptzHarness(t, "34020000001310000001", "127.0.0.1:15061")
	ctx := context.Background()

	if err := svc.Playback(ctx, id, "ch-1", "play", 1); err != nil {
		t.Fatalf("first play: %v", err)
	}
	if err := svc.Playback(ctx, id, "ch-1", "play", 1); err != nil {
		t.Fatalf("second play: %v", err)
	}

	bodies := playbackBodies(t, tr)
	if len(bodies) != 3 {
		t.Fatalf("sent %d controls, want Play, Teardown, Play:\n%v", len(bodies), bodies)
	}
	if !strings.Contains(bodies[0], "<Cmd>Play</Cmd>") {
		t.Errorf("first control = %q, want Play", bodies[0])
	}
	if !strings.Contains(bodies[1], "<Cmd>Teardown</Cmd>") {
		t.Errorf("second control = %q, want the old session's Teardown", bodies[1])
	}
	if !strings.Contains(bodies[2], "<Cmd>Play</Cmd>") {
		t.Errorf("third control = %q, want the new Play", bodies[2])
	}
}

func TestPlayback_TeardownClearsSession(t *testing.T) {
	svc, tr, id := ptzHarness(t, "34020000001310000001", "127.0.0.1:15061")
	ctx := context.Background()

	if err := svc.Playback(ctx, id, "ch-1", "play", 1); err != nil {
		t.Fatalf("play: %v", err)
	}
	if svc.lookupPlayback(id.String()+"|ch-1") == nil {
		t.Fatal("play did not record a session")
	}
	if err := svc.Playback(ctx, id, "ch-1", "teardown", 0); err != nil {
		t.Fatalf("teardown: %v", err)
	}
	if sess := svc.lookupPlayback(id.String() + "|ch-1"); sess != nil {
		t.Error("teardown left the session registered")
	}

	bodies := playbackBodies(t, tr)
	if len(bodies) != 2 || !strings.Contains(bodies[1], "<Cmd>Teardown</Cmd>") {
		t.Errorf("controls = %v, want Play then Teardown", bodies)
	}
}

func TestPlayback_IdleSessionReclaimsItself(t *testing.T) {
	svc, tr, id := ptzHarness(t, "34020000001310000001", "127.0.0.1:15061")
	svc.playbackIdle = 20 * time.Millisecond

	if err := svc.Playback(context.Background(), id, "ch-1", "play", 1); err != nil {
		t.Fatalf("play: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		bodies := playbackBodies(t, tr)
		if len(bodies) >= 2 && strings.Contains(bodies[1], "<Cmd>Teardown</Cmd>") {
			if svc.lookupPlayback(id.String()+"|ch-1") != nil {
				t.Error("the reclaimed session is still registered")
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the idle session never tore itself down")
}

func TestPlayback_PauseWithoutSessionIsUnsupported(t *testing.T) {
	svc, _, id := ptzHarness(t, "34020000001310000001", "127.0.0.1:15061")
	if err := svc.Playback(context.Background(), id, "ch-1", "pause", 0); err == nil {
		t.Fatal("pause without a session succeeded")
	}
}
