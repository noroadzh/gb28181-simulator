package manscdp

import (
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

const (
	keepaliveBody = `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<Notify>` + "\n" +
		`  <CmdType>Keepalive</CmdType>` + "\n" +
		`  <SN>7</SN>` + "\n" +
		`  <DeviceID>34020000011310000001</DeviceID>` + "\n" +
		`  <Status>OK</Status>` + "\n" +
		`</Notify>`

	catalogQueryBody = `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<Notify>` + "\n" +
		`  <CmdType>Catalog</CmdType>` + "\n" +
		`  <SN>42</SN>` + "\n" +
		`  <DeviceID>34020000002000000001</DeviceID>` + "\n" +
		`</Notify>`
)

// A platform reads what other people's devices wrote, so the happy path is
// the least interesting part: the decoder must also survive the rest.
func TestDecodeNotify(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantCmd string
		wantID  string
		wantSN  uint32
		wantErr bool
	}{
		{"keepalive", keepaliveBody, model.CmdTypeKeepalive, "34020000011310000001", 7, false},
		{"catalog query", catalogQueryBody, model.CmdTypeCatalog, "34020000002000000001", 42, false},
		{"without a declaration", `<Notify><CmdType>Keepalive</CmdType>` +
			`<DeviceID>34020000011310000001</DeviceID></Notify>`,
			model.CmdTypeKeepalive, "34020000011310000001", 0, false},
		{"unknown command still decodes", `<Notify><CmdType>Alarm</CmdType>` +
			`<DeviceID>34020000011310000001</DeviceID></Notify>`,
			"Alarm", "34020000011310000001", 0, false},
		{"empty body", "", "", "", 0, true},
		{"blank body", "   ", "", "", 0, true},
		{"not xml", "hello", "", "", 0, true},
		{"truncated xml", `<Notify><CmdType>Keepalive</CmdType>`, "", "", 0, true},
		{"no command type", `<Notify><DeviceID>34020000011310000001</DeviceID></Notify>`, "", "", 0, true},
		{"no device id", `<Notify><CmdType>Keepalive</CmdType></Notify>`, "", "", 0, true},
		{"wrong root element", `<Query><CmdType>Catalog</CmdType>` +
			`<DeviceID>34020000002000000001</DeviceID></Query>`, "", "", 0, true},
		{"sn is not a number", `<Notify><CmdType>Catalog</CmdType><SN>abc</SN>` +
			`<DeviceID>34020000002000000001</DeviceID></Notify>`, "", "", 0, true},
	}
	c := NewMANSCDPCodec()
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			n, err := c.DecodeNotify(tc.body)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("DecodeNotify(%q) = %v, want an error", tc.name, n)
				}
				if n.HasNotify() {
					t.Errorf("a refused notify reports itself as built: %v", n)
				}
				return
			}
			if err != nil {
				t.Fatalf("DecodeNotify: %v", err)
			}
			if n.CmdType() != tc.wantCmd || n.DeviceID() != tc.wantID || n.SN() != tc.wantSN {
				t.Errorf("notify = %s, want %s/%s/%d", n, tc.wantCmd, tc.wantID, tc.wantSN)
			}
		})
	}
}

// The command type is what routes the message, so decoding must hand back a
// notify the use case can dispatch on without re-parsing the XML.
func TestDecodeNotify_Routing(t *testing.T) {
	c := NewMANSCDPCodec()
	keep, err := c.DecodeNotify(keepaliveBody)
	if err != nil {
		t.Fatalf("DecodeNotify: %v", err)
	}
	if !keep.IsKeepalive() || keep.IsCatalogQuery() {
		t.Errorf("keepalive not recognised: %v", keep)
	}
	if keep.Status() != model.KeepaliveStatusOK {
		t.Errorf("status = %q, want OK", keep.Status())
	}
	query, err := c.DecodeNotify(catalogQueryBody)
	if err != nil {
		t.Fatalf("DecodeNotify: %v", err)
	}
	if !query.IsCatalogQuery() || query.IsKeepalive() {
		t.Errorf("catalog query not recognised: %v", query)
	}
	if !query.HasSN() {
		t.Error("a query that carried an SN reports none")
	}
}

// A device that sends extra elements — vendors do — must still be
// understood; unknown elements are ignored, not rejected.
func TestDecodeNotify_IgnoresUnknownElements(t *testing.T) {
	n, err := NewMANSCDPCodec().DecodeNotify(`<?xml version="1.0" encoding="UTF-8"?>` +
		`<Notify><CmdType>Keepalive</CmdType><SN>3</SN>` +
		`<DeviceID>34020000011310000001</DeviceID><Status>OK</Status>` +
		`<Info><Alarm>0</Alarm></Info><VendorOnly>x</VendorOnly></Notify>`)
	if err != nil {
		t.Fatalf("DecodeNotify: %v", err)
	}
	if !n.IsKeepalive() || n.DeviceID() != "34020000011310000001" || n.SN() != 3 {
		t.Errorf("notify = %s, want the known fields intact", n)
	}
}

// Decoding is called on every inbound MESSAGE: a panic would take the
// platform down on a single malformed packet. Every branch above is
// therefore also a no-panic check, and this one fuzzes the shapes.
func TestDecodeNotify_NeverPanics(t *testing.T) {
	c := NewMANSCDPCodec()
	bodies := []string{
		"", "\x00", "<?xml", "<", "<>", "<Notify>", "</Notify>",
		`<?xml version="1.0"?><Notify><SN>-1</SN><DeviceID>a</DeviceID><CmdType>Keepalive</CmdType></Notify>`,
		`<?xml version="1.0"?><Notify><SN>99999999999999999999</SN>` +
			`<DeviceID>a</DeviceID><CmdType>Keepalive</CmdType></Notify>`,
	}
	for _, b := range bodies {
		if _, err := c.DecodeNotify(b); err == nil {
			t.Logf("decoded %q: no error (fine, it may be valid)", b)
		}
	}
}
