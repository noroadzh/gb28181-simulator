// Package port — MANSCDP codec.
package port

import "github.com/your-org/gb28181-simulator/internal/domain/model"

// MANSCDPCodec reads and writes the MANSCDP bodies a platform exchanges:
// the notify a downstream sends and the catalog answer the platform gives
// back.
//
// It is the platform's half of the protocol and deliberately separate from
// KeepaliveCodec, which is the device's half (rendering only). Merging them
// would force every device to carry a parser it never calls.
//
// Implementations MUST NOT panic on malformed input: a body another
// vendor's device produced is data, and a platform that crashes on data is
// a platform that can be switched off remotely. They return an error
// instead, and the caller decides to ignore it.
type MANSCDPCodec interface {
	// DecodeNotify parses a MANSCDP notify body into its command. It
	// returns an error when the body is not XML, is not a notify, or
	// carries no command type or device id.
	DecodeNotify(body string) (model.Notify, error)

	// MarshalCatalog renders a catalog answer, declaration included and
	// terminated by a newline. The result is placed verbatim as the
	// message body.
	MarshalCatalog(catalog model.Catalog) (string, error)

	// DecodeDeviceInfoQuery parses a DeviceInfo query.
	DecodeDeviceInfoQuery(body string) (model.DeviceInfoQuery, error)

	// MarshalDeviceInfoResponse renders a DeviceInfo answer.
	MarshalDeviceInfoResponse(resp model.DeviceInfoResponse) (string, error)

	// DecodeRecordInfoQuery parses a RecordInfo query.
	DecodeRecordInfoQuery(body string) (model.RecordInfoQuery, error)

	// MarshalRecordInfoResponse renders a RecordInfo answer.
	MarshalRecordInfoResponse(resp model.RecordInfoResponse) (string, error)

	// DecodeAlarmNotify parses an Alarm notify.
	DecodeAlarmNotify(body string) (model.AlarmNotify, error)

	// MarshalAlarmAck renders an Alarm acknowledgement.
	MarshalAlarmAck(ack model.AlarmAck) (string, error)

	// DecodePTZControl parses a DeviceControl (PTZ) command.
	DecodePTZControl(body string) (model.PTZControl, error)

	// MarshalPTZControl renders a PTZ command acknowledgement.
	MarshalPTZControl(control model.PTZControl) (string, error)

	// DecodePresetQuery parses a PresetQuery command.
	DecodePresetQuery(body string) (model.PresetQuery, error)

	// MarshalPresetList renders a PresetQuery answer.
	MarshalPresetList(resp model.PresetListResponse) (string, error)

	// MarshalPresetAck renders a PresetQuery acknowledgement.
	MarshalPresetAck(ack model.PresetAck) (string, error)

	// --- GB/T 28181-2022 commands ------------------------------------------------

	// DecodeHomePositionQuery parses a HomePosition query body.
	DecodeHomePositionQuery(body string) (model.HomePositionQuery, error)

	// MarshalHomePositionQuery renders a HomePosition query body.
	MarshalHomePositionQuery(query model.HomePositionQuery, sn uint32) (string, error)

	// DecodeHomePositionSet parses a HomePosition set command body.
	DecodeHomePositionSet(body string) (model.HomePositionSet, error)

	// MarshalHomePositionSet renders a HomePosition set command body.
	MarshalHomePositionSet(position model.HomePosition, sn uint32) (string, error)

	// DecodeHomePositionResponse parses a HomePosition response body.
	DecodeHomePositionResponse(body string) (model.HomePositionResponse, error)

	// MarshalHomePositionResponse renders a HomePosition response body.
	MarshalHomePositionResponse(resp model.HomePositionResponse, sn uint32) (string, error)

	// DecodeCruiseTrackListQuery parses a CruiseTrackList query body.
	DecodeCruiseTrackListQuery(body string) (model.CruiseTrackListQuery, error)

	// MarshalCruiseTrackListQuery renders a CruiseTrackList query body.
	MarshalCruiseTrackListQuery(query model.CruiseTrackListQuery, sn uint32) (string, error)

	// DecodeCruiseTrackListResponse parses a CruiseTrackList response body.
	DecodeCruiseTrackListResponse(body string) (model.CruiseTrackListResponse, error)

	// MarshalCruiseTrackListResponse renders a CruiseTrackList response body.
	MarshalCruiseTrackListResponse(resp model.CruiseTrackListResponse, sn uint32) (string, error)

	// DecodeSnapShotCommand parses a SnapShot command body.
	DecodeSnapShotCommand(body string) (model.SnapShotCommand, error)

	// MarshalSnapShotCommand renders a SnapShot command body.
	MarshalSnapShotCommand(cmd model.SnapShotCommand, sn uint32) (string, error)

	// DecodeSnapShotResponse parses a SnapShot response body.
	DecodeSnapShotResponse(body string) (model.SnapShotResponse, error)

	// MarshalSnapShotResponse renders a SnapShot response body.
	MarshalSnapShotResponse(resp model.SnapShotResponse, sn uint32) (string, error)
}
