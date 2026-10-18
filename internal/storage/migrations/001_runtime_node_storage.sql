-- Migration 001: Runtime Node Storage
--
-- Node definitions move from the YAML `nodes:` section into SQLite so they
-- can be created, edited and deleted while the process runs. This file is
-- applied by storage.Bootstrap exactly once: the version row recording that
-- it ran is written by applyMigrations, not here, so a migration's own SQL
-- never has to know its own filename.
--
-- All three tables cascade off `nodes.id`: deleting a node definition drops
-- the registration and platform rows that hang off it, so a delete can never
-- leave an orphaned registration behind pointing at a node that is gone.

-- Node definitions: the identity plus everything needed to bind its
-- signalling listener. cascade_children is a JSON array of the ordered
-- downstream deviceIDs; cascade_parent is the upstream deviceID, empty at
-- the top of a topology.
CREATE TABLE IF NOT EXISTS nodes (
    id               TEXT    NOT NULL PRIMARY KEY,
    kind             TEXT    NOT NULL,
    domain           TEXT    NOT NULL,
    addr             TEXT    NOT NULL,
    vendor           TEXT    NOT NULL DEFAULT '',
    cascade_parent   TEXT    NOT NULL DEFAULT '',
    cascade_children TEXT    NOT NULL DEFAULT '[]',
    created_at       TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at       TEXT    NOT NULL DEFAULT (datetime('now'))
);

-- Registration targets, one row per node that registers upward. `server` and
-- `server_id` are redundant by design: GB/T 28181 carries the upstream's
-- signalling address in the URI and its 20-digit id separately, and a peer
-- that sends only one of them is common enough to store both rather than
-- require callers to reconstruct one from the other.
CREATE TABLE IF NOT EXISTS node_registrations (
    node_id                TEXT    NOT NULL PRIMARY KEY,
    server                 TEXT    NOT NULL,
    server_id              TEXT    NOT NULL DEFAULT '',
    username               TEXT    NOT NULL,
    password               TEXT    NOT NULL DEFAULT '',
    gb_version             TEXT    NOT NULL DEFAULT '2016',
    expires                INTEGER NOT NULL DEFAULT 3600,
    timeout_ms             INTEGER NOT NULL DEFAULT 30000,
    transport              TEXT    NOT NULL DEFAULT 'UDP',
    allow_no_auth          INTEGER NOT NULL DEFAULT 0,
    heartbeat_interval_ms  INTEGER NOT NULL DEFAULT 60000,
    heartbeat_timeout_ms   INTEGER NOT NULL DEFAULT 10000,
    heartbeat_max_failures INTEGER NOT NULL DEFAULT 3,
    created_at             TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at             TEXT    NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
);

-- Platform serving configuration, one row per platform-large or
-- platform-small. `realm` is empty when the node challenges in its own home
-- domain; min/default/max_expires are the lifetime window it grants.
-- `catalog_poll_interval_ms` of 0 disables the active catalog pull.
CREATE TABLE IF NOT EXISTS node_platforms (
    node_id                   TEXT    NOT NULL PRIMARY KEY,
    realm                     TEXT    NOT NULL DEFAULT '',
    allow_no_auth             INTEGER NOT NULL DEFAULT 0,
    min_expires               INTEGER NOT NULL DEFAULT 60,
    default_expires           INTEGER NOT NULL DEFAULT 3600,
    max_expires               INTEGER NOT NULL DEFAULT 86400,
    catalog_poll_interval_ms  INTEGER NOT NULL DEFAULT 0,
    created_at                TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at                TEXT    NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
);

-- The topology walk reads every node's cascade_parent, and the cascade
-- validation on PUT /v1/nodes/:id/cascade reads it again per hop; without
-- these the walk is a full table scan per hop.
CREATE INDEX IF NOT EXISTS idx_nodes_cascade_parent ON nodes(cascade_parent);
CREATE INDEX IF NOT EXISTS idx_nodes_kind ON nodes(kind);