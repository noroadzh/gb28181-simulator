# Spec Delta: dynamic-catalog-alarm-and-playback

## Purpose
Defines runtime-dynamic behaviors for the simulator: channel list mutations, alarm notification emission, record query/playback control, and mobile position reporting via extended MediaStatus.

## ADDED Requirements

### Requirement: dynamic-catalog-update

A device node's channel list MUST be updatable at runtime. A subsequent Catalog query MUST reflect added/removed channels and each channel's online/offline status without restarting the node.

#### Scenario: channel goes offline
- **WHEN** an operator sets channel 1 status to offline via the runtime API
- **THEN** the next Catalog query response omits channel 1 or marks it with `Status=OFF`

### Requirement: alarm-notification-emission

A device node MUST be able to emit an Alarm notify MANSCDP message to its parent platform at runtime. The message MUST contain Alarm priority, time, and description.

#### Scenario: proactive alarm push
- **WHEN** a device triggers an alarm event through the runtime API
- **THEN** the device sends a MANSCDP MESSAGE with `CmdType=Alarm` to its registered parent, and the parent responds with `200 OK`

### Requirement: record-query-response

A device node MUST respond to a RecordInfo query with a list of RecordItem entries (start time, end time, file path/size) for the requested channel and time range.

#### Scenario: record list fetch
- **WHEN** a platform queries RecordInfo for channel 1 on device 34020000001320000001 for the last hour
- **THEN** the device returns a MANSCDP response containing at least one RecordItem covering that window

### Requirement: playback-control-handling

A device node MUST accept a PlaybackControl request (start/stop) and respond with `200 OK` on valid parameters, or `400 Bad Request` on invalid parameters.

#### Scenario: start playback
- **WHEN** a platform sends PlaybackControl with Start, valid channel, and time range
- **THEN** the device responds `200 OK`; subsequent MediaStatus may indicate playback session state

### Requirement: mobile-position-reporting

A device node MUST include optional longitude, latitude, and speed fields in its MediaStatus notify when these fields are configured.

#### Scenario: position update
- **WHEN** a device has position configured (longitude 121.47, latitude 31.23)
- **THEN** its next MediaStatus notify contains `<Longitude>121.47</Longitude>` and `<Latitude>31.23</Latitude>`

### Requirement: runtime-trigger-api

The simulator MUST expose an HTTP API to trigger dynamic behaviors: push alarm, toggle channel status, and update device position.

#### Scenario: API-driven alarm
- **WHEN** a test script POSTs `/nodes/{id}/alarm` with priority and description
- **THEN** the device emits an Alarm notify to its parent and returns the generated event ID
