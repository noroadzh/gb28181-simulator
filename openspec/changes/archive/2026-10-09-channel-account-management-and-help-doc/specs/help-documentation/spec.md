# Spec Delta

## Purpose

在 Web UI 内置帮助文档页，向用户讲清系统定位（平台 + 设备双角色）、业务系统对接全流程、两个 Web UI 的使用分工与常见操作指南。

## ADDED Requirements

### Requirement: Web UI 帮助文档页

系统 MUST 在 Web UI 提供一个"帮助"页面，内容覆盖：系统架构（platform-large / device 双角色与拓扑）、业务系统对接全流程（REGISTER 注册 → CATALOG 拉设备列表 → INVITE 预览 → RTP/RTSP 拉流）、两个 Web UI（平台 18080 / 设备 18081）的使用分工、常见操作（添加通道、上传媒体、管理账号）指南。

#### Scenario: 侧边栏有帮助入口

- **WHEN** 用户打开 Web UI
- **THEN** 侧边栏菜单含"帮助"项，点击进入帮助文档页

#### Scenario: 帮助页含对接流程四步

- **WHEN** 用户进入帮助页
- **THEN** 页面以可视化的步骤呈现 REGISTER → CATALOG → INVITE → 拉流 四步对接流程，并说明每一步系统侧与业务侧的角色

#### Scenario: 帮助页含双 Web UI 分工

- **WHEN** 用户进入帮助页
- **THEN** 页面说明 platform Web UI 用于账号管理、device Web UI 用于通道/媒体源管理，并给出端口对照

#### Scenario: 帮助页含常见操作指南

- **WHEN** 用户进入帮助页
- **THEN** 页面给出"添加子通道""上传 MP4 绑定媒体源""管理平台账号"的操作入口指引
