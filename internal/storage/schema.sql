-- GB28181 simulator schema bootstrap (Change 1).
--
-- This file intentionally contains only the `meta` table used by future
-- migrations. GB28181 domain tables (nodes, channels, sessions, alarms,
-- scenarios) will be appended here in Change 5+ via new migration files.

CREATE TABLE IF NOT EXISTS schema_meta (
    id          INTEGER PRIMARY KEY,
    version     TEXT    NOT NULL,
    applied_at  TEXT    NOT NULL
);

INSERT OR IGNORE INTO schema_meta (id, version, applied_at)
VALUES (1, '0.1.0', datetime('now'));