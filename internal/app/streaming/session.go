package streaming

import (
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// sessionKey uniquely identifies a streaming session.
type sessionKey struct {
	nodeID    model.NodeID
	channelID string
}

// session represents one active HTTP-FLV subscriber.
type session struct {
	key    sessionKey
	ch     chan<- []byte // unbuffered; back-pressure on HTTP writer
	done   <-chan struct{}
	cancel func()
}

// activeSessions tracks all active subscribers across the gateway.
type activeSessions struct {
	mu   sync.RWMutex
	sess map[sessionKey]*session
}

// newActiveSessions creates a fresh sessions tracker.
func newActiveSessions() *activeSessions {
	return &activeSessions{sess: make(map[sessionKey]*session)}
}

// Add registers a new session.
func (m *activeSessions) Add(key sessionKey, ch chan<- []byte, done <-chan struct{}, cancel func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sess[key] = &session{key: key, ch: ch, done: done, cancel: cancel}
}

// Get returns a session by key, or nil.
func (m *activeSessions) Get(key sessionKey) *session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sess[key]
}

// Remove unregisters a session by key and calls its cancel function.
func (m *activeSessions) Remove(key sessionKey) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sess[key]; ok {
		if s.cancel != nil {
			s.cancel()
		}
		delete(m.sess, key)
	}
}

// CountAll returns the total number of active sessions.
func (m *activeSessions) CountAll() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sess)
}

// AllKeys returns all current session keys.
func (m *activeSessions) AllKeys() []sessionKey {
	m.mu.RLock()
	defer m.mu.RUnlock()
	keys := make([]sessionKey, 0, len(m.sess))
	for k := range m.sess {
		keys = append(keys, k)
	}
	return keys
}

// ChannelCount returns the number of active subscribers for a node+channel.
// In the current model each Subscribe call = 1 subscriber.
func (m *activeSessions) ChannelCount(nodeID model.NodeID, channelID string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	k := sessionKey{nodeID: nodeID, channelID: channelID}
	if _, ok := m.sess[k]; ok {
		return 1
	}
	return 0
}

// IsKeyframe checks if a length-prefixed NALU is an IDR frame.
func IsKeyframeN(nalu []byte) bool {
	if len(nalu) < 5 {
		return false
	}
	return (nalu[4] & 0x1F) == 5
}

// IsSPSPPSN checks if a length-prefixed NALU is SPS or PPS.
func IsSPSPPSN(nalu []byte) bool {
	if len(nalu) < 5 {
		return false
	}
	t := nalu[4] & 0x1F
	return t == 7 || t == 8
}

// Unused var guard.
var _ = atomic.LoadInt32
var _ = slog.Default
