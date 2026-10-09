# Spec Delta: device-node (Modified)

## Purpose

Extend the existing `device-node` capability with multi-channel catalog behavior and channel-level SIP INVITE routing.

## ADDED Requirements

### Requirement: Multi-channel Catalog response
A device node with multiple channels in its `NodeProfile` MUST respond to a Catalog query with one `<Item>` per channel, each with the channel's own 20-digit DeviceID (not the parent device id).

#### Scenario: Three-channel device responds with three items
- **WHEN** a device node (id `34020000001320000001`) with channels `ch01` (`34020000001320000001`), `ch02` (`34020000001320000002`), `ch03` (`34020000001320000003`) receives a Catalog query
- **THEN** the response contains three `<Item>` elements, one per channel, each with the respective channel DeviceID

#### Scenario: Single-channel device responds with one item
- **WHEN** a device node without channels (fallback to device id as default channel) receives a Catalog query
- **THEN** the response contains one `<Item>` with the device id as DeviceID (backward compatible)

### Requirement: Channel-level SIP INVITE routing
When a device node receives a SIP INVITE for a specific channel (via `Channel-ID` parameter or route), it MUST route to the media source configured for that channel.

#### Scenario: INVITE with Channel-ID routes to channel media
- **WHEN** device receives INVITE with `Channel-ID: 34020000001320000002` and channel `ch02` has RTSP media source
- **THEN** the PS output corresponds to the RTSP source of `ch02`, not the node-level default

### Requirement: Channel status in Catalog
Each channel item in the Catalog response MUST include the channel's current status (ON/OFF).

#### Scenario: Channel status reflected in Catalog
- **WHEN** device channel `ch01` status is `ON` and `ch02` status is `OFF`
- **THEN** Catalog items reflect `Status` accordingly
