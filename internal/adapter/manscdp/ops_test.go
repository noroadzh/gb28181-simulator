package manscdp

import (
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// Task 1.1: DeviceStatus answer bytes are the contract a real platform reads.
func TestMarshalDeviceStatusResponse_Golden(t *testing.T) {
	got, err := NewMANSCDPCodec().MarshalDeviceStatusResponse(model.DeviceStatusResponse{
		SN: 1, DeviceID: "34020000001310000001", Result: "OK", Online: "ONLINE", Status: "OK",
	})
	if err != nil {
		t.Fatalf("MarshalDeviceStatusResponse: %v", err)
	}
	const want = `<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<Response>
  <CmdType>DeviceStatus</CmdType>
  <SN>1</SN>
  <DeviceID>34020000001310000001</DeviceID>
  <Result>OK</Result>
  <Online>ONLINE</Online>
  <Status>OK</Status>
</Response>
`
	if got != want {
		t.Errorf("status answer:\n got %q\nwant %q", got, want)
	}
}

func TestDecodeDeviceStatusQuery(t *testing.T) {
	body := `<?xml version="1.0"?><Query><CmdType>DeviceStatus</CmdType><SN>7</SN><DeviceID>34020000001310000001</DeviceID></Query>`
	q, err := NewMANSCDPCodec().DecodeDeviceStatusQuery(body)
	if err != nil {
		t.Fatalf("DecodeDeviceStatusQuery: %v", err)
	}
	if q.SN != 7 || q.DeviceID != "34020000001310000001" {
		t.Errorf("query = %+v", q)
	}
}

// Task 1.2: Broadcast ack bytes.
func TestMarshalBroadcastAck_Golden(t *testing.T) {
	got, err := NewMANSCDPCodec().MarshalBroadcastAck(model.BroadcastAck{
		SN: 2, TargetID: "34020000001310000001", Result: "OK",
	})
	if err != nil {
		t.Fatalf("MarshalBroadcastAck: %v", err)
	}
	const want = `<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<Response>
  <CmdType>Broadcast</CmdType>
  <SN>2</SN>
  <TargetID>34020000001310000001</TargetID>
  <Result>OK</Result>
</Response>
`
	if got != want {
		t.Errorf("broadcast ack:\n got %q\nwant %q", got, want)
	}
}

func TestDecodeBroadcast(t *testing.T) {
	body := `<Notify><CmdType>Broadcast</CmdType><SN>3</SN><SourceID>34020000002000000001</SourceID><TargetID>34020000001310000001</TargetID></Notify>`
	cmd, err := NewMANSCDPCodec().DecodeBroadcast(body)
	if err != nil {
		t.Fatalf("DecodeBroadcast: %v", err)
	}
	if cmd.SourceID != "34020000002000000001" || cmd.TargetID != "34020000001310000001" {
		t.Errorf("broadcast = %+v", cmd)
	}
}

// Task 1.3: alarm query answer.
func TestMarshalAlarmQueryResponse_Golden(t *testing.T) {
	got, err := NewMANSCDPCodec().MarshalAlarmQueryResponse(model.AlarmQueryResponse{
		SN: 5, DeviceID: "34020000001310000001",
		Items: []model.AlarmRecord{{DeviceID: "34020000001310000001",
			AlarmTime: "2026-10-30T10:00:00", Priority: 1, Method: 5}},
	})
	if err != nil {
		t.Fatalf("MarshalAlarmQueryResponse: %v", err)
	}
	for _, want := range []string{
		"<CmdType>Alarm</CmdType>", "<SumNum>1</SumNum>",
		"<AlarmPriority>1</AlarmPriority>", "<AlarmMethod>5</AlarmMethod>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("alarm answer is missing %q:\n%s", want, got)
		}
	}
}

func TestDecodeAlarmQuery(t *testing.T) {
	body := `<Query><CmdType>Alarm</CmdType><SN>6</SN><DeviceID>34020000001310000001</DeviceID><StartAlarmPriority>1</StartAlarmPriority><EndAlarmPriority>4</EndAlarmPriority><StartTime>20261030T000000</StartTime><EndTime>20261031T000000</EndTime></Query>`
	q, err := NewMANSCDPCodec().DecodeAlarmQuery(body)
	if err != nil {
		t.Fatalf("DecodeAlarmQuery: %v", err)
	}
	if q.StartPriority != 1 || q.EndPriority != 4 || q.StartTime != "20261030T000000" {
		t.Errorf("query = %+v", q)
	}
}

