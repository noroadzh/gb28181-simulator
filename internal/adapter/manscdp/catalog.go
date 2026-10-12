package manscdp

import (
	"encoding/xml"
	"fmt"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// Extension is the simulator-internal XML block embedded in a catalog Item to
// carry metadata the GB/T 28181 standard body doesn't reserve a field for. It
// is opaque to a standard platform parser (it ignores unknown children), so
// adding it is wire-compatible: real devices, real platforms, and old peers
// keep working unchanged.
//
// The only field used today is MediaSource; future simulator-specific metadata
// (firmware hints, scenario tags, …) is meant to grow as siblings, not as
// parallel extensions.
type Extension struct {
	XMLName     xml.Name     `xml:"Extension"`
	MediaSource *MediaSource `xml:"MediaSource,omitempty"`
}

// MediaSource is the simulator-internal mirror of model.MediaConfig rendered
// as XML. A non-nil value here is the wire signal that a catalog item carries
// media-source configuration. Empty fields are omitted so a zero-value
// MediaSource round-trips to an absent element (and therefore a nil
// MediaSource on the decoder side), letting a peer override individual
// properties without resending the rest.
//
// Field names are short, lower-camel-case strings to keep the catalog body
// compact; they map 1:1 to model.MediaConfig.
type MediaSource struct {
	Kind  string `xml:"kind,attr,omitempty"`
	Path  string `xml:"path,attr,omitempty"`
	Loop  string `xml:"loop,attr,omitempty"` // "true" / "false"; "" means unset
	SSRC  uint32 `xml:"ssrc,attr,omitempty"`
	MTU   int    `xml:"mtu,attr,omitempty"`
	FPS   int    `xml:"fps,attr,omitempty"`
	Clock uint64 `xml:"clock,attr,omitempty"`
}

// FromMediaConfig builds a MediaSource from a model.MediaConfig; returns nil
// for an empty (zero-value) config so the parent Extension is omitted
// altogether. Used by MarshalCatalogWithMedia when the caller wants to embed
// the config inline.
func (*MediaSource) FromMediaConfig(cfg model.MediaConfig) *MediaSource {
	if (cfg == model.MediaConfig{}) {
		return nil
	}
	out := &MediaSource{
		Kind:  string(cfg.Kind),
		Path:  cfg.Path,
		SSRC:  cfg.SSRC,
		MTU:   cfg.MTU,
		FPS:   cfg.FPS,
		Clock: cfg.Clock,
	}
	if cfg.Loop {
		out.Loop = "true"
	}
	return out
}

// ToMediaConfig is the inverse of FromMediaConfig; missing attributes stay at
// the zero value, callers should Normalize before validating.
func (m MediaSource) ToMediaConfig() model.MediaConfig {
	cfg := model.MediaConfig{
		Kind:  model.MediaSourceKind(m.Kind),
		Path:  m.Path,
		SSRC:  m.SSRC,
		MTU:   m.MTU,
		FPS:   m.FPS,
		Clock: m.Clock,
	}
	if m.Loop == "true" {
		cfg.Loop = true
	}
	return cfg
}

// catalogItem is the wire shape of one device in a catalog answer. Field
// order is the order of the struct, which is what makes the output stable
// enough for a golden test.
type catalogItem struct {
	DeviceID     string     `xml:"DeviceID"`
	Name         string     `xml:"Name"`
	Manufacturer string     `xml:"Manufacturer"`
	Model        string     `xml:"Model"`
	Owner        string     `xml:"Owner,omitempty"`
	CivilCode    string     `xml:"CivilCode"`
	Address      string     `xml:"Address,omitempty"`
	Parental     int        `xml:"Parental"`
	SafetyWay    int        `xml:"SafetyWay"`
	RegisterWay  int        `xml:"RegisterWay"`
	Secrecy      int        `xml:"Secrecy"`
	Status       string     `xml:"Status"`
	Extension    *Extension `xml:"Extension,omitempty"`
}

// deviceList carries the entries and the count GB/T 28181 repeats as an
// attribute, so a peer can size its buffer before reading the items.
type deviceList struct {
	Num   int           `xml:"Num,attr"`
	Items []catalogItem `xml:"Item"`
}

// catalogResponse is the wire shape of a catalog answer.
type catalogResponse struct {
	XMLName    xml.Name   `xml:"Response"`
	CmdType    string     `xml:"CmdType"`
	SN         uint32     `xml:"SN"`
	DeviceID   string     `xml:"DeviceID"`
	SumNum     int        `xml:"SumNum"`
	DeviceList deviceList `xml:"DeviceList"`
}

// catalogQuery is the wire shape of a catalog query: the same envelope as a
// Response minus the payload. GB/T 28181 distinguishes the two purely by the
// root element name — a Query carries only the addressing triple and asks the
// peer to fill in the rest, a Response carries the answer. Keeping them as
// separate structs (rather than one with optional fields) is what makes the
// compiler enforce that a Query can never accidentally grow a DeviceList.
type catalogQuery struct {
	XMLName  xml.Name `xml:"Query"`
	CmdType  string   `xml:"CmdType"`
	SN       uint32   `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
}

// MarshalCatalog renders a catalog answer, declaration included and
// terminated by a newline. No Item carries a media-source extension; use
// MarshalCatalogWithMedia for that. Keeping the two-entry API lets existing
// callers (and golden tests) keep their byte-for-byte contract.
//
// An empty table is a legitimate answer, not an error: `SumNum = 0` tells
// the asker the platform is there and has nobody online. A catalog that was
// never built by model.NewCatalog is rejected rather than rendered as an
// empty response.
func (c *MANSCDPCodecAdapter) MarshalCatalog(catalog model.Catalog) (string, error) {
	return c.MarshalCatalogWithMedia(catalog, nil)
}

// MarshalCatalogWithMedia renders a catalog answer, embedding an Extension
// block in each Item whose DeviceID appears as a key in mediaByDeviceID and
// whose value is a non-zero MediaConfig. A nil map or a missing key is
// equivalent to "no media for this device" — its Item carries no Extension
// element. This is the entry point used by the device-side catalog handler
// in internal/app so the platform node can rebuild the channel's media
// configuration on the other side of the SIP hop.
func (c *MANSCDPCodecAdapter) MarshalCatalogWithMedia(catalog model.Catalog, mediaByDeviceID map[string]model.MediaConfig) (string, error) {
	if !catalog.HasCatalog() {
		return "", fmt.Errorf("manscdp: catalog without a device id")
	}
	items := make([]catalogItem, 0, catalog.SumNum())
	for _, it := range catalog.Items() {
		wire := catalogItem{
			DeviceID:     it.DeviceID(),
			Name:         it.Name(),
			Manufacturer: it.Manufacturer(),
			Model:        it.Model(),
			Owner:        it.Owner(),
			CivilCode:    it.CivilCode(),
			Address:      it.Address(),
			Parental:     it.Parental(),
			SafetyWay:    it.SafetyWay(),
			RegisterWay:  it.RegisterWay(),
			Secrecy:      it.Secrecy(),
			Status:       it.Status(),
		}
		if mediaByDeviceID != nil {
			if cfg, ok := mediaByDeviceID[it.DeviceID()]; ok {
				if ms := new(MediaSource).FromMediaConfig(cfg); ms != nil {
					wire.Extension = &Extension{MediaSource: ms}
				}
			}
		}
		items = append(items, wire)
	}
	body, err := xml.MarshalIndent(catalogResponse{
		CmdType:  model.CmdTypeCatalog,
		SN:       catalog.SN(),
		DeviceID: catalog.DeviceID(),
		SumNum:   catalog.SumNum(),
		DeviceList: deviceList{
			Num:   catalog.SumNum(),
			Items: items,
		},
	}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("manscdp: marshal catalog: %w", err)
	}
	return xmlHeader + string(body) + "\n", nil
}

// DecodeCatalogResponse parses a Catalog response body (the body of a SIP
// MESSAGE answer) and returns the catalog it carries. Items whose Extension
// block fails to decode are reported via a non-nil partial — but only when
// the outer XML parses cleanly enough to read the catalog at all. The
// returned Catalog is always usable; the extension (when present) is
// surfaced through the side channel below.
//
// This is the inverse of MarshalCatalogWithMedia; the round trip is the
// wire contract that keeps the simulator's two halves aligned.
func (c *MANSCDPCodecAdapter) DecodeCatalogResponse(body string) (model.Catalog, []port.ChannelMedia, error) {
	if body == "" {
		return model.Catalog{}, nil, fmt.Errorf("manscdp: empty catalog body")
	}
	var raw catalogResponse
	if err := xml.Unmarshal([]byte(body), &raw); err != nil {
		return model.Catalog{}, nil, fmt.Errorf("manscdp: decode catalog: %w", err)
	}
	if raw.CmdType != model.CmdTypeCatalog {
		return model.Catalog{}, nil, fmt.Errorf("manscdp: catalog cmd type = %q, want %q", raw.CmdType, model.CmdTypeCatalog)
	}
	items := make([]model.CatalogItem, 0, len(raw.DeviceList.Items))
	var media []port.ChannelMedia
	for _, w := range raw.DeviceList.Items {
		ci, err := model.NewCatalogItem(model.CatalogItemParams{
			DeviceID:     w.DeviceID,
			Name:         w.Name,
			Manufacturer: w.Manufacturer,
			Model:        w.Model,
			Owner:        w.Owner,
			CivilCode:    w.CivilCode,
			Address:      w.Address,
			Parental:     w.Parental,
			SafetyWay:    w.SafetyWay,
			RegisterWay:  w.RegisterWay,
			Secrecy:      w.Secrecy,
			Status:       w.Status,
		})
		if err != nil {
			// A single bad device id in the middle of an otherwise fine
			// catalog shouldn't take the whole answer down. Skip and warn.
			continue
		}
		items = append(items, ci)
		if w.Extension != nil && w.Extension.MediaSource != nil {
			media = append(media, port.ChannelMedia{
				DeviceID: w.DeviceID,
				Config:   w.Extension.MediaSource.ToMediaConfig(),
			})
		}
	}
	cat, err := model.NewCatalog(raw.DeviceID, raw.SN, items)
	if err != nil {
		return model.Catalog{}, media, fmt.Errorf("manscdp: rebuild catalog: %w", err)
	}
	return cat, media, nil
}

// MarshalCatalogQuery renders the body of a catalog query MESSAGE — a
// platform actively asking a downstream device for its channel list. This is
// the direction the media-source propagation needs: a device that never
// receives a Catalog query would otherwise never advertise its media-source
// configuration, leaving the platform's profile with nothing to rebuild from.
//
// The payload is the addressing triple only. DeviceID is the *target* (the
// device being asked), never the asker — a peer that answered with its own
// id in this field would be talking about itself. The asker's identity travels
// in the SIP From header, which the caller builds around this body, so it is
// deliberately not a parameter here.
//
// An empty target or an SN of zero are rejected: both are unrecoverable on
// the wire (a device cannot correlate an answer without them), and sending
// them would burn a poll cycle for nothing.
func (c *MANSCDPCodecAdapter) MarshalCatalogQuery(targetDeviceID string, sn uint32) (string, error) {
	if targetDeviceID == "" {
		return "", fmt.Errorf("manscdp: catalog query with no target device")
	}
	if sn == 0 {
		return "", fmt.Errorf("manscdp: catalog query with sn 0")
	}
	body, err := xml.MarshalIndent(catalogQuery{
		CmdType:  model.CmdTypeCatalog,
		SN:       sn,
		DeviceID: targetDeviceID,
	}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("manscdp: marshal catalog query: %w", err)
	}
	return xmlHeader + string(body) + "\n", nil
}
