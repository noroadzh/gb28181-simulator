package manscdp

import (
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// MANSCDPCodecAdapter reads and writes the MANSCDP bodies a platform sees.
// It holds no state: every body is fully described by the value it is given.
type MANSCDPCodecAdapter struct{}

// NewMANSCDPCodec returns a codec ready to parse notifies, render
// catalogs, and handle GB/T 28181-2022 incremental commands.
func NewMANSCDPCodec() *MANSCDPCodecAdapter { return &MANSCDPCodecAdapter{} }

// Compile-time check that the adapter satisfies the domain port.
var _ port.MANSCDPCodec = (*MANSCDPCodecAdapter)(nil)
