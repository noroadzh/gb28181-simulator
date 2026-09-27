package sdp

import (
	"fmt"
	"sort"
	"strconv"

	pionsdp "github.com/pion/sdp"
)

// MediaCapabilityModule names a GB/T 28181-2022 SDP capability module.
type MediaCapabilityModule string

const (
	// ModuleH265 declares H.265/HEVC video capability (payload 100).
	ModuleH265 MediaCapabilityModule = "H265"
	// ModuleAAC declares MPEG-4 AAC-LC audio capability (payload 97).
	ModuleAAC MediaCapabilityModule = "AAC"
	// ModuleG7221 declares G.722.1 audio capability (payload 99).
	ModuleG7221 MediaCapabilityModule = "G7221"
)

// PayloadType returns the RTP dynamic payload type assigned to this module.
// Returns 0 for unknown module names.
func (m MediaCapabilityModule) PayloadType() uint8 {
	switch m {
	case ModuleH265:
		return 100
	case ModuleAAC:
		return 97
	case ModuleG7221:
		return 99
	}
	return 0
}

// RTPMapValue returns the rtpmap attribute value string for this module.
// Returns empty string for unknown module names.
func (m MediaCapabilityModule) RTPMapValue() string {
	switch m {
	case ModuleH265:
		return "H265/90000"
	case ModuleAAC:
		return "MPEG4-GENERIC/16000/2"
	case ModuleG7221:
		return "G7221/16000"
	}
	return ""
}

// DeclareCapability appends a=rtpmap: lines and the corresponding payload
// type to the m= line's format list for each enabled module. The call is
// idempotent: modules whose payload type is already present in the format
// list are silently skipped. Unknown module names are also silently skipped.
//
// Rendering order: newly-added payload types are sorted numerically so the
// m= line and rtpmap lines appear in ascending payload-type order regardless
// of the call order.
func (mb *MediaBlock) DeclareCapability(modules ...MediaCapabilityModule) {
	if mb == nil || mb.MediaDescription == nil || mb.MediaDescription.MediaName.Formats == nil {
		return
	}

	formats := mb.MediaDescription.MediaName.Formats
	// Build index of format strings already present (e.g. "96", "8").
	existing := make(map[string]struct{}, len(formats))
	for _, f := range formats {
		existing[f] = struct{}{}
	}

	var added []string
	for _, mod := range modules {
		pt := mod.PayloadType()
		if pt == 0 {
			continue
		}
		key := fmt.Sprintf("%d", pt)
		if _, ok := existing[key]; ok {
			continue
		}
		formats = append(formats, key)
		mb.Attributes = append(mb.Attributes, pionsdp.Attribute{
			Key:   "rtpmap",
			Value: fmt.Sprintf("%d %s", pt, mod.RTPMapValue()),
		})
		added = append(added, key)
	}

	// Write back (may have re-sliced).
	mb.MediaDescription.MediaName.Formats = formats

	if len(added) > 1 {
		// Sort the format list by numeric payload type for deterministic output.
		sort.Slice(mb.MediaDescription.MediaName.Formats, func(i, j int) bool {
			ni, _ := strconv.Atoi(mb.MediaDescription.MediaName.Formats[i])
			nj, _ := strconv.Atoi(mb.MediaDescription.MediaName.Formats[j])
			return ni < nj
		})
	}
}
