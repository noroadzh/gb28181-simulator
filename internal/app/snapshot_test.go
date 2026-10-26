package app

import (
	"context"
	"errors"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// fakeRenderer stands in for the media adapter so these tests can check what
// the service does with a rendered frame and with a refused one.
type fakeRenderer struct {
	data []byte
	mime string
	err  error
}

func (f fakeRenderer) RenderFrame(model.MediaConfig) ([]byte, string, error) {
	return f.data, f.mime, f.err
}

func deviceWithChannelMedia(t *testing.T, svc *NodeService, channelID string, cfg model.MediaConfig) model.NodeID {
	t.Helper()
	prof := testProfile(t, "34020000001310000001", "127.0.0.1:15061")
	prof.SetChannelMediaConfig(channelID, cfg)
	if _, err := svc.Create(context.Background(), prof); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return mustParse(t, "34020000001310000001")
}

// No source at all is 503, which is a different answer from "this source
// cannot be captured".
func TestSnapshot_NoMediaIsItsOwnAnswer(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	if _, err := svc.Create(context.Background(),
		testProfile(t, "34020000001310000001", "127.0.0.1:15061")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	id := mustParse(t, "34020000001310000001")

	_, _, err := svc.Snapshot(context.Background(), id, "ch-1")
	if !errors.Is(err, port.ErrSnapshotNoMedia) {
		t.Fatalf("err = %v, want ErrSnapshotNoMedia", err)
	}
}

func TestSnapshot_DelegatesToTheRenderer(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	id := deviceWithChannelMedia(t, svc, "ch-1", model.MediaConfig{Kind: model.SourceKindSynthetic})
	if _, err := svc.WithFrameRenderer(fakeRenderer{
		data: []byte{0xFF, 0xD8, 0xFF, 0x00}, mime: "image/jpeg",
	}); err != nil {
		t.Fatalf("WithFrameRenderer: %v", err)
	}

	data, mime, err := svc.Snapshot(context.Background(), id, "ch-1")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(data) != 4 || mime != "image/jpeg" {
		t.Errorf("got (% X, %q), want the renderer's bytes and media type", data, mime)
	}
}

func TestSnapshot_UndecodableSourceIsUnsupported(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	id := deviceWithChannelMedia(t, svc, "ch-1", model.MediaConfig{Kind: model.SourceKindFile, Path: "clip.mp4"})
	if _, err := svc.WithFrameRenderer(fakeRenderer{err: port.ErrFrameRenderUnsupported}); err != nil {
		t.Fatalf("WithFrameRenderer: %v", err)
	}

	_, _, err := svc.Snapshot(context.Background(), id, "ch-1")
	var unsupported port.ErrSnapshotUnsupported
	if !errors.As(err, &unsupported) {
		t.Fatalf("err = %v, want ErrSnapshotUnsupported", err)
	}
}

func TestSnapshot_NonDeviceIsUnsupported(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	if _, err := svc.Create(context.Background(),
		testProfile(t, "34020000002000000001", "127.0.0.1:15062")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	id := mustParse(t, "34020000002000000001")

	if _, _, err := svc.Snapshot(context.Background(), id, "ch-1"); err == nil {
		t.Fatal("a platform node produced a snapshot")
	}
}
