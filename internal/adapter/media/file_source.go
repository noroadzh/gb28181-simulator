package media

import (
	"context"
	"io"
	"os"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// FileSource reads an elementary-stream byte stream from a local file.
type FileSource struct {
	config model.MediaConfig
	file   *os.File
}

// NewFileSource builds a file-backed MediaSource.
func NewFileSource(config model.MediaConfig) *FileSource {
	return &FileSource{config: config}
}

// Open opens the configured file for reading.
func (f *FileSource) Open(ctx context.Context) (io.ReadCloser, error) {
	file, err := os.Open(f.config.Path)
	if err != nil {
		return nil, err
	}
	f.file = file
	return file, nil
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
