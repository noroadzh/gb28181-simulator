# Spec Delta: media-source-config (Modified)

## Purpose

Extend the existing `media-source-config` capability to support per-channel media source configuration, while maintaining backward compatibility when no channel-level configuration is present.

## ADDED Requirements

### Requirement: Channel-level MediaConfig
`NodeProfile` MUST support a `ByChannel map[string]MediaConfig` field. When looking up the media source for a given channel id:
- If `ByChannel` contains an entry for that channel id, use it
- If `ByChannel` is empty or lacks the channel id, fall back to the node-level `MediaConfig`

#### Scenario: Per-channel media source overrides node-level
- **WHEN** node has node-level `MediaConfig` as file `/a.mp4` and `ByChannel` maps `ch01` to RTSP `rtsp://x/live`
- **THEN** channel `ch01` resolves to RTSP; other channels resolve to `/a.mp4`

#### Scenario: Empty ByChannel falls back to node-level
- **WHEN** node has `MediaConfig` set and `ByChannel` is nil or empty
- **THEN** all channels resolve to the node-level `MediaConfig` (backward compatible)

#### Scenario: Channel not in ByChannel falls back to node-level
- **WHEN** `ByChannel` maps only `ch01` but channel `ch02` is queried
- **THEN** `ch02` resolves to node-level `MediaConfig`

### Requirement: HTTP API channel-level media operations
The existing node-level endpoints MUST also support channel-level operations:

| Method | Path | Body | Response |
|--------|------|------|----------|
| GET | `/v1/nodes/:id/channels/:ch/media` | — | `200 MediaConfig` or `204` |
| PUT | `/v1/nodes/:id/channels/:ch/media` | `MediaConfig` JSON | `200` |
| DELETE | `/v1/nodes/:id/channels/:ch/media` | — | `204` |

#### Scenario: Set channel-level media source
- **WHEN** PUT `/v1/nodes/{device}/channels/{ch}/media` body=`{kind:"file",path:"/tmp/video.mp4"}`
- **THEN** 200; subsequent channel-level media queries return this config
