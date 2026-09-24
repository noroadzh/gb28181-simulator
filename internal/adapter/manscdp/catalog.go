package manscdp

import (
	"encoding/xml"
	"fmt"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// catalogItem is the wire shape of one device in a catalog answer. Field
// order is the order of the struct, which is what makes the output stable
// enough for a golden test.
type catalogItem struct {
	DeviceID     string `xml:"DeviceID"`
	Name         string `xml:"Name"`
	Manufacturer string `xml:"Manufacturer"`
	Model        string `xml:"Model"`
	Owner        string `xml:"Owner,omitempty"`
	CivilCode    string `xml:"CivilCode"`
	Address      string `xml:"Address,omitempty"`
	Parental     int    `xml:"Parental"`
	SafetyWay    int    `xml:"SafetyWay"`
	RegisterWay  int    `xml:"RegisterWay"`
	Secrecy      int    `xml:"Secrecy"`
	Status       string `xml:"Status"`
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

// MarshalCatalog renders a catalog answer, declaration included and
// terminated by a newline.
//
// An empty table is a legitimate answer, not an error: `SumNum = 0` tells
// the asker the platform is there and has nobody online. A catalog that was
// never built by model.NewCatalog is rejected rather than rendered as an
// empty response.
func (c *MANSCDPCodecAdapter) MarshalCatalog(catalog model.Catalog) (string, error) {
	if !catalog.HasCatalog() {
		return "", fmt.Errorf("manscdp: catalog without a device id")
	}
	items := make([]catalogItem, 0, catalog.SumNum())
	for _, it := range catalog.Items() {
		items = append(items, catalogItem{
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
		})
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
