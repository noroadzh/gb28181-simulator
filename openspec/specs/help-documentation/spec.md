# help-documentation Specification

## Purpose
在 Web UI 内置帮助文档页，向用户讲清系统定位（单进程多角色）、业务系统对接全流程、合壳 Web 的角色视角与常见操作指南。

## Requirements

### Requirement: Web UI 帮助文档页

系统 MUST 在 Web UI 提供一个"帮助"页面，内容覆盖：系统架构（同进程 platform-large / device 多角色与拓扑）、业务系统对接全流程（REGISTER 注册 → CATALOG 拉设备列表 → INVITE 预览 → RTP/RTSP 拉流）、**一套 Web UI**（默认 :18080）下顶栏「全部 / 平台 / 设备」视角分工、常见操作（添加通道、上传媒体、管理账号）指南。

#### Scenario: 侧边栏有帮助入口

- **WHEN** 用户打开 Web UI
- **THEN** 侧边栏菜单含"帮助"项，点击进入帮助文档页

#### Scenario: 帮助页含对接流程四步

- **WHEN** 用户进入帮助页
- **THEN** 页面以可视化的步骤呈现 REGISTER → CATALOG → INVITE → 拉流 四步对接流程，并说明每一步系统侧与业务侧的角色

#### Scenario: 帮助页含合壳角色视角说明

- **WHEN** 用户进入帮助页
- **THEN** 页面说明默认只部署一套 Web，顶栏平台视角用于账号/级联，设备视角用于通道/媒体源，并给出端口对照（HTTP :18080、平台 SIP :15060）

#### Scenario: 帮助页含常见操作指南

- **WHEN** 用户进入帮助页
- **THEN** 页面给出"添加子通道""上传 MP4 绑定媒体源""管理平台账号"的操作入口指引
