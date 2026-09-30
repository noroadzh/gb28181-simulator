// Package media — MP4 container demuxer.
//
// MP4Demuxer turns a local mp4 file into an ordered ES-frame stream:
//
//   - video samples (avc1 / hvc1) are rewritten from AVCC/HVCC length-prefixed
//     NALUs to Annex-B start-code framing (00 00 00 01),
//   - audio samples (mp4a carrying AAC) gain a 7-byte ADTS header derived
//     from the AudioSpecificConfig in esds,
//   - every frame carries the container PTS converted into the 90 kHz domain
//     the GB/T 28181 pipeline uses,
//   - the end of the container is not the end of the stream: reading past
//     the last sample loops back to the first one.
//
// Box walking and the sample tables (mdhd / stts / stsc / stsz / stco / co64)
// are delegated to github.com/abema/go-mp4. The codec-specific tail of the
// stsd box (avcC / hvcC / esds) is parsed locally: go-mp4 refuses to expand
// into unknown child box types, and sample entries are exactly that.
package media

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/abema/go-mp4"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// annexBStartCode prefixes every NAL unit of a converted video sample.
var annexBStartCode = []byte{0x00, 0x00, 0x00, 0x01}

// bpath builds a BoxPath from plain strings.
func bpath(t ...string) mp4.BoxPath {
	out := make(mp4.BoxPath, len(t))
	for i, s := range t {
		out[i] = mp4.StrToBoxType(s)
	}
	return out
}

// Fixed part sizes, in bytes, of a sample entry minus its 8-byte box
// header — everything before the first child box (avcC, esds, ...).
const (
	audioEntryFixed = 6 + 2 + // SampleEntry: reserved + data_reference_index
		4 + 4 + 2 + 2 + 2 + 2 + 4 // AudioSampleEntry body
	videoEntryFixed = 6 + 2 + // SampleEntry: reserved + data_reference_index
		2 + 2 + 12 + 2 + 2 + 4 + 4 + 4 + 2 + 32 + 2 + 2 // VisualSampleEntry body
)

// mp4Track is one decoded track: its sample table flattened to
// (file offset, size, 90 kHz PTS) triples plus whatever codec config the
// framing conversion needs.
type mp4Track struct {
	kind  string // "video" or "audio"
	codec string // "avc1", "hvc1" or "mp4a"

	timescale uint64 // media timescale from mdhd
	samples   []mp4Sample

	// video: NALU length prefix size; SPS/PPS (Annex-B framed) prepended
	// once per loop iteration so decoders see parameter sets before the
	// first slice. hvcC parameter sets are not extracted.
	nalLenSize int
	vpsSpsPps  []byte

	// audio: AudioSpecificConfig from esds, from which the ADTS header
	// is derived per frame.
	asc []byte
}

type mp4Sample struct {
	offset int64
	size   int
	pts    uint64 // 90 kHz domain
}

// MP4Demuxer is the reader FileSource returns for a container-detected mp4
// file. It satisfies both io.ReadCloser (the MediaSource contract) and
// port.ESFrameReader (so MediaService uses container PTS directly instead
// of synthesising it from FPS).
type MP4Demuxer struct {
	f      *os.File
	tracks []*mp4Track // [0] video (if any), then audio
	order  []sampleRef // tracks' samples merged in PTS order
	pos    int         // next entry in order

	// io.ReadCloser plumbing: one frame materialised at a time.
	pending []byte
	closed  bool
}

type sampleRef struct {
	track int
	index int
}

var (
	_ port.ESFrameReader = (*MP4Demuxer)(nil)
)

// mp4ReadCloser wraps an MP4Demuxer so it satisfies io.ReadCloser.
// MediaSource.Open must return io.ReadCloser; port.ESFrameReader is the
// preferred (precise) path consumed by MediaService.OpenSource.
type mp4ReadCloser struct {
	d *MP4Demuxer
}

func (c *mp4ReadCloser) Read(p []byte) (int, error) {
	if c.d == nil || c.d.f == nil || c.d.closed {
		return 0, io.EOF
	}
	// Skip over degenerate empty frames so a zero-size sample cannot spin
	// this loop forever.
	for i := 0; i < 16 && len(c.d.pending) == 0; i++ {
		frame, err := c.d.ReadFrame(context.Background())
		if err != nil {
			return 0, err
		}
		c.d.pending = frame.Payload
	}
	if len(c.d.pending) == 0 {
		return 0, io.ErrNoProgress
	}
	n := copy(p, c.d.pending)
	c.d.pending = c.d.pending[n:]
	return n, nil
}

