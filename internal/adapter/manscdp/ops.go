package manscdp

// Codec for the operations commands added by device-control-coverage:
// DeviceStatus, Broadcast, alarm query, ConfigDownload and upgrade.

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"github.com/noroadzh/gb28181-simulator/internal/domain/model"
)

// --- DeviceStatus ---------------------------------------------------------

type deviceStatusQueryEnvelope struct {
	XMLName  xml.Name `xml:"Query"`
	CmdType  string   `xml:"CmdType"`
	SN       string   `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
}

type deviceStatusResponseEnvelope struct {
	XMLName    xml.Name `xml:"Response"`
	CmdType    string   `xml:"CmdType"`
	SN         uint32   `xml:"SN"`
	DeviceID   string   `xml:"DeviceID"`
	Result     string   `xml:"Result"`
	Online     string   `xml:"Online,omitempty"`
	Status     string   `xml:"Status,omitempty"`
	DeviceTime string   `xml:"DeviceTime,omitempty"`
}

func (c *MANSCDPCodecAdapter) DecodeDeviceStatusQuery(body string) (model.DeviceStatusQuery, error) {
	var env deviceStatusQueryEnvelope
	if err := unmarshalBody(body, &env); err != nil {
		return model.DeviceStatusQuery{}, fmt.Errorf("manscdp: unmarshal device status query: %w", err)
	}
	sn, err := parseSN(env.SN)
	if err != nil {
		return model.DeviceStatusQuery{}, err
	}
	if err := model.ValidateDeviceID(env.DeviceID); err != nil {
		return model.DeviceStatusQuery{}, err
	}
	return model.DeviceStatusQuery{SN: sn, DeviceID: strings.TrimSpace(env.DeviceID)}, nil
}

func (c *MANSCDPCodecAdapter) MarshalDeviceStatusResponse(resp model.DeviceStatusResponse) (string, error) {
	if err := model.ValidateDeviceID(resp.DeviceID); err != nil {
		return "", err
	}
	result := resp.Result
	if result == "" {
		result = model.ResultOK
	}
	return marshalEnvelope(deviceStatusResponseEnvelope{
		CmdType: model.CmdTypeDeviceStatus, SN: resp.SN, DeviceID: resp.DeviceID,
		Result: result, Online: resp.Online, Status: resp.Status, DeviceTime: resp.DeviceTime,
	})
}

// --- Broadcast ------------------------------------------------------------

type broadcastEnvelope struct {
	XMLName  xml.Name `xml:"Notify"`
	CmdType  string   `xml:"CmdType"`
	SN       string   `xml:"SN"`
	SourceID string   `xml:"SourceID"`
	TargetID string   `xml:"TargetID"`
}

type broadcastAckEnvelope struct {
	XMLName  xml.Name `xml:"Response"`
	CmdType  string   `xml:"CmdType"`
	SN       uint32   `xml:"SN"`
	TargetID string   `xml:"TargetID"`
	Result   string   `xml:"Result"`
}

func (c *MANSCDPCodecAdapter) DecodeBroadcast(body string) (model.BroadcastCommand, error) {
	var env broadcastEnvelope
	if err := unmarshalBody(body, &env); err != nil {
		return model.BroadcastCommand{}, fmt.Errorf("manscdp: unmarshal broadcast: %w", err)
	}
	sn, err := parseSN(env.SN)
	if err != nil {
		return model.BroadcastCommand{}, err
	}
	if err := model.ValidateDeviceID(env.TargetID); err != nil {
		return model.BroadcastCommand{}, err
	}
	return model.BroadcastCommand{SN: sn, SourceID: strings.TrimSpace(env.SourceID),
		TargetID: strings.TrimSpace(env.TargetID)}, nil
}

func (c *MANSCDPCodecAdapter) MarshalBroadcastAck(ack model.BroadcastAck) (string, error) {
	if err := model.ValidateDeviceID(ack.TargetID); err != nil {
		return "", err
	}
	result := ack.Result
	if result == "" {
		result = model.ResultOK
	}
	return marshalEnvelope(broadcastAckEnvelope{CmdType: model.CmdTypeBroadcast, SN: ack.SN,
		TargetID: ack.TargetID, Result: result})
}

// --- Alarm query ----------------------------------------------------------

type alarmQueryEnvelope struct {
	XMLName            xml.Name `xml:"Query"`
	CmdType            string   `xml:"CmdType"`
	SN                 string   `xml:"SN"`
	DeviceID           string   `xml:"DeviceID"`
	StartAlarmPriority int      `xml:"StartAlarmPriority"`
	EndAlarmPriority   int      `xml:"EndAlarmPriority"`
	StartTime          string   `xml:"StartTime"`
	EndTime            string   `xml:"EndTime"`
}

type alarmQueryResponseEnvelope struct {
	XMLName   xml.Name              `xml:"Response"`
	CmdType   string                `xml:"CmdType"`
	SN        uint32                `xml:"SN"`
	DeviceID  string                `xml:"DeviceID"`
	SumNum    int                   `xml:"SumNum"`
	AlarmList []alarmRecordEnvelope `xml:"AlarmList>Item"`
}

type alarmRecordEnvelope struct {
	DeviceID         string `xml:"DeviceID"`
	AlarmTime        string `xml:"AlarmTime"`
	AlarmPriority    int    `xml:"AlarmPriority"`
	AlarmMethod      int    `xml:"AlarmMethod"`
	AlarmDescription string `xml:"AlarmDescription,omitempty"`
}

func (c *MANSCDPCodecAdapter) DecodeAlarmQuery(body string) (model.AlarmQuery, error) {
	var env alarmQueryEnvelope
	if err := unmarshalBody(body, &env); err != nil {
		return model.AlarmQuery{}, fmt.Errorf("manscdp: unmarshal alarm query: %w", err)
	}
	sn, err := parseSN(env.SN)
	if err != nil {
		return model.AlarmQuery{}, err
	}
	return model.AlarmQuery{
		SN: sn, DeviceID: strings.TrimSpace(env.DeviceID),
		StartPriority: env.StartAlarmPriority, EndPriority: env.EndAlarmPriority,
		StartTime: strings.TrimSpace(env.StartTime), EndTime: strings.TrimSpace(env.EndTime),
	}, nil
}

func (c *MANSCDPCodecAdapter) MarshalAlarmQueryResponse(resp model.AlarmQueryResponse) (string, error) {
	items := make([]alarmRecordEnvelope, 0, len(resp.Items))
	for _, it := range resp.Items {
		items = append(items, alarmRecordEnvelope{
			DeviceID: it.DeviceID, AlarmTime: it.AlarmTime,
			AlarmPriority: it.Priority, AlarmMethod: it.Method,
			AlarmDescription: it.Description,
		})
	}
	return marshalEnvelope(alarmQueryResponseEnvelope{
		CmdType: model.CmdTypeAlarm, SN: resp.SN, DeviceID: resp.DeviceID,
		SumNum: len(items), AlarmList: items,
	})
}

// --- ConfigDownload -------------------------------------------------------

type configQueryEnvelope struct {
	XMLName    xml.Name `xml:"Query"`
	CmdType    string   `xml:"CmdType"`
	SN         string   `xml:"SN"`
	DeviceID   string   `xml:"DeviceID"`
	ConfigType string   `xml:"ConfigType"`
}

type basicParamEnvelope struct {
	Name              string `xml:"Name,omitempty"`
	Manufacturer      string `xml:"Manufacturer,omitempty"`
	Model             string `xml:"Model,omitempty"`
	Firmware          string `xml:"Firmware,omitempty"`
	ChannelNum        int    `xml:"ChannelNum,omitempty"`
	HeartBeatInterval int    `xml:"HeartBeatInterval,omitempty"`
	HeartBeatTimeout  int    `xml:"HeartBeatTimeout,omitempty"`
}

type configControlEnvelope struct {
	XMLName    xml.Name           `xml:"Control"`
	CmdType    string             `xml:"CmdType"`
	SN         string             `xml:"SN"`
	DeviceID   string             `xml:"DeviceID"`
	ConfigType string             `xml:"ConfigType"`
	BasicParam basicParamEnvelope `xml:"BasicParam"`
}

type configResponseEnvelope struct {
	XMLName    xml.Name           `xml:"Response"`
	CmdType    string             `xml:"CmdType"`
	SN         uint32             `xml:"SN"`
	DeviceID   string             `xml:"DeviceID"`
	ConfigType string             `xml:"ConfigType,omitempty"`
	Result     string             `xml:"Result"`
	BasicParam basicParamEnvelope `xml:"BasicParam"`
}

func (c *MANSCDPCodecAdapter) DecodeConfigQuery(body string) (model.ConfigQuery, error) {
	var env configQueryEnvelope
	if err := unmarshalBody(body, &env); err != nil {
		return model.ConfigQuery{}, fmt.Errorf("manscdp: unmarshal config query: %w", err)
	}
	sn, err := parseSN(env.SN)
	if err != nil {
		return model.ConfigQuery{}, err
	}
	return model.ConfigQuery{SN: sn, DeviceID: strings.TrimSpace(env.DeviceID),
		ConfigType: strings.TrimSpace(env.ConfigType)}, nil
}

func (c *MANSCDPCodecAdapter) DecodeConfigSet(body string) (model.ConfigSet, error) {
	var env configControlEnvelope
	if err := unmarshalBody(body, &env); err != nil {
		return model.ConfigSet{}, fmt.Errorf("manscdp: unmarshal config control: %w", err)
	}
	sn, err := parseSN(env.SN)
	if err != nil {
		return model.ConfigSet{}, err
	}
	return model.ConfigSet{
		SN: sn, DeviceID: strings.TrimSpace(env.DeviceID),
		ConfigType: strings.TrimSpace(env.ConfigType),
		Basic: model.BasicConfig{
			Name: env.BasicParam.Name, Manufacturer: env.BasicParam.Manufacturer,
			Model: env.BasicParam.Model, Firmware: env.BasicParam.Firmware,
			ChannelCount:      env.BasicParam.ChannelNum,
			HeartbeatInterval: env.BasicParam.HeartBeatInterval,
			HeartbeatTimeout:  env.BasicParam.HeartBeatTimeout,
		},
	}, nil
}

func (c *MANSCDPCodecAdapter) MarshalConfigResponse(resp model.ConfigResponse) (string, error) {
	result := resp.Result
	if result == "" {
		result = model.ResultOK
	}
	return marshalEnvelope(configResponseEnvelope{
		CmdType: model.CmdTypeConfigDownload, SN: resp.SN, DeviceID: resp.DeviceID,
		ConfigType: resp.ConfigType, Result: result,
		BasicParam: basicParamEnvelope{
			Name: resp.Basic.Name, Manufacturer: resp.Basic.Manufacturer,
			Model: resp.Basic.Model, Firmware: resp.Basic.Firmware,
			ChannelNum:        resp.Basic.ChannelCount,
			HeartBeatInterval: resp.Basic.HeartbeatInterval,
			HeartBeatTimeout:  resp.Basic.HeartbeatTimeout,
		},
	})
}

// --- Upgrade / generic ack ------------------------------------------------

type upgradeEnvelope struct {
	XMLName  xml.Name `xml:"Control"`
	CmdType  string   `xml:"CmdType"`
	SN       string   `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
	Firmware string   `xml:"Firmware"`
}

