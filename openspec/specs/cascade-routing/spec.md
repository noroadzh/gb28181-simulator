# Spec: cascade-routing

## Purpose
Defines the behavior contract for GB/T 28181 cascade path tracking and forwarding: how X-RoutePath and X-PreferredPath headers are injected, forwarded, and selected across arbitrary multi-level topologies in the simulator.

## Requirements

### Requirement: cascade-header-injection

When a Node forwards a SIP message to a downstream cascade neighbor, it MUST prepend its own deviceID (or platformID) to the `X-RoutePath` header, creating a comma-separated path trace (e.g., `X-RoutePath: 34020000001320000001,34020000001320000002`).

#### Scenario: two-hop cascade injection

- **WHEN** platform-large (34020000001320000001) forwards a message to platform-small (34020000001320000002), which forwards to device (34020000001320000003)
- **THEN** the SIP message received by the device carries `X-RoutePath: 34020000001320000001,34020000001320000002`

### Requirement: preferred-path-selection

When a node receives a request with an `X-PreferredPath` header, it MUST strip the leading (nearest) route segment and forward the request to the next listed deviceID. If no routes remain, the request is forwarded to the final destination without the header.

#### Scenario: preferred path routing

- **WHEN** a message arrives with `X-PreferredPath: 34020000001320000002,34020000001320000003` and the current node is 34020000001320000001
- **THEN** the current node forwards to 34020000001320000002, stripping its own ID from the header before forwarding

### Requirement: cascade-forwarding

When a node receives a MANSCDP MESSAGE for a downstream neighbor and the node has a `CascadeParent` configured, the node MUST forward the message to the parent without altering the body, preserving all X-RoutePath and X-PreferredPath headers.

#### Scenario: upstream alarm propagation

- **WHEN** a device sends an Alarm notify to its local platform-small, and the platform-small has CascadeParent set to a platform-large
- **THEN** the platform-small forwards the Alarm notify upstream to the platform-large, including the original Alarm body and all cascade headers

### Requirement: multi-node-isolation

Each Node instance in the simulator MUST maintain its own independent SIP stack, port allocation, and node state. Two nodes on the same host MUST NOT share transport resources, MUST NOT share dialog state, and MUST be addressable by distinct DeviceID/PlatformID.

#### Scenario: concurrent multi-node operation

- **WHEN** the simulator starts a platform-large (ID 34020000001320000001, port 5060) and a platform-large (ID 34020000001320000003, port 5062) simultaneously
- **THEN** each node receives and processes its own inbound SIP traffic independently; REGISTER from ID 1 never appears on node ID 3

### Requirement: loop-prevention

If a message arrives with `X-RoutePath` that already contains the current node's own ID, the node MUST NOT forward it further and MUST return a `482 Loop Detected` response (or silently drop and log for simulation mode).

#### Scenario: circular cascade detection

- **WHEN** a node (34020000001320000002) receives a message with `X-RoutePath: ...,34020000001320000002,...`
- **THEN** the node detects the loop and stops forwarding, logging the event
