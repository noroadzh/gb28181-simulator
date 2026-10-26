package manscdp

import (
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// Golden on the query the platform sends: field order, the string SN, the
// time window, and the omitted optional elements.
func TestMarshalRecordInfoQuery_Golden(t *testing.T) {
	query := model.NewRecordInfoQuery("34020000001310000001", 7,
		"2026-10-01T00:00:00", "2026-10-08T00:00:00")

	body, err := NewMANSCDPCodec().MarshalRecordInfoQuery(query, 7)
	if err != nil {
		t.Fatalf("MarshalRecordInfoQuery: %v", err)
	}

	want := xmlHeader + `<Query>
  <CmdType>RecordInfo</CmdType>
  <SN>7</SN>
  <DeviceID>34020000001310000001</DeviceID>
  <StartTime>2026-10-01T00:00:00</StartTime>
  <EndTime>2026-10-08T00:00:00</EndTime>
</Query>` + "\n"
	if body != want {
		t.Fatalf("query body mismatch:\n got: %q\nwant: %q", body, want)
	}

	// The device side must read back exactly what the platform wrote.
	decoded, err := NewMANSCDPCodec().DecodeRecordInfoQuery(body)
	if err != nil {
		t.Fatalf("DecodeRecordInfoQuery: %v", err)
	}
	if decoded.DeviceID != "34020000001310000001" || decoded.SN != 7 {
		t.Errorf("decoded = %+v, want device 34020000001310000001 sn 7", decoded)
	}
	if decoded.StartTime != "2026-10-01T00:00:00" || decoded.EndTime != "2026-10-08T00:00:00" {
		t.Errorf("decoded window = (%s,%s)", decoded.StartTime, decoded.EndTime)
	}
}

func TestMarshalRecordInfoQuery_RequiresDeviceAndSN(t *testing.T) {
	if _, err := NewMANSCDPCodec().MarshalRecordInfoQuery(model.RecordInfoQuery{}, 1); err == nil {
		t.Error("accepted a query without a device id")
	}
	if _, err := NewMANSCDPCodec().MarshalRecordInfoQuery(
		model.NewRecordInfoQuery("34020000001310000001", 0, "", ""), 0); err == nil {
		t.Error("accepted a query with sn 0")
	}
}

// An empty answer is a valid answer: no recordings, not an error.
func TestDecodeRecordInfoResponse_EmptyList(t *testing.T) {
	resp, err := NewMANSCDPCodec().DecodeRecordInfoResponse(xmlHeader + `<Response>
  <CmdType>RecordInfo</CmdType>
  <SN>7</SN>
  <DeviceID>34020000001310000001</DeviceID>
  <SumNum>0</SumNum>
</Response>`)
	if err != nil {
		t.Fatalf("DecodeRecordInfoResponse: %v", err)
	}
	if resp.SumNum != 0 || len(resp.Items) != 0 {
		t.Errorf("resp = %+v, want an empty list", resp)
	}
}