// Task 1.4: config query, write and answer.
func TestMarshalConfigResponse_Golden(t *testing.T) {
	got, err := NewMANSCDPCodec().MarshalConfigResponse(model.ConfigResponse{
		SN: 3, DeviceID: "34020000001310000001", ConfigType: "BasicParam", Result: "OK",
		Basic: model.BasicConfig{Name: "cam", Manufacturer: "acme", ChannelCount: 2, HeartbeatInterval: 60},
	})
	if err != nil {
		t.Fatalf("MarshalConfigResponse: %v", err)
	}
	for _, want := range []string{
		"<CmdType>ConfigDownload</CmdType>", "<ConfigType>BasicParam</ConfigType>",
		"<Name>cam</Name>", "<ChannelNum>2</ChannelNum>", "<HeartBeatInterval>60</HeartBeatInterval>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("config answer is missing %q:\n%s", want, got)
		}
	}
}

func TestDecodeConfigSet(t *testing.T) {
	body := `<Control><CmdType>ConfigDownload</CmdType><SN>8</SN><DeviceID>34020000001310000001</DeviceID><ConfigType>BasicParam</ConfigType><BasicParam><HeartBeatInterval>30</HeartBeatInterval></BasicParam></Control>`
	set, err := NewMANSCDPCodec().DecodeConfigSet(body)
	if err != nil {
		t.Fatalf("DecodeConfigSet: %v", err)
	}
	if set.ConfigType != "BasicParam" || set.Basic.HeartbeatInterval != 30 {
		t.Errorf("set = %+v", set)
	}
}

// Task 1.5: upgrade command and ack.
func TestDecodeUpgrade(t *testing.T) {
	body := `<Control><CmdType>DeviceUpgrade</CmdType><SN>9</SN><DeviceID>34020000001310000001</DeviceID><Firmware>2.0.0</Firmware></Control>`
	cmd, err := NewMANSCDPCodec().DecodeUpgrade(body)
	if err != nil {
		t.Fatalf("DecodeUpgrade: %v", err)
	}
	if cmd.Firmware != "2.0.0" {
		t.Errorf("upgrade = %+v", cmd)
	}
}

func TestMarshalOpsAck_Golden(t *testing.T) {
	got, err := NewMANSCDPCodec().MarshalOpsAck(model.OpsAck{
		SN: 4, DeviceID: "34020000001310000001", Result: "OK",
	}, model.CmdTypeDeviceUpgrade)
	if err != nil {
		t.Fatalf("MarshalOpsAck: %v", err)
	}
	const want = `<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<Response>
  <CmdType>DeviceUpgrade</CmdType>
  <SN>4</SN>
  <DeviceID>34020000001310000001</DeviceID>
  <Result>OK</Result>
</Response>
`
	if got != want {
		t.Errorf("ops ack:\n got %q\nwant %q", got, want)
	}
}

// Task 1.6: the Download SDP keeps the playback shape and only changes s=.
func TestPlaybackSDP_DownloadSession(t *testing.T) {
	start := time.Date(2026, 10, 30, 10, 0, 0, 0, time.Local)
	body, err := model.PlaybackSDPBytes(model.PlaybackSDP{
		DeviceID: "ch-1", SN: "9", ConnIP: "127.0.0.1", RTPPort: 40000,
		Start: start, End: start.Add(time.Minute), Session: "Download",
	})
	if err != nil {
		t.Fatalf("PlaybackSDPBytes: %v", err)
	}
	if !strings.Contains(body, "s=Download") {
		t.Errorf("download SDP is missing s=Download:\n%s", body)
	}
}

func TestNOTIFYEnvelopeIsRecorded(t *testing.T) {
	q, err := NewMANSCDPCodec().DecodeNotify(`<Query><CmdType>Alarm</CmdType><SN>1</SN><DeviceID>34020000001310000001</DeviceID></Query>`)
	if err != nil {
		t.Fatalf("DecodeNotify: %v", err)
	}
	if !q.IsQuery() || q.IsControl() {
		t.Errorf("envelope = %q, want query", q.CmdType())
	}
	n, err := NewMANSCDPCodec().DecodeNotify(`<Notify><CmdType>Alarm</CmdType><SN>1</SN><DeviceID>34020000001310000001</DeviceID></Notify>`)
	if err != nil {
		t.Fatalf("DecodeNotify: %v", err)
	}
	if n.IsQuery() {
		t.Error("a Notify envelope was read as a query")
	}
}
