# Proposal

## Why
级联媒体中继已交付，但运维只能抓包确认「谁在中继」。需要只读会话列表。

路线图：#48 `cascade-observability`。

## What Changes
- `GET /v1/nodes/:id/cascade/sessions`
- Web「级联中继」页
- 主 spec `cascade-observability`

## Non-goals
强制拆链按钮；改 MESSAGE/INVITE 语义。
