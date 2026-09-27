package manscdp

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// mediaStatusEnvelope is the wire shape of a MediaStatus notify.
type mediaStatusEnvelope struct {
	XMLName    xml.Name `xml:"Notify"`
	CmdType    string   `xml:"CmdType"`
	SN         string   `xml:"SN"`
	DeviceID   string   `xml:"DeviceID"`
	ChannelID  string   `xml:"ChannelID"`
	Record     string   `xml:"Record"`
	VideoParam string   `xml:"Video"`
}

// DecodeMediaStatus parses a MANSCDP MediaStatus notify.
func (c *MANSCDPCodecAdapter) DecodeMediaStatus(body string) (model.MediaStatus, error) {
	var env mediaStatusEnvelope
	trimmed := strings.TrimSpace(body)
	trimmed = strings.TrimPrefix(trimmed, xmlHeader)
	trimmed = strings.TrimSpace(trimmed)
	if err := xml.Unmarshal([]byte(trimmed), &env); err != nil {
		return model.MediaStatus{}, fmt.Errorf("manscdp: media status: %w", err)
	}
	if strings.TrimSpace(env.CmdType) != model.CmdTypeMediaStatus {
		return model.MediaStatus{}, fmt.Errorf("manscdp: not a MediaStatus notify (cmd=%s)", env.CmdType)
	}
	ms := model.MediaStatus{
		DeviceID:  strings.TrimSpace(env.DeviceID),
		ChannelID: strings.TrimSpace(env.ChannelID),
	}
	if ms.DeviceID == "" {
		return model.MediaStatus{}, fmt.Errorf("manscdp: MediaStatus without DeviceID")
	}
	ms.RecordStatus = parseRecordStatus(strings.TrimSpace(env.Record))
	// VideoParam is a nested element; parse it separately if present.
	if env.VideoParam != "" {
		ms.Video = parseVideoParam(strings.TrimSpace(env.VideoParam))
	}
	return ms, nil
}

func parseRecordStatus(s string) model.RecordStatus {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "recording":
		return model.RecordStatusRecording
	case "2", "playback":
		return model.RecordStatusPlayback
	default:
		return model.RecordStatusIdle
	}
}

func parseVideoParam(raw string) *model.VideoParam {
	// Video is a comma-separated list: width,height,bitrate,fps,codec
	parts := strings.Split(raw, ",")
	if len(parts) < 5 {
		return nil
	}
	vp := &model.VideoParam{}
	fmt.Sscanf(parts[0], "%d", &vp.Width)
	fmt.Sscanf(parts[1], "%d", &vp.Height)
	fmt.Sscanf(parts[2], "%d", &vp.Bitrate)
	fmt.Sscanf(parts[3], "%d", &vp.FrameRate)
	vp.Codec = strings.TrimSpace(parts[4])
	return vp
}

// MarshalMediaStatus renders a MediaStatus notify body.
func (c *MANSCDPCodecAdapter) MarshalMediaStatus(ms model.MediaStatus) (string, error) {
	var b strings.Builder
	b.WriteString(xmlHeader)
	b.WriteString("<Notify>\r\n")
	b.WriteString("  <CmdType>MediaStatus</CmdType>\r\n")
	b.WriteString("  <SN>1</SN>\r\n")
	fmt.Fprintf(&b, "  <DeviceID>%s</DeviceID>\r\n", xmlEscape(ms.DeviceID))
	fmt.Fprintf(&b, "  <ChannelID>%s</ChannelID>\r\n", xmlEscape(ms.ChannelID))
	if ms.Video != nil {
		b.WriteString(fmt.Sprintf("  <Video>%d,%d,%d,%d,%s</Video>\r\n",
			ms.Video.Width, ms.Video.Height, ms.Video.Bitrate, ms.Video.FrameRate, ms.Video.Codec))
	}
	fmt.Fprintf(&b, "  <Record>%s</Record>\r\n", ms.RecordStatus.String())
	if ms.Position != nil {
		fmt.Fprintf(&b, "  <Longitude>%v</Longitude>\r\n", ms.Position.Longitude())
		fmt.Fprintf(&b, "  <Latitude>%v</Latitude>\r\n", ms.Position.Latitude())
		fmt.Fprintf(&b, "  <Speed>%v</Speed>\r\n", ms.Position.Speed())
	}
	b.WriteString("</Notify>\r\n")
	return b.String(), nil
}
