package manscdp

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// notifyEnvelope is the wire shape of a notify a platform receives. Only the
// four elements this simulator acts on are modelled; everything else a peer
// may add is ignored rather than treated as an error, because GB/T 28181
// devices in the wild do add elements.
type notifyEnvelope struct {
	XMLName  xml.Name `xml:"Notify"`
	CmdType  string   `xml:"CmdType"`
	SN       string   `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
	Status   string   `xml:"Status"`
}

// DecodeNotify parses a MANSCDP notify body into the command it carries.
//
// Malformed input is an error, never a panic: a platform reads what other
// people's devices wrote. An XML declaration is optional on the way in
// (it is mandatory on the way out), and a body that is not a <Notify> is
// reported as such so the caller can log something useful.
func (c *MANSCDPCodecAdapter) DecodeNotify(body string) (model.Notify, error) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return model.Notify{}, fmt.Errorf("manscdp: empty notify body")
	}
	// encoding/xml skips a leading declaration on its own, but a stray
	// one after whitespace confuses it, so it is stripped explicitly.
	trimmed = strings.TrimPrefix(trimmed, xmlHeader)
	trimmed = strings.TrimSpace(trimmed)

	var env notifyEnvelope
	if err := xml.Unmarshal([]byte(trimmed), &env); err != nil {
		return model.Notify{}, fmt.Errorf("manscdp: unmarshal notify: %w", err)
	}
	if env.XMLName.Local != "" && env.XMLName.Local != "Notify" {
		return model.Notify{}, fmt.Errorf("manscdp: notify body is a %q, want a Notify", env.XMLName.Local)
	}
	sn, err := parseSN(env.SN)
	if err != nil {
		return model.Notify{}, err
	}
	return model.NewNotify(strings.TrimSpace(env.CmdType), strings.TrimSpace(env.DeviceID), sn,
		strings.TrimSpace(env.Status))
}

// parseSN reads the sequence number. An absent or unparseable SN is not a
// reason to drop the whole notify: a keepalive still names its sender, and
// only a catalog answer truly needs the number back.
func parseSN(raw string) (uint32, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("manscdp: notify sn: %w", err)
	}
	return uint32(n), nil
}
