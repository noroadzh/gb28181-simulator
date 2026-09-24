package model

import (
	"strings"
	"testing"
)

func TestNewNotify(t *testing.T) {
	cases := []struct {
		name     string
		cmdType  string
		deviceID string
		sn       uint32
		status   string
		wantErr  bool
	}{
		{"keepalive", CmdTypeKeepalive, "34020000011310000001", 7, "OK", false},
		{"catalog query", CmdTypeCatalog, "34020000002000000001", 9, "", false},
		{"unknown command is still a notify", "Alarm", "34020000011310000001", 1, "", false},
		{"no command type", "", "34020000011310000001", 1, "", true},
		{"no device id", CmdTypeKeepalive, "", 1, "", true},
		{"blank command type", "  ", "34020000011310000001", 1, "", true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			n, err := NewNotify(tc.cmdType, tc.deviceID, tc.sn, tc.status)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("NewNotify(%q, %q): got nil, want an error", tc.cmdType, tc.deviceID)
				}
				if n.HasNotify() {
					t.Errorf("a refused notify reports itself as built: %v", n)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewNotify: %v", err)
			}
			if n.CmdType() != tc.cmdType || n.DeviceID() != tc.deviceID ||
				n.SN() != tc.sn || n.Status() != tc.status {
				t.Errorf("notify = %+v, want %q/%q/%d/%q", n, tc.cmdType, tc.deviceID, tc.sn, tc.status)
			}
		})
	}
}

// The command type is what routes a notify, so the two commands a platform
// answers must be recognisable — and only those.
func TestNotify_CommandShape(t *testing.T) {
	keep, err := NewNotify(CmdTypeKeepalive, "34020000011310000001", 1, "OK")
	if err != nil {
		t.Fatalf("NewNotify: %v", err)
	}
	if !keep.IsKeepalive() || keep.IsCatalogQuery() {
		t.Errorf("a keepalive is not recognised as one: %v", keep)
	}
	query, err := NewNotify(CmdTypeCatalog, "34020000002000000001", 2, "")
	if err != nil {
		t.Fatalf("NewNotify: %v", err)
	}
	if !query.IsCatalogQuery() || query.IsKeepalive() {
		t.Errorf("a catalog query is not recognised as one: %v", query)
	}
	other, err := NewNotify("Alarm", "34020000011310000001", 3, "")
	if err != nil {
		t.Fatalf("NewNotify: %v", err)
	}
	if other.IsKeepalive() || other.IsCatalogQuery() {
		t.Errorf("an unrelated command was recognised: %v", other)
	}
}

// A catalog answer has to echo the query's SN; a notify without one must be
// recognisable as such instead of pretending to carry 0.
func TestNotify_HasSN(t *testing.T) {
	with, err := NewNotify(CmdTypeCatalog, "34020000002000000001", 5, "")
	if err != nil {
		t.Fatalf("NewNotify: %v", err)
	}
	if !with.HasSN() {
		t.Error("a notify carrying SN 5 reports none")
	}
	without, err := NewNotify(CmdTypeCatalog, "34020000002000000001", 0, "")
	if err != nil {
		t.Fatalf("NewNotify: %v", err)
	}
	if without.HasSN() {
		t.Error("a notify with no SN reports one")
	}
}

// Values that reach a log line must never carry a secret; the summary is
// built by String, so it is what guards that.
func TestNotify_StringIsLogSafe(t *testing.T) {
	n, err := NewNotify(CmdTypeKeepalive, "34020000011310000001", 1, "OK")
	if err != nil {
		t.Fatalf("NewNotify: %v", err)
	}
	summary := strings.ToLower(n.String())
	for _, secret := range []string{"password", "authorization", "secret"} {
		if strings.Contains(summary, secret) {
			t.Errorf("String() carries %q: %s", secret, n.String())
		}
	}
	if !strings.Contains(n.String(), "34020000011310000001") {
		t.Errorf("String() omits the device id: %s", n.String())
	}
}