func (c *mp4ReadCloser) Close() error { return c.d.Close() }

// openMP4 parses the container structure of f and builds the demuxer.
// A file without a usable moov, without a supported track, or with broken
// sample tables is rejected with an error — never a panic.
func openMP4(f *os.File) (*MP4Demuxer, error) {
	trakBIs, err := mp4.ExtractBoxes(f, nil, []mp4.BoxPath{bpath("moov", "trak")})
	if err != nil {
		return nil, fmt.Errorf("media: walk moov/trak: %w", err)
	}
	if len(trakBIs) == 0 {
		return nil, fmt.Errorf("media: mp4 without a trak box")
	}

	var video, audio *mp4Track
	for _, trakBI := range trakBIs {
		trk, err := parseTrack(f, trakBI)
		if err != nil {
			return nil, err
		}
		if trk == nil {
			continue // hint, metadata or otherwise unusable track
		}
		switch trk.kind {
		case "video":
			if video == nil {
				video = trk
			}
		case "audio":
			if audio == nil {
				audio = trk
			}
		}
	}
	if video == nil && audio == nil {
		return nil, fmt.Errorf("media: mp4 without a supported video or audio track")
	}

	d := &MP4Demuxer{f: f}
	if video != nil {
		d.tracks = append(d.tracks, video)
	}
	if audio != nil {
		d.tracks = append(d.tracks, audio)
	}
	d.order = mergeSampleOrder(d.tracks)
	if len(d.order) == 0 {
		return nil, fmt.Errorf("media: mp4 without any media sample")
	}
	return d, nil
}

// parseTrack extracts one trak's handler type, timescale, sample table and
// codec config. It returns (nil, nil) for tracks this demuxer does not
// serve (non-avc1/hvc1 video, non-AAC audio, hint or metadata tracks).
func parseTrack(f *os.File, trakBI *mp4.BoxInfo) (*mp4Track, error) {
	boxes, err := mp4.ExtractBoxesWithPayload(f, trakBI, []mp4.BoxPath{
		bpath("mdia", "hdlr"),
		bpath("mdia", "mdhd"),
		bpath("mdia", "minf", "stbl", "stts"),
		bpath("mdia", "minf", "stbl", "stsc"),
		bpath("mdia", "minf", "stbl", "stsz"),
		bpath("mdia", "minf", "stbl", "stco"),
		bpath("mdia", "minf", "stbl", "co64"),
	})
	if err != nil {
		return nil, fmt.Errorf("media: walk trak: %w", err)
	}

	var hdlr *mp4.Hdlr
	var mdhd *mp4.Mdhd
	var stts *mp4.Stts
	var stsc *mp4.Stsc
	var stsz *mp4.Stsz
	var stco *mp4.Stco
	var co64 *mp4.Co64
	for _, b := range boxes {
		switch box := b.Payload.(type) {
		case *mp4.Hdlr:
			hdlr = box
		case *mp4.Mdhd:
			mdhd = box
		case *mp4.Stts:
			stts = box
		case *mp4.Stsc:
			stsc = box
		case *mp4.Stsz:
			stsz = box
		case *mp4.Stco:
			stco = box
		case *mp4.Co64:
			co64 = box
		}
	}
	if hdlr == nil || mdhd == nil || stts == nil || stsc == nil || stsz == nil {
		return nil, nil // not a media track we can serve
	}
	kind := string(hdlr.HandlerType[:])
	if kind != "vide" && kind != "soun" {
		return nil, nil
	}
	if mdhd.Timescale == 0 {
		return nil, fmt.Errorf("media: track with zero timescale")
	}

	// Chunk offsets: stco (32-bit) or its co64 (64-bit) sibling.
	var chunkOffsets []uint64
	switch {
	case stco != nil:
		for _, off := range stco.ChunkOffset {
			chunkOffsets = append(chunkOffsets, uint64(off))
		}
	case co64 != nil:
		chunkOffsets = append(chunkOffsets, co64.ChunkOffset...)
	default:
		return nil, fmt.Errorf("media: track without stco/co64")
	}
	if len(chunkOffsets) == 0 {
		return nil, fmt.Errorf("media: track with an empty chunk offset table")
	}

	// Flatten stsc × chunk offsets × stsz into per-sample file offsets.
	sizes := sampleSizes(stsz)
	samples := make([]mp4Sample, 0, len(sizes))
	si := 0 // sample index across the whole track
	for e, entry := range stsc.Entries {
		last := uint32(len(chunkOffsets) + 1)
		if e+1 < len(stsc.Entries) {
			last = stsc.Entries[e+1].FirstChunk
		}
		for chunk := entry.FirstChunk; chunk < last; chunk++ {
			if chunk == 0 || int(chunk) > len(chunkOffsets) {
				return nil, fmt.Errorf("media: stsc references chunk %d beyond stco", chunk)
			}
			offset := chunkOffsets[chunk-1]
			for s := uint32(0); s < entry.SamplesPerChunk && si < len(sizes); s++ {
				samples = append(samples, mp4Sample{
					offset: int64(offset),
					size:   sizes[si],
				})
				offset += uint64(sizes[si])
				si++
			}
		}
	}
	if len(samples) != len(sizes) {
		return nil, fmt.Errorf("media: stsc covers %d of %d samples", len(samples), len(sizes))
	}

	// PTS: cumulative stts deltas converted into the 90 kHz domain. The
	// samples slice is index-aligned with the stts sequence.
	t := &mp4Track{
		kind:      map[string]string{"vide": "video", "soun": "audio"}[kind],
		timescale: uint64(mdhd.Timescale),
		samples:   samples,
	}
	mediaTS := uint64(0)
	for i := range t.samples {
		t.samples[i].pts = pts90k(mediaTS, t.timescale)
		mediaTS += uint64(sttsDelta(stts, i))
	}

	// Codec config from the stsd box (parsed by hand; see package doc).
	stsdBIs, err := mp4.ExtractBoxes(f, trakBI, []mp4.BoxPath{bpath("mdia", "minf", "stbl", "stsd")})
	if err != nil {
		return nil, fmt.Errorf("media: locate stsd: %w", err)
	}
	if len(stsdBIs) == 0 {
		return nil, fmt.Errorf("media: track without stsd")
	}
	bi := stsdBIs[0]
	raw := make([]byte, int(bi.Size-bi.HeaderSize))
	if _, err := f.ReadAt(raw, int64(bi.Offset+bi.HeaderSize)); err != nil {
		return nil, fmt.Errorf("media: read stsd: %w", err)
	}
	if err := parseSampleEntries(t, raw); err != nil {
		return nil, err
	}
	if t.codec == "" {
		return nil, nil // unsupported codec: skip the track
	}
	return t, nil
}

