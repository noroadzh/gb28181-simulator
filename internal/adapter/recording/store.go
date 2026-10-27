// Package recording persists the PS stream a platform pulls into segment
// files, one per (node, channel) run, and replays them for query and
// playback.
//
// The filesystem is the index: a segment's node, channel and start time live
// in its file name, and a scan at startup rebuilds everything the process
// needs. No database, no migration.
package recording

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// segmentTimeFormat is the start-time spelling used in file names. It has no
// colons so the name is portable across the three target platforms.
const segmentTimeFormat = "20060102T150405"

// psStartCode marks the beginning of a program stream pack.
var psStartCode = []byte{0x00, 0x00, 0x01, 0xBA}

// Store is the filesystem-backed port.RecordingStore.
type Store struct {
	dir     string
	segment time.Duration

	mu      sync.Mutex
	entries map[string]port.Recording // by id
	active  map[string]*writer        // by node|channel
	now     func() time.Time
	closed  bool
}

// writer is the segment currently being appended to.
type writer struct {
	id      string
	path    string
	file    *os.File
	buf     *bufio.Writer
	size    int64
	started time.Time
}

// New opens (or creates) the recording directory and indexes what is already
// there. A zero segment duration falls back to five minutes.
func New(dir string, segment time.Duration) (*Store, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("recording: no directory")
	}
	if segment <= 0 {
		segment = 5 * time.Minute
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("recording: create %s: %w", dir, err)
	}
	s := &Store{
		dir:     dir,
		segment: segment,
		entries: make(map[string]port.Recording),
		active:  make(map[string]*writer),
		now:     time.Now,
	}
	if err := s.scan(); err != nil {
		return nil, err
	}
	return s, nil
}

// Dir reports where segments are written.
func (s *Store) Dir() string { return s.dir }

func channelKey(node model.NodeID, channelID string) string {
	return node.String() + "|" + channelID
}

// Append writes one PS frame to the channel's current segment. It creates a
// segment on first use and rolls one when the configured duration lapses.
func (s *Store) Append(_ context.Context, node model.NodeID, channelID string, ps model.PSFrame) error {
	if len(ps.Payload) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("recording: store is closed")
	}
	key := channelKey(node, channelID)
	w := s.active[key]
	now := s.now()
	if w == nil || now.Sub(w.started) >= s.segment {
		if w != nil {
			s.finishLocked(key, w)
		}
		var err error
		w, err = s.openLocked(node, channelID, now)
		if err != nil {
			return err
		}
		s.active[key] = w
	}
	n, err := w.buf.Write(ps.Payload)
	if err != nil {
		return fmt.Errorf("recording: write segment %s: %w", w.id, err)
	}
	// Flush per frame: a segment still being written must be readable
	// (query and playback of an active channel) and survive a crash.
	if err := w.buf.Flush(); err != nil {
		return fmt.Errorf("recording: flush segment %s: %w", w.id, err)
	}
	w.size += int64(n)
	entry := s.entries[w.id]
	entry.End = now
	entry.Size = w.size
	s.entries[w.id] = entry
	return nil
}

// openLocked creates a segment file and its index entry. Callers hold s.mu.
func (s *Store) openLocked(node model.NodeID, channelID string, now time.Time) (*writer, error) {
	base := node.String() + "_" + channelID + "_" + now.Format(segmentTimeFormat)
	id := base
	for i := 2; ; i++ {
		if _, ok := s.entries[id]; !ok {
			if _, statErr := os.Stat(filepath.Join(s.dir, id+".ps")); os.IsNotExist(statErr) {
				break
			}
		}
		id = fmt.Sprintf("%s-%d", base, i)
	}
	path := filepath.Join(s.dir, id+".ps")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("recording: create %s: %w", path, err)
	}
	w := &writer{id: id, path: path, file: f, buf: bufio.NewWriter(f), started: now}
	s.entries[id] = port.Recording{
		ID: id, NodeID: node, ChannelID: channelID,
		Start: now, End: now, Path: path,
	}
	return w, nil
}

// finishLocked flushes and closes one segment. Callers hold s.mu.
func (s *Store) finishLocked(key string, w *writer) {
	_ = w.buf.Flush()
	_ = w.file.Close()
	entry := s.entries[w.id]
	entry.End = s.now()
	entry.Size = w.size
	s.entries[w.id] = entry
	if s.active[key] == w {
		delete(s.active, key)
	}
}

// List returns the recordings of one channel that overlap [start, end],
// ordered by start time. A zero bound means unbounded.
func (s *Store) List(_ context.Context, node model.NodeID, channelID string, start, end time.Time) []port.Recording {
	s.mu.Lock()
	out := make([]port.Recording, 0, len(s.entries))
	for _, rec := range s.entries {
		if rec.NodeID != node || rec.ChannelID != channelID {
			continue
		}
		// A window and a segment overlap when neither ends before the
		// other starts; bounds are inclusive so a query for exactly one
		// segment's span returns it.
		if !start.IsZero() && rec.End.Before(start) {
			continue
		}
		if !end.IsZero() && rec.Start.After(end) {
			continue
		}
		out = append(out, rec)
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Start.Equal(out[j].Start) {
			return out[i].ID < out[j].ID
		}
		return out[i].Start.Before(out[j].Start)
	})
	return out
}

