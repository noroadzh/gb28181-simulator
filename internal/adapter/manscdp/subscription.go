package manscdp

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// subscribeEnvelope is the wire shape of a SUBSCRIBE request body.
type subscribeEnvelope struct {
	XMLName   xml.Name `xml:"Subscribe"`
	CmdType   string   `xml:"CmdType"`
	SN        string   `xml:"SN"`
	DeviceID  string   `xml:"DeviceID"`
	Expires   string   `xml:"Expires"`
	EventID   string   `xml:"EventID"`
	EventType string   `xml:"EventType"`
}

// subscribeResult is the parsed form of a SUBSCRIBE.
type subscribeResult struct {
	CmdType   string
	SN        uint32
	DeviceID  string
	Expires   uint32
	EventType string
}

// DecodeSubscribe parses a MANSCDP Subscribe body.
func (c *MANSCDPCodecAdapter) DecodeSubscribe(raw []byte) (subscribeResult, error) {
	var env subscribeEnvelope
	dec := xml.NewDecoder(strings.NewReader(string(raw)))
	if err := dec.Decode(&env); err != nil {
		// A body that is not XML is still an error: the caller should
		// log and ignore, but the platform must know it was unreadable.
		return subscribeResult{}, fmt.Errorf("manscdp: subscribe body: %w", err)
	}
	sn, _ := parseSN(env.SN)
	expires, err := parseExpires(env.Expires)
	if err != nil {
		return subscribeResult{}, err
	}
	if env.EventType == "" {
		env.EventType = "catalog"
	}
	return subscribeResult{
		CmdType:   strings.TrimSpace(env.CmdType),
		SN:        sn,
		DeviceID:  strings.TrimSpace(env.DeviceID),
		Expires:   expires,
		EventType: strings.TrimSpace(env.EventType),
	}, nil
}

// parseExpires reads the Expires value. An absent or zero Expires is not
// an error; it defaults to 3600 seconds per GB/T 28181.
func parseExpires(raw string) (uint32, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 3600, nil
	}
	n, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("manscdp: subscribe expires: %w", err)
	}
	return uint32(n), nil
}

// MarshalCatalogNotify renders a catalog NOTIFY body using the same
// shape as a catalog response, but wrapped in a Notify envelope.
func (c *MANSCDPCodecAdapter) MarshalCatalogNotify(catalog model.Catalog) (string, error) {
	body, err := c.MarshalCatalog(catalog)
	if err != nil {
		return "", err
	}
	// Wrap the <Response>...</Response> into a <Notify>...</Notify>.
	body = strings.TrimSuffix(body, "\n")
	body = strings.Replace(body, "<Response>", "<Notify>", 1)
	body = strings.Replace(body, "</Response>", "</Notify>", 1)
	return body, nil
}

// MarshalAlarmNotify renders an Alarm notify body, declaration included
// and terminated by a newline. The result is placed verbatim as a NOTIFY
// message body.
func (c *MANSCDPCodecAdapter) MarshalAlarmNotify(n model.AlarmNotify) (string, error) {
	return (*KeepaliveCodecAdapter)(c).MarshalAlarmNotify(n)
}

// xmlEscape replaces the five characters that must be escaped in XML.
func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}
