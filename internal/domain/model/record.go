// Package model — Record.
package model

import (
	"fmt"
	"time"
)

// RecordItem describes one recording available for playback.
type RecordItem struct {
	DeviceID  string
	ChannelID string
	Start     time.Time
	End       time.Time
	Format    string // "PS", "MP4", etc.
	FilePath  string // absolute path on the storage device
	FileSize  int64  // bytes; 0 when unknown
}

// String renders a log-safe summary.
func (r RecordItem) String() string {
	return fmt.Sprintf("Record<device=%s channel=%s start=%s end=%s path=%s size=%d>",
		r.DeviceID, r.ChannelID, r.Start.Format("2006-01-02 15:04:05"), r.End.Format("2006-01-02 15:04:05"), r.FilePath, r.FileSize)
}
