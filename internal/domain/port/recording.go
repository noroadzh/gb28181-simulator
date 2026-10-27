// Package port — platform recording.
package port

import (
	"context"
	"errors"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

var (
	// ErrRecordingUnsupported signals that recording is not wired or is
	// switched off. HTTP callers map it to 501.
	ErrRecordingUnsupported = errors.New("port: recording unsupported")

	// ErrRecordingNotFound signals that the requested recording does not
	// exist for that node and channel. HTTP callers map it to 404.
	ErrRecordingNotFound = errors.New("port: recording not found")
)

// Recording is one finished or in-progress segment of a platform's pulled
// stream.
type Recording struct {
	ID        string
	NodeID    model.NodeID
	ChannelID string
	Start     time.Time
	End       time.Time
	Size      int64
	Path      string
}

// RecordingStore persists the PS frames a platform pulls and serves them
// back for query and playback.
type RecordingStore interface {
	// Append writes one PS frame into the channel's current segment,
	// rolling to a new segment when the configured duration has elapsed.
	Append(ctx context.Context, node model.NodeID, channelID string, ps model.PSFrame) error

	// List returns the recordings of one channel that overlap [start, end],
	// ordered by start time. A zero bound means "unbounded".
	List(ctx context.Context, node model.NodeID, channelID string, start, end time.Time) []Recording

	// Stream replays the recordings that overlap [start, end] as a PS frame
	// stream. The cleanup must be called when the consumer stops early.
	Stream(ctx context.Context, node model.NodeID, channelID string, start, end time.Time) (<-chan model.PSFrame, func(), error)

	// Finish closes the channel's open segment, if any. A later Append
	// starts a new one. It is the natural end of a pull.
	Finish(ctx context.Context, node model.NodeID, channelID string) error

	// Close flushes and closes any open segment.
	Close() error
}
