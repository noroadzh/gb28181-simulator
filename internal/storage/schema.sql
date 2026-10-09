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

-- platform-large 下级注册账号（change: channel-account-management-and-help-doc §1）
-- no_auth=1 表示免鉴权账号（空密码 + AllowNoAuth 模式），Lookup 重建 no-auth 标志
CREATE TABLE IF NOT EXISTS platform_accounts (
    node_id     TEXT    NOT NULL,
    username    TEXT    NOT NULL,
    password    TEXT    NOT NULL,
    no_auth     INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (node_id, username)
);

-- device 节点动态通道（运行时增删，落库持久化）
CREATE TABLE IF NOT EXISTS channels (
    node_id     TEXT    NOT NULL,
    channel_id  TEXT    NOT NULL,
    name        TEXT    NOT NULL,
    status      TEXT    NOT NULL DEFAULT 'ON',
    parent_id   TEXT    NOT NULL DEFAULT '',
    PRIMARY KEY (node_id, channel_id)
);

-- 通道级媒体源配置
CREATE TABLE IF NOT EXISTS channel_media (
    node_id     TEXT    NOT NULL,
    channel_id  TEXT    NOT NULL,
    kind        TEXT    NOT NULL,
    path        TEXT    NOT NULL DEFAULT '',
    loop        INTEGER NOT NULL DEFAULT 0,
    mtu         INTEGER NOT NULL DEFAULT 1400,
    fps         INTEGER NOT NULL DEFAULT 25,
    clock       INTEGER NOT NULL DEFAULT 90000,
    PRIMARY KEY (node_id, channel_id)
);

-- 节点级媒体源配置
CREATE TABLE IF NOT EXISTS node_media (
    node_id     TEXT    NOT NULL,
    kind        TEXT    NOT NULL,
    path        TEXT    NOT NULL DEFAULT '',
    loop        INTEGER NOT NULL DEFAULT 0,
    mtu         INTEGER NOT NULL DEFAULT 1400,
    fps         INTEGER NOT NULL DEFAULT 25,
    clock       INTEGER NOT NULL DEFAULT 90000,
    PRIMARY KEY (node_id)
);