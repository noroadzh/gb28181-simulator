# Spec Delta

## Purpose

Extends the existing `media-sources` capability with GB/T 28181-2022 SDP
media-capability declaration: a node profile may declare H.265, AAC and
G.722.1 capability modules, and the SDP generated for INVITE / 200 OK
includes the corresponding `a=rtpmap` lines only when the module is enabled
and only toward peers that negotiated version 2022.

## ADDED Requirements

### Requirement: SDP capability modules declared on demand

SDP generated for an INVITE or its `200 OK` MUST include `a=rtpmap` entries
for H.265 (payload 100), AAC (payload 97) and G.722.1 (payload 99) exactly
when the node profile enables the corresponding capability module and the
peer negotiated GB/T 28181-2022. Capability modules are disabled by default;
a profile without explicit module configuration MUST produce today's SDP
unchanged.

#### Scenario: H.265 module enabled for a 2022 peer

- **WHEN** a device node with the H.265 module enabled answers an INVITE
  from a 2022 peer
- **THEN** the SDP contains `a=rtpmap:100 H265/90000`

#### Scenario: module enabled but peer is 2016

- **WHEN** the same node answers an INVITE from a peer that registered
  without `X-GB-Ver: 2022`
- **THEN** the SDP contains no H.265 rtpmap line

#### Scenario: modules default to off

- **WHEN** a profile declares no capability modules and a 2022 peer sends an
  INVITE
- **THEN** the SDP is byte-identical to the pre-2022 generation
