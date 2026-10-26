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

	// MarshalCatalogWithMedia renders a catalog answer, embedding a
	// per-device MediaSource extension block in each Item whose DeviceID
	// appears in mediaByDeviceID with a non-zero MediaConfig. Used by the
	// device side to broadcast its media-source configuration through
	// the catalog so a platform can rebuild it on the other side. nil
	// mediaByDeviceID and the absence of a key both result in the legacy
	// shape (no Extension). This is the change-driven superset of
	// MarshalCatalog.
	MarshalCatalogWithMedia(catalog model.Catalog, mediaByDeviceID map[string]model.MediaConfig) (string, error)

	// DecodeCatalogResponse parses a Catalog response body and returns
	// the catalog it carries together with the per-channel media
	// configuration it advertises (only for items that carry an
	// Extension/MediaSource block). A catalog without an Extension
	// block is still fully parsed; the returned media slice is empty
	// in that case. Used by the platform side to harvest a device's
	// media-source configuration from the catalog it just received.
	DecodeCatalogResponse(body string) (model.Catalog, []ChannelMedia, error)

	// MarshalCatalogQuery renders a catalog query MESSAGE body sent by a
	// platform to pull a downstream device's catalog. The caller provides
	// the target device id and a per-call serial number; the asker's
	// identity travels in the SIP From header built around this body. The
	// result is placed verbatim as the body of a SIP MESSAGE whose To
	// header is the target device's URI. Used by CatalogPoller to actively
	// harvest a device's catalog on a fixed interval.
	MarshalCatalogQuery(targetDeviceID string, sn uint32) (string, error)

	// DecodeDeviceInfoQuery parses a DeviceInfo query.
	DecodeDeviceInfoQuery(body string) (model.DeviceInfoQuery, error)

	// MarshalDeviceInfoResponse renders a DeviceInfo answer.
	MarshalDeviceInfoResponse(resp model.DeviceInfoResponse) (string, error)

	// MarshalRecordInfoQuery renders a RecordInfo query body.
	MarshalRecordInfoQuery(query model.RecordInfoQuery, sn uint32) (string, error)

	// DecodeRecordInfoResponse parses a RecordInfo answer.
	DecodeRecordInfoResponse(body string) (model.RecordInfoResponse, error)

	// DecodeRecordInfoQuery parses a RecordInfo query.
	DecodeRecordInfoQuery(body string) (model.RecordInfoQuery, error)

	// MarshalRecordInfoResponse renders a RecordInfo answer.
	MarshalRecordInfoResponse(resp model.RecordInfoResponse) (string, error)

	// DecodeAlarmNotify parses an Alarm notify.
	DecodeAlarmNotify(body string) (model.AlarmNotify, error)

	// MarshalAlarmAck renders an Alarm acknowledgement.
	MarshalAlarmAck(ack model.AlarmAck) (string, error)

	// MarshalCatalogNotify renders a catalog subscription notification body,
	// declaration included and terminated by a newline. The result is
	// placed verbatim as a NOTIFY message body.
	MarshalCatalogNotify(catalog model.Catalog) (string, error)

	// MarshalAlarmNotify renders an Alarm notify body, declaration included
	// and terminated by a newline. The result is placed verbatim as a
	// NOTIFY message body.
	MarshalAlarmNotify(n model.AlarmNotify) (string, error)

	// MarshalMobilePositionNotify renders a MobilePosition notify body,
	// declaration included and terminated by a newline. The result is
	// placed verbatim as a NOTIFY message body.
	MarshalMobilePositionNotify(mp model.MobilePositionNotify) (string, error)

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