// pts90k converts a media-domain timestamp to the 90 kHz domain. Rounding
// errors stay below half a tick for every timescale that fits a uint32.
func pts90k(media, timescale uint64) uint64 {
	return media * 90000 / timescale
}

// sttsDelta returns the decode delta of sample index j; samples past the
// last entry repeat the final delta.
func sttsDelta(stts *mp4.Stts, j int) uint32 {
	if len(stts.Entries) == 0 {
		return 0
	}
	consumed := 0
	for _, entry := range stts.Entries {
		if j < consumed+int(entry.SampleCount) {
			return entry.SampleDelta
		}
		consumed += int(entry.SampleCount)
	}
	return stts.Entries[len(stts.Entries)-1].SampleDelta
}

// sampleSizes flattens stsz into one size per sample, honouring the
// fixed-size variant.
func sampleSizes(stsz *mp4.Stsz) []int {
	if stsz.SampleSize != 0 {
		out := make([]int, stsz.SampleCount)
		for i := range out {
			out[i] = int(stsz.SampleSize)
		}
		return out
	}
	out := make([]int, len(stsz.EntrySize))
	for i, s := range stsz.EntrySize {
		out[i] = int(s)
	}
	return out
}

// mergeSampleOrder interleaves the tracks' samples in PTS order. Ties keep
// the writer's sample order, so audio and video stay deterministic.
func mergeSampleOrder(tracks []*mp4Track) []sampleRef {
	type tagged struct {
		ref sampleRef
		pts uint64
		seq int
	}
	var all []tagged
	seq := 0
	for ti, t := range tracks {
		for i := range t.samples {
			all = append(all, tagged{ref: sampleRef{track: ti, index: i}, pts: t.samples[i].pts, seq: seq})
			seq++
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].pts != all[j].pts {
			return all[i].pts < all[j].pts
		}
		return all[i].seq < all[j].seq
	})
	out := make([]sampleRef, len(all))
	for i, tg := range all {
		out[i] = tg.ref
	}
	return out
}

