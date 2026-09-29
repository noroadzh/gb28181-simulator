// Package capture — production store: per-node ring buffer + subscriber
// fan-out + PCAP synthesis.
package capture

import (
	"bytes"
	"encoding/binary"
	"log/slog"
	"sync"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"

	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// New returns a ready-to-use CaptureStore.
func New(log *slog.Logger) port.CaptureStore {
	return &store{defaultCap: 2048, log: withTag(log)}
}

// NewWithCapacity is a test seam: override the ring capacity (small values
// are useful for eviction tests).
func NewWithCapacity(cap int, log *slog.Logger) port.CaptureStore {
	if cap <= 0 {
		cap = 2048
	}
	return &store{defaultCap: cap, log: withTag(log)}
}

type store struct {
	defaultCap int
	mu         sync.RWMutex
	nodes      map[string]*ring
	// log is only used on cold paths (Subscribe, PCAP synthesis). The
	// Append fan-out loop stays silent by contract: the ring dropping an
	// event is the documented behaviour, not an error worth a log record.
	// Logging there would allocate per dropped event and flood the ring.
	// log is set in the constructors and never mutated afterwards, so the
	// unsynchronised read in the cold paths is safe.
	log *slog.Logger
}

// withTag returns log decorated with the capture subsystem tag, or the
// default logger when log is nil. A nil fallback keeps every test that
// passes nil working without an extra nil check at each call site.
func withTag(log *slog.Logger) *slog.Logger {
	if log == nil {
		log = slog.Default()
	}
	return log.With("component", "internal/adapter/capture", "subsystem", "capture_store")
}

type ring struct {
	mu      sync.Mutex
	events  []port.CaptureEvent
	cap     int
	head    int
	full    bool
	count   int
	subBufs map[chan<- port.CaptureEvent]int
}

func newRing(cap int) *ring {
	return &ring{
		cap:     cap,
		events:  make([]port.CaptureEvent, cap),
		subBufs: make(map[chan<- port.CaptureEvent]int),
	}
}

func (s *store) node(nodeID string) *ring {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.nodes == nil {
		s.nodes = make(map[string]*ring)
	}
	r, ok := s.nodes[nodeID]
	if !ok {
		r = newRing(s.defaultCap)
		s.nodes[nodeID] = r
	}
	return r
}

// Append implements port.CaptureStore. It drops the event when the ring is
// full rather than block; a silent drop keeps the transport's hot path
// allocation-free and bounded.
func (s *store) Append(nodeID string, evt port.CaptureEvent) {
	r := s.node(nodeID)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.full {
		// drop silently — do not evict the oldest, do not log, do not
		// block. Behaviour is the exact opposite of the web-subscription
		// hot path, which never blocks the writer.
		return
	}
	// Populate NodeID from the ring key as the authoritative source when
	// the transport did not tag it (backwards-compat / tests).
	if evt.NodeID == "" {
		evt.NodeID = nodeID
	}
	r.events[r.head] = evt
	r.head = (r.head + 1) % r.cap
	if r.head == 0 {
		r.full = true
	}
	r.count++
	// fan-out
	for ch, remaining := range r.subBufs {
		select {
		case ch <- evt:
			remaining--
			if remaining <= 0 {
				delete(r.subBufs, ch)
			} else {
				r.subBufs[ch] = remaining
			}
		default:
			// drop for this subscriber only
		}
	}
}

// Query implements port.CaptureStore. It never mutates the ring; the order
// of the returned slice is oldest-first among the returned subset.
func (s *store) Query(nodeID string, limit int) []port.CaptureEvent {
	s.mu.RLock()
	r, ok := s.nodes[nodeID]
	s.mu.RUnlock()
	if !ok || r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.count
	if n == 0 {
		return nil
	}
	if limit <= 0 || limit > n {
		limit = n
	}
	start := r.head - n
	if start < 0 {
		start += r.cap
	}
	out := make([]port.CaptureEvent, 0, limit)
	for i := 0; i < limit; i++ {
		idx := (start + i) % r.cap
		out = append(out, r.events[idx])
	}
	return out
}

// Subscribe implements port.CaptureStore. The channel buffer is the largest
// of [8, 2% of defaultCap], so a slow consumer is unlikely to block the
// writer and is dropped when it falls behind. Cancel closes the channel
// and releases the subscriber slot.
func (s *store) Subscribe(nodeID string) (<-chan port.CaptureEvent, func()) {
	r := s.node(nodeID)
	buf := 8
	if s.defaultCap > 0 {
		if b := s.defaultCap / 50; b > buf {
			buf = b
		}
	}
	ch := make(chan port.CaptureEvent, buf)
	r.mu.Lock()
	subscriberCount := len(r.subBufs) + 1
	r.subBufs[ch] = buf
	canceled := false
	r.mu.Unlock()
	if subscriberCount > 1 {
		s.log.Debug("capture subscriber added",
			"node_id", nodeID,
			"subscriber_count", subscriberCount,
			"buffer_size", buf)
	}
	cancel := func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if !canceled {
			canceled = true
			delete(r.subBufs, ch)
			close(ch)
		}
	}
	return ch, cancel
}

