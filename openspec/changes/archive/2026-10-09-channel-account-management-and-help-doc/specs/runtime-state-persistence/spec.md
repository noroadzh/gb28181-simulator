# Spec Delta

## Purpose

覆盖运行时状态（动态通道、通道媒体配置、节点级媒体源）的 SQLite 持久化与节点启动恢复，使运维人员在 Web UI 上做的运行时改动重启不丢。

## ADDED Requirements

### Requirement: 动态通道落库与启动恢复

系统 MUST 将运行时新增/删除的通道及其媒体配置持久化到 SQLite，并在节点启动时从库中恢复动态通道，与 YAML 静态通道合并后作为 profile 的通道来源。

#### Scenario: 新增通道重启后仍在

- **WHEN** 运行时新增通道 `34020000001320000099`，随后重启节点
- **THEN** 重启后 `Channels()` 仍包含该通道；YAML 静态通道与库内动态通道并存

#### Scenario: 删除通道重启后不恢复

- **WHEN** 运行时删除某通道，重启节点
- **THEN** 该通道不再出现（删除持久化生效）

#### Scenario: 库内与配置同 id 时库内优先

- **WHEN** YAML 声明了通道 A，SQLite 也存了运行时新增的通道 A（同 id 不同属性）
- **THEN** 以库内运行时状态为准，YAML 的同名静态条目不覆盖运行时改动

### Requirement: 通道媒体配置与节点级媒体源落库

系统 MUST 将每个通道的 MediaConfig（`channel_media` 表）与节点级 MediaConfig（`node_media` 表）持久化到 SQLite，并在启动时恢复为 profile 的对应配置。

#### Scenario: 通道媒体配置重启后恢复

- **WHEN** 给通道 A 配置 RTSP 媒体源，重启节点
- **THEN** 重启后 `ChannelMediaConfig("A")` 返回原配置

#### Scenario: 节点级媒体源重启后恢复

- **WHEN** 配置节点级 local_file 媒体源，重启节点
- **THEN** 重启后 `MediaConfigForChannel` 回退到节点级时返回原配置

#### Scenario: 清空媒体源后重启不恢复

- **WHEN** 清空某通道媒体配置，重启节点
- **THEN** 该通道没有显式配置（回退到节点级或默认）