// Stream replays every recording of the channel overlapping [start, end] as
// one PS frame stream. The frames are the bytes as written: the store does
// not re-pack them.
func (s *Store) Stream(ctx context.Context, node model.NodeID, channelID string, start, end time.Time) (<-chan model.PSFrame, func(), error) {
	recs := s.List(ctx, node, channelID, start, end)
	if len(recs) == 0 {
		return nil, nil, fmt.Errorf("%w: node %s channel %s", port.ErrRecordingNotFound, node, channelID)
	}
	out := make(chan model.PSFrame, 16)
	stop := make(chan struct{})
	var once sync.Once
	cleanup := func() { once.Do(func() { close(stop) }) }

	go func() {
		defer close(out)
		for _, rec := range recs {
			if ctx.Err() != nil {
				return
			}
			if err := readSegment(ctx, stop, rec.Path, func(frame []byte) error {
				select {
				case out <- model.PSFrame{Payload: frame}:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				case <-stop:
					return errStopped
				}
			}); err != nil {
				return
			}
		}
	}()
	return out, cleanup, nil
}

// Finish closes one channel's open segment. It is called when a pull ends,
// so the next pull starts a fresh file instead of resuming a stale one.
func (s *Store) Finish(_ context.Context, node model.NodeID, channelID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := channelKey(node, channelID)
	if w := s.active[key]; w != nil {
		s.finishLocked(key, w)
	}
	return nil
}

// Close flushes and closes every open segment.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	for key, w := range s.active {
		s.finishLocked(key, w)
	}
	return nil
}

var errStopped = fmt.Errorf("recording: playback stopped")

// scan indexes the files already on disk. A crashed process leaves segments
// without a recorded end time; their mtime is the best bound available.
func (s *Store) scan() error {
	dirEntries, err := os.ReadDir(s.dir)
	if err != nil {
		return fmt.Errorf("recording: scan %s: %w", s.dir, err)
	}
	for _, de := range dirEntries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".ps") {
			continue
		}
		rec, ok := parseSegmentName(de.Name(), s.dir, s.segment)
		if !ok {
			continue
		}
		if info, statErr := de.Info(); statErr == nil {
			rec.Size = info.Size()
			if rec.End.IsZero() || info.ModTime().Before(rec.End) {
				if info.ModTime().After(rec.Start) {
					rec.End = info.ModTime()
				}
			}
		}
		s.entries[rec.ID] = rec
	}
	return nil
}

// parseSegmentName reads <node>_<channel>_<start>[-N].ps. Anything else is
// not ours and is ignored rather than treated as corruption.
func parseSegmentName(name, dir string, segment time.Duration) (port.Recording, bool) {
	base := strings.TrimSuffix(name, ".ps")
	parts := strings.SplitN(base, "_", 3)
	if len(parts) != 3 {
		return port.Recording{}, false
	}
	node, err := model.ParseNodeID(parts[0])
	if err != nil {
		return port.Recording{}, false
	}
	stamp := parts[2]
	if i := strings.Index(stamp, "-"); i > 0 {
		stamp = stamp[:i]
	}
	start, err := time.ParseInLocation(segmentTimeFormat, stamp, time.Local)
	if err != nil {
		return port.Recording{}, false
	}
	return port.Recording{
		ID: base, NodeID: node, ChannelID: parts[1],
		Start: start, End: start.Add(segment), Path: filepath.Join(dir, name),
	}, true
}

// readSegment splits one file into PS frames and hands each to emit. The
// split is on pack start codes: that is what a program stream offers as a
// boundary, and every frame this store writes begins with one.
func readSegment(ctx context.Context, stop <-chan struct{}, path string, emit func([]byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return splitPS(ctx, stop, f, emit)
}

func splitPS(ctx context.Context, stop <-chan struct{}, r io.Reader, emit func([]byte) error) error {
	buf := make([]byte, 0, 128<<10)
	tmp := make([]byte, 64<<10)
	started := false
	for {
		n, readErr := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		for {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			select {
			case <-stop:
				return errStopped
			default:
			}
			i := bytes.Index(buf, psStartCode)
			if i < 0 {
				if !started && len(buf) > len(psStartCode)-1 {
					// Keep a possible partial start code for the next read.
					buf = append(buf[:0], buf[len(buf)-(len(psStartCode)-1):]...)
				}
				break
			}
			if i > 0 {
				buf = buf[i:]
			}
			// The first aligned start code opens the first frame; the last
			// frame has no successor, so EOF flushes it below.
			started = true
			next := bytes.Index(buf[len(psStartCode):], psStartCode)
			if next < 0 {
				break
			}
			end := len(psStartCode) + next
			frame := make([]byte, end)
			copy(frame, buf[:end])
			if err := emit(frame); err != nil {
				if err == errStopped {
					return nil
				}
				return err
			}
			buf = append(buf[:0], buf[end:]...)
		}
		if readErr != nil {
			if readErr == io.EOF {
				if started && len(buf) > 0 {
					frame := make([]byte, len(buf))
					copy(frame, buf)
					return emit(frame)
				}
				return nil
			}
			return readErr
		}
	}
}
