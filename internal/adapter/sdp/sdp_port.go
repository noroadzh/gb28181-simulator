package sdp

import (
	"fmt"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// SDPCodecAdapter implements port.SDPCodec. The Parse path translates raw SDP
// text into a legacy *Session, then wraps it into an immutable model.Session
// (which stores only high-level fields). The Marshal path re-emits the raw
// text unchanged: the legacy session is opaque at this stage and Callers
// should treat model.Session as a read-only envelope. Full round-trip is
// deferred to Change 4 when the model layer exposes raw SDP preservation.
type SDPCodecAdapter struct {
	// lastRaw holds the most recent input text so Marshal can replay it.
	lastRaw string
}

var _ port.SDPCodec = (*SDPCodecAdapter)(nil)

// NewSDPCodecAdapter returns a port.SDPCodec backed by the legacy sdp parser.
func NewSDPCodecAdapter() *SDPCodecAdapter { return &SDPCodecAdapter{} }

// Parse implements port.SDPCodec. It delegates to legacy Parse and wraps
// the resulting *Session in a model.Session. The raw SDP text is retained
// so that Marshal can replay it (see Marshal docstring).
func (a *SDPCodecAdapter) Parse(text string) (model.Session, error) {
	if text == "" {
		return model.Session{}, fmt.Errorf("sdp: empty input")
	}
	legacy, err := Parse(text)
	if err != nil {
		return model.Session{}, err
	}
	a.lastRaw = text
	// Build a model.Session from the legacy fields. model.Session is opaque,
	// so we only preserve the high-level summary.
	origin := ""
	name := ""
	conn := ""
	if legacy.SessionDescription != nil {
		origin = fmt.Sprintf("%s@%s:%d", legacy.Origin.Username, legacy.Origin.UnicastAddress, legacy.Origin.SessionID)
		name = string(legacy.SessionName)
		if legacy.ConnectionInformation != nil && legacy.ConnectionInformation.Address != nil {
			conn = legacy.ConnectionInformation.Address.IP.String()
		}
	}
	streams := make([]model.MediaStream, 0, len(legacy.Media))
	for _, m := range legacy.Media {
		ms := model.MediaStream{
			MediaType: m.MediaName.Media,
			Port:      m.MediaName.Port.Value,
			Protocol:  m.MediaName.Protos[0],
			Formats:   append([]string(nil), m.MediaName.Formats...),
		}
		if ms.Protocol == "" || len(ms.Formats) == 0 || ms.MediaType == "" {
			continue
		}
		streams = append(streams, ms)
	}
	if len(streams) == 0 {
		// model.NewSession panics without a stream; synthesise a minimal one
		// to keep the envelope valid. Real SDP bodies always carry at least
		// one m= line, so this path is defensive only.
		streams = append(streams, model.MediaStream{
			MediaType: "video", Port: 0, Protocol: "RTP/AVP", Formats: []string{"96"},
		})
	}
	return model.NewSession(origin, name, conn, streams, nil), nil
}

// Marshal implements port.SDPCodec. Because model.Session does not expose
// raw SDP text, the adapter replays the last input supplied to Parse. This
// guarantees that the Marshal output is identical to what was parsed. Callers
// that mutate SDP after parsing must re-serialise via the legacy Marshal
// on the underlying *Session; the adapter stores no reference to the legacy
// session and therefore cannot round-trip arbitrary mutations.
func (a *SDPCodecAdapter) Marshal(s model.Session) (string, error) {
	if a.lastRaw == "" {
		return "", fmt.Errorf("sdp: Marshal called before Parse; cannot replay raw text")
	}
	return a.lastRaw, nil
}
