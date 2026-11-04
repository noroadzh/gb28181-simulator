// Package port — cascade media relay observability.
package port

import "time"

// CascadeSession is one upstream↔downstream media relay visible to operators.
type CascadeSession struct {
	UpstreamCallID string    `json:"upstream_call_id"`
	ChannelID      string    `json:"channel_id"`
	Peer           string    `json:"peer"`
	Transport      string    `json:"transport"`
	LocalPort      int       `json:"local_port"`
	StartedAt      time.Time `json:"started_at"`
}
