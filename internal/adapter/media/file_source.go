package media

import (
	"context"
	"io"
	"os"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// FileSource reads an elementary-stream byte stream from a local file.
// When the config's Loop flag is set the source rewinds and re-opens the
// file when EOF is reached, so a single Open call produces an indefinite
// stream.
type FileSource struct {
	config model.MediaConfig
	file   *os.File
}

// NewFileSource builds a file-backed MediaSource.
func NewFileSource(config model.MediaConfig) *FileSource {
	return &FileSource{config: config}
}

// openOne returns the raw reader for a single pass through the file.
// It detects MP4 containers via "ftyp" magic and returns an mp4ReadCloser
// in that case, a plain *os.File otherwise.
func (f *FileSource) openOne(ctx context.Context) (io.ReadCloser, error) {
	file, err := os.Open(f.config.Path)
	if err != nil {
		return nil, err
	}
	f.file = file
	head := make([]byte, 8)
	if n, _ := file.ReadAt(head, 0); n >= 8 && string(head[4:8]) == "ftyp" {
		demuxer, err := NewMP4Demuxer(file)
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		return &mp4ReadCloser{d: demuxer}, nil
	}
	return file, nil
}

// Open opens the configured file for reading. Container-sniffing happens
// here: a file whose bytes 4..8 read "ftyp" is an mp4 and comes back as an
// MP4Demuxer reader (Annex-B / ADTS frames with container PTS); everything
// else is treated as a raw elementary stream, byte-identical to before.
// When config.Loop is true the returned ReadCloser loops forever: when Read
// hits EOF it re-opens the file and continues. Only the first Open call on
// the FileSource is valid; subsequent calls return an error.
func (f *FileSource) Open(ctx context.Context) (io.ReadCloser, error) {
	first, err := f.openOne(ctx)
	if err != nil {
		return nil, err
	}
	if !f.config.Loop {
		return first, nil
	}
	return &loopingFileReader{fs: f, rc: first}, nil
}

// Close releases the underlying file handle.
func (f *FileSource) Close() error {
	if f.file != nil {
		return f.file.Close()
	}
	return nil
}

// Config returns the source configuration.
func (f *FileSource) Config() model.MediaConfig {
	return f.config
}

// loopingFileReader wraps a ReadCloser returned by FileSource so that EOF
// re-opens the underlying file. It exists so a single INVITE can drive a
// long-lived PS stream without the caller having to manage rewinds. The
// wrapped file is closed and re-opened transparently on each loop; any
// non-EOF error is propagated to the caller and the loop terminates.
//
// Design: the loop boundary is placed at the Read call boundary, never
// inside a single call. When Read hits EOF with no data in *this* call
// it rewinds and re-opens; any partial data returned alongside EOF is
// surfaced as (n, io.EOF) per io.Reader's contract, and the next call
// starts the new pass. This avoids recursion, keeps every Read
// single-pass, and matches what io.ReadAll and well-behaved pipelines
// expect from a streaming source.
type loopingFileReader struct {
	fs *FileSource
	rc io.ReadCloser
}

func (l *loopingFileReader) Read(p []byte) (int, error) {
	if l.rc == nil {
		return 0, io.EOF
	}
	n, err := l.rc.Read(p)
	if err == io.EOF && n == 0 {
		// End of a full pass with no data buffered in this call: close
		// the spent reader and open a fresh one. The fresh mp4ReadCloser
		// (if any) re-parses the container from the start of the file,
		// so PTS and frame ordering restart cleanly.
		_ = l.rc.Close()
		next, oerr := l.fs.openOne(context.Background())
		if oerr != nil {
			l.rc = nil
			return 0, oerr
		}
		l.rc = next
		// The new reader may also be empty; let the next Read surface
		// that (0, io.EOF) naturally rather than recursing.
		return 0, nil
	}
	return n, err
}

func (l *loopingFileReader) Close() error {
	if l.rc == nil {
		return nil
	}
	return l.rc.Close()
}
