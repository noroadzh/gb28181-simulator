# Tasks

## 1. 值对象与编解码

- [ ] 1.1 `DeviceStatus` 值对象与编解码（查询/应答），golden 断言字节
- [ ] 1.2 `Broadcast` 值对象与编解码（下发/ack），golden
- [ ] 1.3 `AlarmQuery` 值对象与编解码（查询/应答），golden
- [ ] 1.4 `ConfigDownload` 查询/下发与 ack 的值对象与编解码，golden
- [ ] 1.5 `DeviceUpgrade` / `DeviceReboot` 值对象与编解码，golden
- [ ] 1.6 `Download` 的 SDP 形状（`s=Download`）与解析，单测覆盖缺时间窗/无 `c=`

## 2. 设备侧处理

- [ ] 2.1 DeviceStatus 查询应答（Result/在线状态）
- [ ] 2.2 Broadcast ack + 主动音频 INVITE（复用对讲 offer）
- [ ] 2.3 AlarmQuery 应答（从节点报警快照按窗口过滤）与布防状态可查询
- [ ] 2.4 ConfigDownload 查询应答与下发（心跳间隔生效，其余 ERROR）
- [ ] 2.5 DeviceUpgrade / DeviceReboot 应答（记录版本/时间，不中断节点）
- [ ] 2.6 `s=Download` 受理：200 + sendonly SDP，按窗口发 PS/RTP，无录像 404

## 3. 平台侧处理

- [ ] 3.1 平台转发 DeviceStatus / AlarmQuery / ConfigDownload / Upgrade / Reboot 到在线下级并回传
- [ ] 3.2 平台布防撤防：改写节点布防状态，并让后续报警通知体现
- [ ] 3.3 平台作为下载请求方：发起 `s=Download`，把收到的流写入 `downloads/`

## 4. HTTP 入口

- [ ] 4.1 status / alarms / alarm-mode 三个端点与错误映射
- [ ] 4.2 config（GET/PUT）、upgrade、reboot 三个端点
- [ ] 4.3 download 端点（时间窗 → 发起下载 → 返回结果）

## 5. 收尾

- [ ] 5.1 e2e：广播（下发 → 设备 INVITE → 平台收音频）与下载（时间窗 → 落盘）
- [ ] 5.2 `docs/web-ui-guide.md` §5.6 增补六类命令一行；`openspec validate --strict`、`make lint`、`make test`（`-race`）通过