// parseSampleEntries walks the sample entries inside an stsd payload and
// fills the track's codec config. An unsupported first entry leaves
// t.codec empty, which makes parseTrack skip the whole track.
func parseSampleEntries(t *mp4Track, stsd []byte) error {
	if len(stsd) < 8 {
		return fmt.Errorf("media: stsd shorter than its fixed header")
	}
	pos := 8 // version/flags + entry_count
	for pos+8 <= len(stsd) {
		size := int(binary.BigEndian.Uint32(stsd[pos:]))
		format := string(stsd[pos+4 : pos+8])
		if size < 8 || pos+size > len(stsd) {
			return fmt.Errorf("media: stsd entry %q with invalid size %d", format, size)
		}
		entry := stsd[pos : pos+size]
		switch format {
		case "avc1", "avc3":
			if err := t.parseAVC(entry); err != nil {
				return err
			}
		case "hvc1", "hev1":
			if err := t.parseHEVC(entry); err != nil {
				return err
			}
		case "mp4a":
			if err := t.parseMP4A(entry); err != nil {
				return err
			}
		default:
			if t.codec == "" {
				return nil // first entry decides; unsupported → skip track
			}
		}
		pos += size
	}
	return nil
}

func (t *mp4Track) parseAVC(entry []byte) error {
	if t.kind != "video" {
		return fmt.Errorf("media: avc1 sample entry in a %s track", t.kind)
	}
	avcC, err := childBox(entry, "avcC", videoEntryFixed)
	if err != nil {
		return fmt.Errorf("media: avc1 without a parsable avcC: %w", err)
	}
	if len(avcC) < 7 {
		return fmt.Errorf("media: avcC too short (%d bytes)", len(avcC))
	}
	t.codec = "avc1"
	t.nalLenSize = int(avcC[4]&0x03) + 1
	spsPps, err := avcParameterSets(avcC)
	if err != nil {
		return err
	}
	t.vpsSpsPps = spsPps
	return nil
}

func (t *mp4Track) parseHEVC(entry []byte) error {
	if t.kind != "video" {
		return fmt.Errorf("media: hvc1 sample entry in a %s track", t.kind)
	}
	hvcC, err := childBox(entry, "hvcC", videoEntryFixed)
	if err != nil {
		return fmt.Errorf("media: hvc1 without a parsable hvcC: %w", err)
	}
	if len(hvcC) < 23 {
		return fmt.Errorf("media: hvcC too short (%d bytes)", len(hvcC))
	}
	t.codec = "hvc1"
	t.nalLenSize = int(hvcC[21]&0x03) + 1
	return nil
}

func (t *mp4Track) parseMP4A(entry []byte) error {
	if t.kind != "audio" {
		return fmt.Errorf("media: mp4a sample entry in a %s track", t.kind)
	}
	esds, err := childBox(entry, "esds", audioEntryFixed)
	if err != nil {
		return fmt.Errorf("media: mp4a without a parsable esds: %w", err)
	}
	asc, aot, err := audioSpecificConfig(esds)
	if err != nil {
		return fmt.Errorf("media: esds without an AudioSpecificConfig: %w", err)
	}
	if aot != 1 && aot != 2 { // AAC MAIN / AAC LC only
		return fmt.Errorf("media: unsupported audio object type %d (HE-AAC and beyond are out of scope)", aot)
	}
	t.codec = "mp4a"
	t.asc = asc
	return nil
}

// childBox finds one direct child box of a sample entry by fourCC and
// returns its payload. fixed is the byte offset of the first child box
// after the 8-byte box header (audioEntryFixed or videoEntryFixed).
func childBox(entry []byte, fourCC string, fixed int) ([]byte, error) {
	pos := 8 + fixed
	for pos+8 <= len(entry) {
		size := int(binary.BigEndian.Uint32(entry[pos:]))
		typ := string(entry[pos+4 : pos+8])
		if size < 8 || pos+size > len(entry) {
			break
		}
		if typ == fourCC {
			return entry[pos+8 : pos+size], nil
		}
		pos += size
	}
	return nil, fmt.Errorf("box %s not found", fourCC)
}

