package main

import (
	"fmt"
	"os"

	"github.com/your-org/gb28181-simulator/internal/adapter/media"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

func main() {
	pktz := media.NewPSPacketizer(nil)
	vFrame := model.NewESFrame([]byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x80, 0x0A, 0x00, 0x00, 0x00, 0x01, 0x68, 0xCE, 0x38, 0x80, 0x00, 0x00, 0x00, 0x01, 0x65, 0x88, 0x80, 0x12, 0x01, 0x02, 0x03, 0x04, 0x05}, 270000)
	vPS, err := pktz.Packetize(vFrame)
	if err != nil {
		panic(err)
	}
	aFrame := model.ESFrame{Payload: []byte{0x01, 0x02, 0x03, 0x04}, PTS: 270000, Kind: model.ESFrameAudio}
	aPS, err := pktz.Packetize(aFrame)
	if err != nil {
		panic(err)
	}
	rtp := media.NewRTPizer(0xABCDEF01, 1400, nil)
	vPkts, err := rtp.Packetize(vPS)
	if err != nil {
		panic(err)
	}
	aPkts, err := rtp.Packetize(aPS)
	if err != nil {
		panic(err)
	}
	f, err := os.Create("internal/adapter/media/testdata/rtp-roundtrip.bin")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	for _, p := range vPkts {
		f.Write(p.Payload)
	}
	for _, p := range aPkts {
		f.Write(p.Payload)
	}
	fmt.Println("wrote rtp-roundtrip.bin")
}