// PCAP implements port.CaptureStore using gopacket/pcapgo (pure Go).
func (s *store) PCAP(nodeID string) ([]byte, error) {
	s.mu.RLock()
	r, ok := s.nodes[nodeID]
	s.mu.RUnlock()
	if !ok || r == nil {
		s.log.Debug("capture pcap requested for unknown node",
			"node_id", nodeID)
		return nil, nil
	}
	r.mu.Lock()
	n := r.count
	r.mu.Unlock()
	events := s.Query(nodeID, n)
	data, err := encodePCAP(events)
	if err != nil {
		s.log.Debug("capture pcap encoding failed",
			"node_id", nodeID,
			"event_count", n,
			"err", err)
		return nil, err
	}
	s.log.Debug("capture pcap synthesised",
		"node_id", nodeID,
		"event_count", n,
		"bytes", len(data))
	return data, nil
}

func encodePCAP(events []port.CaptureEvent) ([]byte, error) {
	var buf bytes.Buffer
	w := pcapgo.NewWriter(&buf)
	if err := w.WriteFileHeader(65536, layers.LinkTypeEthernet); err != nil {
		return nil, err
	}
	for i := range events {
		pkt := serializeEthernet(events[i].Bytes)
		ci := gopacket.CaptureInfo{
			Length:        len(pkt),
			CaptureLength: len(pkt),
		}
		if events[i].At.IsZero() {
			ci.Timestamp = time.Now()
		} else {
			ci.Timestamp = events[i].At
		}
		if err := w.WritePacket(ci, pkt); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func serializeEthernet(payload []byte) []byte {
	// synthetic Ethernet II frame: type 0x0800 (IPv4), payload carries
	// the SIP payload verbatim. Source/destination MAC are the well-known
	// gopacket test patterns; they have no runtime meaning.
	frame := make([]byte, 14+20+8+len(payload))
	// dst MAC: 00:00:00:00:00:01
	frame[0], frame[1], frame[2], frame[3], frame[4], frame[5] = 0x00, 0x00, 0x00, 0x00, 0x00, 0x01
	// src MAC: 00:00:00:00:00:02
	frame[6], frame[7], frame[8], frame[9], frame[10], frame[11] = 0x00, 0x00, 0x00, 0x00, 0x00, 0x02
	// type: 0x0800
	frame[12], frame[13] = 0x08, 0x00
	// minimal IPv4 header with UDP: version/IHL=0x45, TTL=64, proto=UDP,
	// src=127.0.0.1, dst=127.0.0.2
	frame[14], frame[15], frame[16], frame[17] = 0x45, 0x00, 0x00, 0x00 // tot len filled below
	frame[18], frame[19], frame[20], frame[21] = 0x00, 0x00, 0x00, 0x00 // id+flags+offset
	frame[22], frame[23] = 0x40, 0x11                                   // TTL=64, proto=UDP
	frame[24], frame[25], frame[26], frame[27] = 0x7f, 0x00, 0x00, 0x01 // src
	frame[28], frame[29], frame[30], frame[31] = 0x7f, 0x00, 0x00, 0x02 // dst
	total := 20 + 8 + len(payload)
	binary.BigEndian.PutUint16(frame[16:18], uint16(total))
	// UDP ports 5060->5060
	frame[32], frame[33] = 0x13, 0x94 // src 5060
	frame[34], frame[35] = 0x13, 0x94 // dst 5060
	binary.BigEndian.PutUint16(frame[36:38], uint16(8+len(payload)))
	frame[38], frame[39] = 0x00, 0x00 // checksum placeholder
	copy(frame[40:], payload)
	return frame
}