// avcParameterSets extracts SPS/PPS from an avcC payload as an Annex-B
// blob ready to be prepended to the stream.
func avcParameterSets(avcC []byte) ([]byte, error) {
	var out []byte
	pos := 6 // configurationVersion, profile, compat, level, 0xFF byte
	numSPS := int(avcC[5] & 0x1f)
	for i := 0; i < numSPS; i++ {
		if pos+2 > len(avcC) {
			return nil, fmt.Errorf("media: avcC truncated in the SPS table")
		}
		n := int(binary.BigEndian.Uint16(avcC[pos:]))
		pos += 2
		if pos+n > len(avcC) {
			return nil, fmt.Errorf("media: avcC truncated in an SPS")
		}
		out = append(out, annexBStartCode...)
		out = append(out, avcC[pos:pos+n]...)
		pos += n
	}
	if pos >= len(avcC) {
		return nil, fmt.Errorf("media: avcC missing the PPS count")
	}
	numPPS := int(avcC[pos])
	pos++
	for i := 0; i < numPPS; i++ {
		if pos+2 > len(avcC) {
			return nil, fmt.Errorf("media: avcC truncated in the PPS table")
		}
		n := int(binary.BigEndian.Uint16(avcC[pos:]))
		pos += 2
		if pos+n > len(avcC) {
			return nil, fmt.Errorf("media: avcC truncated in a PPS")
		}
		out = append(out, annexBStartCode...)
		out = append(out, avcC[pos:pos+n]...)
		pos += n
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("media: avcC without any parameter set")
	}
	return out, nil
}

// audioSpecificConfig walks the esds descriptor chain down to the
// DecoderSpecificInfo and returns the ASC bytes plus the audio object type
// (the ASC's own 5-bit AOT field, which is authoritative).
func audioSpecificConfig(esds []byte) (asc []byte, aot int, err error) {
	if len(esds) < 4 {
		return nil, 0, fmt.Errorf("esds shorter than its header")
	}
	err = walkDescriptorsFrom(esds, 4, func(tag byte, payload []byte) {
		if tag == 0x05 { // DecoderSpecificInfo
			asc = append([]byte(nil), payload...)
		}
	})
	if err != nil {
		return nil, 0, err
	}
	if len(asc) == 0 {
		return nil, 0, fmt.Errorf("no DecoderSpecificInfo in the descriptor chain")
	}
	if len(asc) >= 1 {
		aot = int(asc[0] >> 3)
	}
	return asc, aot, nil
}

// walkDescriptorsFrom traverses an ISO 14496-1 descriptor chain (tag byte,
// expandable length, payload) and calls fn for every descriptor, descending
// into the two container kinds that nest further descriptors.
func walkDescriptorsFrom(b []byte, pos int, fn func(tag byte, payload []byte)) error {
	for pos+2 <= len(b) {
		tag := b[pos]
		pos++
		length, used, err := descriptorLength(b, pos)
		if err != nil {
			return err
		}
		pos += used
		if pos+length > len(b) {
			return fmt.Errorf("descriptor payload truncated (tag %#x)", tag)
		}
		payload := b[pos : pos+length]
		fn(tag, payload)
		if skip := nestedDescriptorOffset(tag, payload); skip >= 0 && skip <= len(payload) {
			if err := walkDescriptorsFrom(payload, skip, fn); err != nil {
				return err
			}
		}
		pos += length
	}
	return nil
}

// descriptorLength decodes an expandable length (up to four 7-bit groups)
// and returns the value plus the bytes it consumed.
func descriptorLength(b []byte, pos int) (int, int, error) {
	length := 0
	for count := 0; count < 4; count++ {
		if pos >= len(b) {
			return 0, 0, fmt.Errorf("descriptor length truncated")
		}
		c := b[pos]
		pos++
		length = length<<7 | int(c&0x7f)
		if c&0x80 == 0 {
			return length, count + 1, nil
		}
	}
	return 0, 0, fmt.Errorf("descriptor length longer than four bytes")
}

// nestedDescriptorOffset returns where, inside a container descriptor's
// payload, its nested descriptors begin — or -1 for leaf descriptors.
func nestedDescriptorOffset(tag byte, payload []byte) int {
	switch tag {
	case 0x03: // ES_Descriptor: ES_ID (2) + flags (1) + optionals
		if len(payload) < 3 {
			return len(payload)
		}
		flags := payload[2]
		pos := 3
		if flags&0x80 != 0 { // streamDependenceFlag
			pos += 2
		}
		if flags&0x40 != 0 { // URL_Flag: 1-byte length + string
			if pos >= len(payload) {
				return len(payload)
			}
			pos += 1 + int(payload[pos])
		}
		if flags&0x20 != 0 { // OCRstreamFlag
			pos += 2
		}
		return pos
	case 0x04: // DecoderConfigDescriptor: 13 fixed bytes
		const dcdFixed = 1 + 1 + 3 + 4 + 4
		if len(payload) < dcdFixed {
			return len(payload)
		}
		return dcdFixed
	default:
		return -1
	}
}

