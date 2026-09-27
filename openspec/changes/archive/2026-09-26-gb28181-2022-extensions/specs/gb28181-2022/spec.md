# Spec Delta

## ADDED Requirements

### Requirement: x-gb-ver negotiation semantics

A platform node (platform-large / platform-small) that receives a REGISTER
carrying an `X-GB-Ver` header MUST record the declared version as part of the
downstream device's registration state and MUST echo the negotiated version
in its `200 OK` response. A REGISTER without the header MUST be treated as a
2016-class peer: no version is recorded and no `X-GB-Ver` is echoed.

#### Scenario: 2022 device registers and receives the version echo

- **WHEN** a device sends a REGISTER with `X-GB-Ver: 2022` and valid
  credentials
- **THEN** the platform answers `200 OK` carrying `X-GB-Ver: 2022`
- **AND** the downstream device record exposes `GBVersion == "2022"`

#### Scenario: 2016 device registers without version negotiation

- **WHEN** a device sends a REGISTER without an `X-GB-Ver` header
- **THEN** the `200 OK` response carries no `X-GB-Ver` header
- **AND** the downstream device record exposes an empty `GBVersion`

#### Scenario: re-registration updates the recorded version

- **WHEN** a device first registers with `X-GB-Ver: 2016` and later
  re-registers with `X-GB-Ver: 2022`
- **THEN** the stored device record reflects `GBVersion == "2022"` after the
  second registration

### Requirement: version-gated behaviour

2022-only wire behaviour (extra response fields, 2022 command replies, SDP
capability modules) MUST be emitted only to peers whose recorded version is
`2022`. A 2016 peer MUST receive responses that are byte-identical to the
pre-2022 behaviour.

#### Scenario: 2016 peer never sees 2022 fields

- **WHEN** a device registered without `X-GB-Ver: 2022` is queried for
  DeviceStatus
- **THEN** the response contains no storage-card or other 2022-only fields

#### Scenario: 2022 peer receives the extended answer

- **WHEN** a device registered with `X-GB-Ver: 2022` is queried for
  DeviceStatus and the profile declares storage-card state
- **THEN** the response includes the 2022 storage-card status field

### Requirement: home-position query and set

A device node MUST answer a MANSCDP `HomePosition` query with the configured
guard position (longitude, latitude, altitude, azimuth) and MUST accept a
`HomePosition` set command, acknowledging with `OK` when the parameters are
valid and `ERROR` when they are malformed.

#### Scenario: query returns the configured guard position

- **WHEN** the platform sends a HomePosition query to a device whose profile
  defines longitude 121.47 / latitude 31.23
- **THEN** the device responds with a HomePosition response containing those
  values

#### Scenario: set command updates the in-memory guard position

- **WHEN** the platform sends a HomePosition set with valid coordinates
- **THEN** the device answers `OK` and a subsequent query returns the new
  values

### Requirement: cruise-track-list query

A device node MUST answer a `CruiseTrackList` query with the configured list
of cruise tracks (identifier, name, waypoint count) for the requested
channel.

#### Scenario: cruise roster returned

- **WHEN** the platform queries CruiseTrackList for a channel whose profile
  defines two cruise tracks
- **THEN** the response lists both tracks with their identifiers and names

### Requirement: snapshot command

A device node MUST accept a `SnapShot` command, respond `OK`, record a
capture record (device id, channel, capture time), and expose the record so
operators can retrieve it. The simulator does not transfer image binaries.

#### Scenario: snapshot acknowledged and recorded

- **WHEN** the platform sends a SnapShot command for channel 1
- **THEN** the device answers `OK` and a capture record with the current time
  is stored on the node

### Requirement: storage-card status in DeviceStatus

A device node registered as 2022 MUST include storage-card state (normal /
error / absent) in its `DeviceStatus` response when the profile declares it.
2016 peers MUST NOT receive the field.

#### Scenario: storage card normal

- **WHEN** a 2022 peer queries DeviceStatus and the profile declares a normal
  storage card
- **THEN** the response carries the storage-card status `Normal`

### Requirement: teleboot soft reboot

A device node MUST accept a `DeviceControl` command whose `TeleBoot` element
is present, log the reboot request, answer `OK`, and transition its
registration state machine through a simulated restart without tearing down
the process.

#### Scenario: teleboot acknowledged

- **WHEN** the platform sends DeviceControl with TeleBoot
- **THEN** the device answers `OK` and logs the reboot request

### Requirement: precise PTZ position

A device node registered as 2022 MUST include precise PTZ position values
(azimuth, elevation, zoom with sub-degree precision) in position/status
responses when the profile declares them.

#### Scenario: precise position reported

- **WHEN** a 2022 peer requests the position of a PTZ device whose profile
  declares azimuth 121.4731
- **THEN** the response contains the precise azimuth value

### Requirement: annex O catalog section type

A device node MAY include the Annex O capture-section type attribute on
catalog items when the profile configures it. The attribute MUST be omitted
for peers that did not negotiate 2022.

#### Scenario: section type emitted for 2022 peer

- **WHEN** a 2022 peer queries the catalog of a device whose channel declares
  section type `23` (crossroads)
- **THEN** the catalog item carries the section-type attribute `23`

#### Scenario: section type omitted for 2016 peer

- **WHEN** a 2016 peer queries the same catalog
- **THEN** the catalog item carries no section-type attribute
