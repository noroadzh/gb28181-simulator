package manscdp

import (
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// Golden on the playback control the platform sends: the command word, the
// channel, and the scale element only where it is meaningful.
func TestMarshalPlaybackControl_Golden(t *testing.T) {
	ctrl, err := model.NewPlaybackControl("34020000001310000001", "Play", "ch-1", "", "", 9)
	if err != nil {
		t.Fatalf("NewPlaybackControl: %v", err)
	}

	body, err := NewMANSCDPCodec().MarshalPlaybackControl(ctrl, "2")
	if err != nil {
		t.Fatalf("MarshalPlaybackControl: %v", err)
	}

	want := xmlHeader + `<Control>
  <CmdType>PlaybackControl</CmdType>
  <SN>9</SN>
  <DeviceID>34020000001310000001</DeviceID>
  <ChannelID>ch-1</ChannelID>
  <Cmd>Play</Cmd>
  <Scale>2</Scale>
</Control>` + "\n"
	if body != want {
		t.Fatalf("playback control mismatch:\n got: %q\nwant: %q", body, want)
	}
}

func TestMarshalPlaybackControl_RequiresDeviceAndCommand(t *testing.T) {
	ctrl, err := model.NewPlaybackControl("34020000001310000001", "Play", "ch-1", "", "", 9)
	if err != nil {
		t.Fatalf("NewPlaybackControl: %v", err)
	}
	ctrl = model.PlaybackControl{}
	if _, err := NewMANSCDPCodec().MarshalPlaybackControl(ctrl, ""); err == nil {
		t.Error("accepted a playback control without a device id")
	}
}