// NewMP4Demuxer wraps an open mp4 file into a demuxer. It parses the
// container eagerly so broken files fail at open, not mid-stream.
func NewMP4Demuxer(f *os.File) (*MP4Demuxer, error) {
	return openMP4(f)
}

// ReadFrame returns the next frame with its container PTS (90 kHz domain).
// Past the last sample it loops back to the first one — a local file is a
// standing programme, not a one-shot.
func (d *MP4Demuxer) ReadFrame(ctx context.Context) (model.ESFrame, error) {
	if d.closed {
		return model.ESFrame{}, io.EOF
	}
	if d.pos >= len(d.order) {
		d.pos = 0 // loop
	}
	ref := d.order[d.pos]
	d.pos++
	track := d.tracks[ref.track]
	sample := track.samples[ref.index]

	raw := make([]byte, sample.size)
	if _, err := d.f.ReadAt(raw, sample.offset); err != nil {
		return model.ESFrame{}, fmt.Errorf("media: read sample at %d: %w", sample.offset, err)
	}

	switch track.kind {
	case "video":
		// Parameter sets ride on the first video sample of each loop so
		// late joiners and looping decoders both start from a decodable
		// point. hvc1 carries no extracted sets (nil).
		var spsPps []byte
		if ref.index == 0 {
			spsPps = track.vpsSpsPps
		}
		return model.ESFrameWithPTSAndKind(track.videoPayload(raw, spsPps), sample.pts, model.ESFrameVideo), nil
	case "audio":
		return model.ESFrameWithPTSAndKind(track.adtsFrame(raw), sample.pts, model.ESFrameAudio), nil
	default:
		return model.ESFrame{}, fmt.Errorf("media: unknown track kind %q", track.kind)
	}
}

// videoPayload rewrites one AVCC/HVCC sample into Annex-B framing,
// optionally prepending Annex-B parameter sets.
func (t *mp4Track) videoPayload(raw, spsPps []byte) []byte {
	out := make([]byte, 0, len(raw)+len(spsPps)+8)
	out = append(out, spsPps...)
	pos := 0
	for pos+t.nalLenSize <= len(raw) {
		n := 0
		for i := 0; i < t.nalLenSize; i++ {
			n = n<<8 | int(raw[pos+i])
		}
		pos += t.nalLenSize
		if n <= 0 || pos+n > len(raw) {
			break // corrupt length prefix: keep what we have
		}
		out = append(out, annexBStartCode...)
		out = append(out, raw[pos:pos+n]...)
		pos += n
	}
	return out
}

// adtsFrame prefixes one raw AAC sample with a 7-byte ADTS header derived
// from the track's AudioSpecificConfig.
func (t *mp4Track) adtsFrame(aac []byte) []byte {
	h := adtsHeader(t.asc, len(aac)+7)
	return append(h, aac...)
}

// adtsHeader builds a 7-byte ADTS header from the AudioSpecificConfig:
// protection_absent=1 (no CRC), MPEG-4, layer 0.
func adtsHeader(asc []byte, frameLen int) []byte {
	if len(asc) < 2 {
		return nil
	}
	aot := int(asc[0]>>3) & 0x1f
	freqIdx := (int(asc[0]&0x07) << 1) | int(asc[1]>>7)
	channelCfg := int(asc[1]>>3) & 0x0f
	profile := aot - 1 // AAC object type minus one
	if profile < 0 {
		profile = 0
	}
	h := make([]byte, 7)
	h[0] = 0xFF
	h[1] = 0xF1 // MPEG-4, layer 0, protection_absent=1
	h[2] = byte(profile&0x03)<<6 | byte(freqIdx&0x0f)<<2 | byte(channelCfg>>2&0x01)
	h[3] = byte(channelCfg&0x03)<<6 | byte(frameLen>>11&0x03)
	h[4] = byte(frameLen >> 3 & 0xFF)
	h[5] = byte(frameLen&0x07)<<5 | 0x1F
	h[6] = 0xFC
	return h
}

// Close releases the underlying file. It is idempotent.
func (d *MP4Demuxer) Close() error {
	if d.closed {
		return nil
	}
	d.closed = true
	return d.f.Close()
}