func (c *MANSCDPCodecAdapter) DecodeUpgrade(body string) (model.UpgradeCommand, error) {
	var env upgradeEnvelope
	if err := unmarshalBody(body, &env); err != nil {
		return model.UpgradeCommand{}, fmt.Errorf("manscdp: unmarshal upgrade: %w", err)
	}
	sn, err := parseSN(env.SN)
	if err != nil {
		return model.UpgradeCommand{}, err
	}
	return model.UpgradeCommand{SN: sn, DeviceID: strings.TrimSpace(env.DeviceID),
		Firmware: strings.TrimSpace(env.Firmware)}, nil
}

type opsAckEnvelope struct {
	XMLName  xml.Name `xml:"Response"`
	CmdType  string   `xml:"CmdType"`
	SN       uint32   `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
	Result   string   `xml:"Result"`
}

func (c *MANSCDPCodecAdapter) MarshalOpsAck(ack model.OpsAck, cmdType string) (string, error) {
	result := ack.Result
	if result == "" {
		result = model.ResultOK
	}
	return marshalEnvelope(opsAckEnvelope{CmdType: cmdType, SN: ack.SN, DeviceID: ack.DeviceID, Result: result})
}

// --- shared helpers -------------------------------------------------------

// unmarshalBody strips an optional XML declaration and decodes the body.
func unmarshalBody(body string, out any) error {
	trimmed := strings.TrimSpace(body)
	trimmed = strings.TrimPrefix(trimmed, xmlHeader)
	trimmed = strings.TrimSpace(trimmed)
	return xml.Unmarshal([]byte(trimmed), out)
}

// marshalEnvelope renders an envelope with the mandatory declaration.
func marshalEnvelope(env any) (string, error) {
	b, err := xml.MarshalIndent(env, "", "  ")
	if err != nil {
		return "", fmt.Errorf("manscdp: marshal envelope: %w", err)
	}
	return xmlHeader + string(b) + "\n", nil
}

// ParseOpsSN is a helper for callers building an SN from a query.
func ParseOpsSN(raw string) uint32 {
	n, _ := strconv.ParseUint(strings.TrimSpace(raw), 10, 32)
	return uint32(n)
}
