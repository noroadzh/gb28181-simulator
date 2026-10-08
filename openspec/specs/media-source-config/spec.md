# media-source-config Specification

## Purpose
Complete the media-source end-to-end path: configuration → NodeProfile → Acceptor wiring → HTTP API.
This is the delta against the already-delivered `media-sources` spec, which covers the adapter layer only.

## Requirements

### Requirement: NodeProfile carries a per-node MediaConfig

The `NodeProfile` MUST expose methods to get and set a `MediaConfig`.
When a node is created from config, its `MediaConfig` is initialized from the `nodes[].media:` section.
An empty config means the node has no configured source.

#### Scenario: Node created from config with media section

- **GIVEN** a `NodeConfig` whose `media.kind = "file"` and `media.path = "/data/sample.ps"`
- **WHEN** a `NodeProfile` is created via `NewNodeProfileFromConfig`
- **THEN** `profile.MediaConfig()` returns `(MediaConfig{Kind: SourceKindFile, Path: "/data/sample.ps"}, true)`

#### Scenario: Node created from config without media section

- **GIVEN** a `NodeConfig` without a `media:` section
- **WHEN** a `NodeProfile` is created via `NewNodeProfileFromConfig`
- **THEN** `profile.MediaConfig()` returns `(MediaConfig{}, false)`

#### Scenario: MediaConfig is cleared via SetMedia

- **GIVEN** a node whose profile has a non-empty `MediaConfig`
- **WHEN** `profile.SetMediaConfig(MediaConfig{})` is called with an empty config
- **THEN** `profile.MediaConfig()` returns `(MediaConfig{}, false)`

---

### Requirement: HTTP API exposes per-node media source operations

Three endpoints MUST exist per node:

| Method | Path | Body | Response |
|--------|------|------|----------|
| GET | `/v1/nodes/:id/media` | — | `200 {"kind":"file","path":"…",…}` or `204` |
| PUT | `/v1/nodes/:id/media` | `MediaConfig` JSON | `200` |
| DELETE | `/v1/nodes/:id/media` | — | `204` |

`MediaConfig` JSON shape mirrors the `model.MediaConfig` struct:
`kind` (required), `path` (required for file/rtsp/hls), `loop`, `mtu`, `ssrc`, `clock`, `fps`.

#### Scenario: GET returns 204 when no media configured

- **WHEN** a `GET /v1/nodes/:id/media` request is made for a node without a media source
- **THEN** the server responds `204 No Content` with an empty body

#### Scenario: PUT validates MediaConfig

- **WHEN** a `PUT /v1/nodes/:id/media` request carries a body `{"kind":"file","path":""}`
- **THEN** the server responds `400 Bad Request` with `{"error":"model: MediaConfig.Path is empty for kind \"file\""}`

#### Scenario: PUT replaces existing MediaConfig

- **GIVEN** a node whose profile has `MediaConfig{Kind: SourceKindFile, Path: "/a.ps"}`
- **WHEN** a `PUT /v1/nodes/:id/media` request carries `{"kind":"rtsp","path":"rtsp://x/live"}`
- **THEN** the server responds `200` and subsequent `GET` returns the RTSP config

#### Scenario: DELETE clears MediaConfig

- **WHEN** a `DELETE /v1/nodes/:id/media` request is made for a node with a configured source
- **THEN** the server responds `204` and subsequent `GET` returns `204`

---

### Requirement: FileSource loops when Loop is true

When a `FileSource` is opened with `MediaConfig.Loop == true` and the underlying file reaches EOF,
the source MUST re-open the file and continue yielding ES frames from the beginning,
repeating indefinitely until `Close()` is called.

#### Scenario: Loop enabled — frames repeat after EOF

- **GIVEN** a FileSource opened with `Loop: true` on a 10-frame PS file
- **WHEN** frames are read past the EOF boundary
- **THEN** the source yields frames starting from the first frame again
- **AND** the PTS continues from where it left off (no reset)

#### Scenario: Loop disabled — stops at EOF

- **GIVEN** a FileSource opened with `Loop: false`
- **WHEN** all bytes are consumed
- **THEN** subsequent reads return `io.EOF`
