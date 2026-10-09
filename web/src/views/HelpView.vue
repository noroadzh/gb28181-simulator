<script setup>
</script>

<template>
  <el-card shadow="never">
    <template #header>
      <span style="font-weight:600">帮助：系统业务逻辑与对接指南</span>
    </template>

    <div class="help-body">
      <h2 id="arch">一、系统定位</h2>
      <p>
        本系统是一个 <b>GB/T 28181 国标平台模拟器</b>，单进程可同时扮演两类角色：
      </p>
      <ul>
        <li><b>platform（国标平台，UAS）</b>：对外提供 SIP 信令服务（默认 15060 端口），接收第三方设备/系统的注册，接受 CATALOG 查询、INVITE 点播等请求。第三方业务系统对接的就是它。</li>
        <li><b>device（设备，UAC）</b>：模拟 IPC/NVR 摄像头，主动注册到 platform，把自己的通道目录上报给平台，并按 INVITE 请求向外推送 RTP 媒体流。</li>
      </ul>
      <p>
        一个进程可以创建任意数量、任意类型的节点，组成任意拓扑（设备→小平台→大平台级联）。
      </p>

      <pre class="diagram">┌────────────────────────┐         ┌────────────────────────────┐
│  第三方业务系统 / 设备   │         │   gb28181-simulator         │
│  (VMS / 真实IPC / APP)  │         │                            │
└───────────┬────────────┘         │  ┌──────────────────────┐  │
            │ SIP :15060           │  │ platform 节点 (UAS)  │  │
            │ REGISTER / CATALOG   │  │  平台 Web UI :18080  │  │
            │ INVITE / RTP         │  └──────────▲───────────┘  │
            └──────────────────────┼─────────────┼──────────────┘
                                   │      SIP 注册│
                                   │      ┌──────┴───────────┐
                                   │      │ device 节点 (UAC)│  │
                                   │      │  设备 Web UI:18081│  │
                                   │      └──────────────────┘  │
                                   └────────────────────────────┘</pre>

      <h2 id="flow">二、业务系统对接全流程</h2>
      <p>
        假设您有一个业务系统要对接本模拟平台（拉取设备列表、生成视频流地址），整个流程分四步：
      </p>

      <el-steps direction="vertical" :active="4" style="margin:16px 0">
        <el-step title="① 注册 REGISTER" description="业务系统/设备向平台 SIP 地址（如 10.96.1.125:15060）发送 REGISTER，经 401 挑战 + 摘要认证后 200 OK 注册成功。账号须事先在【平台账号管理】页添加。">
        </el-step>
        <el-step title="② 拉取设备列表 CATALOG" description="平台向已注册设备发送 MESSAGE/CATALOG 查询，设备返回自己的通道（子通道）列表。通道在【设备节点 → 通道列表】页维护，可动态新增/删除。">
        </el-step>
        <el-step title="③ 点播 INVITE" description="业务系统向平台发 INVITE 指定点播某个通道，平台转发给设备；设备按自身媒体源配置（RTSP 流 / MP4 文件 / HLS）开始推流。">
        </el-step>
        <el-step title="④ 拉取媒体流 RTP/RTSP" description="设备通过 RTP（PS 封装）把视频流推送到 INVITE SDP 约定的地址；业务系统也可通过 HTTP-FLV 接口（:18090）在浏览器直接播放。">
        </el-step>
      </el-steps>

      <h2 id="uis">三、两个 Web UI 的分工</h2>
      <el-table :data="uiRoles" style="width:100%;margin:12px 0" border>
        <el-table-column prop="ui" label="Web UI" width="220" />
        <el-table-column prop="role" label="角色" width="140" />
        <el-table-column prop="usage" label="您在这里做什么" />
      </el-table>
      <p>
        简单记：<b>设备 UI 管通道和媒体源，平台 UI 管账号和看接入</b>。业务系统对接只需要连平台的 SIP 端口。
      </p>

      <h2 id="ops">四、常见操作</h2>
      <h3>1. 允许一台新设备注册到平台</h3>
      <p>平台 Web UI →【账号管理】→ 选择 platform 节点 → 新增账号（20 位国标编码 + 密码）。设备侧用同一对用户名/密码注册即可。</p>

      <h3>2. 给设备增加一路子通道（虚拟摄像头）</h3>
      <p>设备 Web UI →【节点概览】→ device 节点卡片 →【通道】→ 新增通道（20 位编码 + 名称）。新增的通道会持久化到 SQLite，重启不丢，并即时出现在 CATALOG 目录里。</p>

      <h3>3. 给通道配置视频源</h3>
      <p>设备 Web UI → 通道卡片 →【媒体源】：</p>
      <ul>
        <li><b>RTSP 流</b>：填 rtsp://... 地址，设备把外部 RTSP 流转封装为 PS+RTP 推出；</li>
        <li><b>MP4 等文件</b>：直接<b>拖拽上传</b>（自动保存到容器内并回填路径），或手动填写容器内路径；</li>
        <li><b>HLS</b>：填 m3u8 地址；</li>
        <li><b>合成图</b>：无真实片源时生成动态合成画面用于测试。</li>
      </ul>

      <h3>4. 在浏览器直接预览</h3>
      <p>通道详情页提供 HTTP-FLV 播放器（流媒体网关 :18090），无需 VLC 即可直接观看点播流。</p>

      <h2 id="ports">五、默认端口速查</h2>
      <el-table :data="ports" style="width:100%" border>
        <el-table-column prop="port" label="端口" width="120" />
        <el-table-column prop="proto" label="协议" width="120" />
        <el-table-column prop="desc" label="用途" />
      </el-table>
    </div>
  </el-card>
</template>

<script>
const uiRoles = [
  { ui: '平台 Web UI（:18080）', role: '国标平台管理端', usage: '维护 SIP 注册账号；查看已注册设备/在线状态；抓包、故障注入、场景编排、抓取 pcap' },
  { ui: '设备 Web UI（:18081）', role: '模拟设备管理端', usage: '维护通道（新增/删除子通道）；配置节点级/通道级媒体源（RTSP/上传 MP4/HLS/合成图）；PTZ、录像回放、对讲' }
]
const ports = [
  { port: '15060', proto: 'SIP/UDP', desc: '平台 SIP 信令端口（业务系统/设备对接入口）' },
  { port: '15061', proto: 'SIP/UDP', desc: '设备侧 SIP 端口（级联场景）' },
  { port: '18080', proto: 'HTTP', desc: '平台 Web UI + REST API' },
  { port: '18081', proto: 'HTTP', desc: '设备 Web UI + REST API' },
  { port: '18090', proto: 'HTTP-FLV', desc: '流媒体网关（浏览器直接播放）' },
  { port: '30000-31000', proto: 'RTP/UDP', desc: '媒体流传输端口范围' }
]
export default {
  data () {
    return { uiRoles, ports }
  }
}
</script>

<style scoped>
.help-body {
  max-width: 900px;
  font-size: 14px;
  line-height: 1.8;
  color: #303133;
}
.help-body h2 {
  font-size: 16px;
  margin: 28px 0 8px;
  padding-left: 8px;
  border-left: 3px solid #409eff;
}
.help-body h3 {
  font-size: 14px;
  margin: 18px 0 6px;
}
.help-body ul {
  padding-left: 20px;
  margin: 6px 0;
}
.diagram {
  background: #f5f7fa;
  border: 1px solid #e4e7ed;
  border-radius: 4px;
  padding: 12px;
  font-size: 12px;
  line-height: 1.4;
  overflow-x: auto;
}
</style>
