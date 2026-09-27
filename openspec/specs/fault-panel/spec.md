# fault-panel 规范

## Purpose

Web 管理界面中的故障注入面板能力。本 capability 覆盖 canned 映射、延迟、丢包、blackhole 与 unsupported-method 的表单编辑，安装/清除故障 profile 的 REST API，以及面板与后端故障计数、节点状态的实时一致性。

## Requirements

### Requirement: 故障注入面板

故障注入面板必须（MUST）允许用户为目标节点编辑故障 profile（canned 方法映射、delay 基础时长与抖动、drop 概率、blackhole 方法列表、unsupported-method 501 开关），并通过 `POST /v1/nodes/:id/faults` 提交。面板必须（MUST）展示当前已安装的 profile 详情与异常事件计数，并提供调用 `DELETE /v1/nodes/:id/faults` 的清除按钮。

#### Scenario: 安装 canned 403 故障

- **WHEN** 用户在面板为平台节点配置 REGISTER→403 的 canned 映射并提交
- **THEN** 面板展示当前 profile 内容，后续设备注册在面板的故障计数中体现 canned_response 增加

#### Scenario: 清除故障恢复计数

- **WHEN** 用户点击清除按钮删除 profile
- **THEN** 节点详情中的故障计数归零，后续请求恢复正常处理

### Requirement: 非法输入前端校验

面板必须（MUST）在前端校验 profile 参数（状态码 400–699、drop ∈ [0,1]、非负时长），非法输入被拒绝并提示，不发起到后端的请求。

#### Scenario: drop 概率越界被拦截

- **WHEN** 用户填写 drop 概率为 1.5 并提交
- **THEN** 前端提示"丢弃概率必须在 [0,1] 区间内"，且未发出 POST 请求
