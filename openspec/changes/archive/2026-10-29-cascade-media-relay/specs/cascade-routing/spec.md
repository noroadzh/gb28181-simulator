# cascade-routing Delta

## ADDED Requirements

### Requirement: 媒体转发与 MESSAGE 转发并存

级联节点对 MANSCDP `MESSAGE` 的转发规则 MUST 保持不变；新增的媒体中继 MUST 只在 INVITE 上生效。平台 MUST NOT 因媒体中继而改变 `X-RoutePath` / `X-PreferredPath` 在 MESSAGE 上的处理，也 MUST NOT 把 INVITE 交给 MESSAGE 的转发表。

#### Scenario: MESSAGE 转发不受影响

- **WHEN** 平台配置了级联下级并收到指向该下级的 MANSCDP MESSAGE
- **THEN** 报文按既有规则逐跳转发，路径头语义不变

#### Scenario: INVITE 不走 MESSAGE 转发表

- **WHEN** 平台收到指向下级通道的实时视频 INVITE
- **THEN** 平台按 `cascade-media-relay` 建立双对话，而不是把 INVITE 交给 MESSAGE 的转发逻辑
