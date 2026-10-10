<script setup>
import { ref, onMounted, onBeforeUnmount, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../api'
import { ElMessage } from 'element-plus'
import flvjs from 'flv.js'

const route = useRoute()
const router = useRouter()
const nodeId = computed(() => route.params.id)
const channelId = computed(() => route.params.ch)

const channel = ref(null)
const loading = ref(false)
const playing = ref(false)
const playerError = ref('')
const playerRef = ref(null)
let player = null

// 播放模式：'native' | 'flv' | null
const playerMode = ref(null)
// 原生播放失败后是否已回退到 flv.js（防止循环）
const fallbackToFlv = ref(false)
// 播放代次计数器：startPlay/stopPlay/卸载都会递增，用于作废
// await resolveMediaConfig() 期间发生的停止/重复点击/组件卸载（防竞态）。
let playGeneration = 0

function currentPlayUrl () {
  if (playerMode.value === 'native') return api.mediaFileUrl(nodeId.value, channelId.value)
  return api.flvUrl(nodeId.value, channelId.value)
}

// PTZ
const ptzVisible = ref(false)
const ptzSpeed = ref(50)
const ptzInterval = ref(null)
const ptzCmd = ref(null)
const ptzActions = [
  { name: '上', key: 'up', icon: '↑' },
  { name: '下', key: 'down', icon: '↓' },
  { name: '左', key: 'left', icon: '←' },
  { name: '右', key: 'right', icon: '→' },
  { name: '左上', key: 'left-up', icon: '↖' },
  { name: '左上变倍', key: 'zoom-in-left-up', icon: '↖+' },
  { name: '左下', key: 'left-down', icon: '↙' },
  { name: '右下', key: 'right-down', icon: '↘' },
  { name: '右上', key: 'right-up', icon: '↗' },
  { name: '远', key: 'zoom-out', icon: '−' },
  { name: '近', key: 'zoom-in', icon: '+' },
  { name: '光圈开', key: 'iris-open', icon: '◎+' },
  { name: '光圈关', key: 'iris-close', icon: '◎−' },
  { name: '聚焦远', key: 'focus-far', icon: '◯−' },
  { name: '聚焦近', key: 'focus-near', icon: '◯+' },
  { name: '停止', key: 'stop', icon: '■' }
]

// Talk
const talkActive = ref(false)
const talkError = ref('')

// Snapshot
const snapshotData = ref(null)
const snapshotVisible = ref(false)

async function loadChannel () {
  loading.value = true
  try {
    channel.value = await api.listChannels(nodeId.value).then(cs => {
      const found = Array.isArray(cs) ? cs.find(c => c.id === channelId.value) : null
      if (!found) throw new Error('通道不存在')
      return found
    })
  } catch (e) {
    ElMessage.error(`加载失败：${e.message}`)
  } finally {
    loading.value = false
  }
}

function startFlv () {
  if (!playerRef.value) return
  if (player) { destroyFlv() }
  if (!flvjs.isSupported()) {
    playerError.value = '当前浏览器不支持 FLV 播放'
    return
  }
  playerMode.value = 'flv'
  const url = api.flvUrl(nodeId.value, channelId.value)
  player = flvjs.createPlayer({
    type: 'flv',
    url,
    hasAudio: false,
    isLive: true
  }, {
    // flv.js 1.6.2 + Vite 5 组合下 enableWorker:true 会导致 Web Worker 内
    // importScripts 模块解析失败, 抛出 "Class extends value undefined" 后
    // 整个播放器直接崩溃。关闭 worker 即可消除该错误, 对单路直播性能无感知。
    enableWorker: false,
    enableStashBuffer: false,
    stashInitialSize: 128
  })
  player.attachMediaElement(playerRef.value)
  // 在 flv.js 解析完 metadata 之后再触发 play(), 避免 load→play 竞态导致
  // "The play() request was interrupted by a call to pause()" 报错。
  let startedPlay = false
  const safePlay = () => {
    if (startedPlay || !player) return
    startedPlay = true
    player.play().then(() => {
      playing.value = true
      playerError.value = ''
    }).catch(e => {
      // AbortError 是 destroy 过程中触发的预期行为, 静默吞掉。
      if (e && e.name === 'AbortError') return
      playerError.value = `播放失败：${e.message || e}`
      playing.value = false
    })
  }
  player.on(flvjs.Events.METADATA_PARSED, safePlay)
  player.load()
  player.on(flvjs.Events.ERROR, (e, _, info) => {
    if (e === flvjs.ErrorTypes.MEDIA_ERROR &&
        info === flvjs.ErrorDetails.MEDIA_METADATA_PARSE_ERROR) {
      // 某些本地文件 metadata 解析会失败, 直接尝试播放
      safePlay()
      return
    }
    playerError.value = `解码错误：${info || e}`
    playing.value = false
  })
}

// 解析通道媒体配置：通道级未配置时回退节点级。
// 返回 { kind, path } 或 null（均未配置）。
async function resolveMediaConfig () {
  try {
    const d = await api.getChannelMedia(nodeId.value, channelId.value)
    if (d && d.has && d.config) return { kind: d.config.kind, path: d.config.path || '' }
  } catch (_) { /* 通道级查询失败继续回退节点级 */ }
  try {
    const d = await api.getMedia(nodeId.value)
    if (d && d.kind) return { kind: d.kind, path: d.path || '' }
  } catch (_) { /* 节点级也无配置 */ }
  return null
}

function isNativePlayable (cfg) {
  return !!cfg &&
    (cfg.kind === 'file' || cfg.kind === 'local_file') &&
    /\.mp4$/i.test((cfg.path || '').trim())
}

// 原生 <video> 播放 MP4 直出端点。失败时由 onVideoError 一次性回退 flv.js。
function startNative () {
  if (!playerRef.value) return
  playerMode.value = 'native'
  playerError.value = ''
  const url = api.mediaFileUrl(nodeId.value, channelId.value)
  const video = playerRef.value
  video.src = url
  video.play().then(() => {
    playing.value = true
    playerError.value = ''
  }).catch(e => {
    // 已回退 flv（onVideoError 先于本 catch 执行）时静默，避免误报错误
    if (playerMode.value !== 'native') return
    if (e && e.name === 'AbortError') return
    // 浏览器拒绝播放（编码不支持/文件损坏）→ 交给 error 处理统一回退
    playerError.value = `原生播放失败：${e.message || e}`
  })
}

// 原生播放失败（如 H.265 编码浏览器不可解）：一次性回退 flv.js。
function onVideoError () {
  if (playerMode.value !== 'native' || fallbackToFlv.value) return
  fallbackToFlv.value = true
  clearNativeVideo()
  playerError.value = ''
  startFlv()
}

function clearNativeVideo () {
  const video = playerRef.value
  if (!video) return
  video.pause()
  video.removeAttribute('src')
  video.load()
}

// 统一停止：按当前模式清理对应资源。
function stopPlay () {
  // 递增代次，作废任何进行中的 startPlay（await 期间被停止/卸载）
  playGeneration++
  if (playerMode.value === 'flv') {
    destroyFlv()
  } else if (playerMode.value === 'native') {
    clearNativeVideo()
    playing.value = false
  }
  playerMode.value = null
}

// 播放入口：解析媒体配置后分派播放模式。
async function startPlay () {
  if (!channel.value?.has_media) {
    playerError.value = '该通道未配置媒体源'
    return
  }
  stopPlay()
  fallbackToFlv.value = false
  playerError.value = ''
  const gen = playGeneration
  const cfg = await resolveMediaConfig()
  // await 期间发生了停止/再次播放/组件卸载 → 本次分派作废，防止操作已卸载的 DOM
  if (gen !== playGeneration || !playerRef.value) return
  if (isNativePlayable(cfg)) {
    startNative()
  } else {
    startFlv()
  }
}

function togglePlay () {
  if (playing.value) stopPlay()
  else startPlay()
}

function destroyFlv () {
  if (!player) return
  player.pause()
  player.unload()
  player.detachMediaElement()
  player.destroy()
  player = null
  playing.value = false
}

// PTZ helpers
function ptzCmdFor (key) {
  return { command: key, speed: ptzSpeed.value }
}

function startPtz (key) {
  if (key === 'stop') { ptzStop(); return }
  ptzCmd.value = key
  ptzInterval.value = setInterval(() => {
    if (ptzCmd.value !== key) return
    api.ptzControl(nodeId.value, channelId.value, ptzCmdFor(key)).catch(() => {})
  }, 500)
}

function stopPtz (key) {
  if (key === 'stop') {
    api.ptzControl(nodeId.value, channelId.value, ptzCmdFor('stop')).catch(() => {})
    return
  }
  clearInterval(ptzInterval.value)
  ptzInterval.value = null
  ptzCmd.value = null
  api.ptzControl(nodeId.value, channelId.value, ptzCmdFor('stop')).catch(() => {})
}

// Talk
async function startTalk () {
  try {
    await api.startTalk(nodeId.value, channelId.value)
    talkActive.value = true
    talkError.value = ''
    ElMessage.success('对讲已启动')
  } catch (e) {
    talkError.value = e.message
    ElMessage.error(`对讲启动失败：${e.message}`)
  }
}

async function stopTalk () {
  try {
    await api.stopTalk(nodeId.value, channelId.value)
    talkActive.value = false
    talkError.value = ''
    ElMessage.success('对讲已停止')
  } catch (e) {
    talkError.value = e.message
    ElMessage.error(`对讲停止失败：${e.message}`)
  }
}

// Snapshot
async function takeSnapshot () {
  try {
    const resp = await api.getSnapshot(nodeId.value, channelId.value)
    if (!resp.ok) throw new Error(resp.statusText)
    const blob = await resp.blob()
    const url = URL.createObjectURL(blob)
    snapshotData.value = url
    snapshotVisible.value = true
    // auto-download
    const a = document.createElement('a')
    a.href = url
    a.download = `${nodeId.value}_${channelId.value}_${Date.now()}.jpg`
    a.click()
  } catch (e) {
    ElMessage.error(`抓图失败：${e.message}`)
  }
}

function copyPlayUrl () {
  const url = currentPlayUrl()
  navigator.clipboard.writeText(url).then(() => ElMessage.success('已复制')).catch(() => {})
}

onBeforeUnmount(() => {
  stopPlay()
  if (ptzInterval.value) clearInterval(ptzInterval.value)
  if (snapshotData.value) URL.revokeObjectURL(snapshotData.value)
  if (talkActive.value) api.stopTalk(nodeId.value, channelId.value).catch(() => {})
})

onMounted(loadChannel)
</script>

<template>
  <div style="min-height:100vh;background:#0B1120;color:#F9FAFB;padding:16px">
    <!-- Header -->
    <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:16px">
      <div style="display:flex;align-items:center;gap:12px">
        <el-button @click="router.back()" size="small">返回</el-button>
        <span style="font-size:18px;font-weight:600">{{ nodeId }} / {{ channelId }}</span>
      </div>
      <div style="display:flex;gap:8px">
        <el-button size="small" @click="router.push({ name: 'record', params: { id: nodeId, ch: channelId } })">录像回放</el-button>
        <el-button size="small" @click="loadChannel">刷新</el-button>
      </div>
    </div>

    <el-row :gutter="16">
      <!-- 播放器区域 -->
      <el-col :span="16">
        <div style="position:relative;background:#000;border-radius:8px;overflow:hidden;aspect-ratio:16/9">
          <video ref="playerRef" style="width:100%;height:100%;display:block" muted @error="onVideoError"></video>
          <div v-if="!channel?.has_media" style="position:absolute;inset:0;display:flex;align-items:center;justify-content:center;flex-direction:column;gap:12px;color:#9ca3af">
            <div style="font-size:40px;opacity:0.4">📹</div>
            <div>通道未配置媒体源</div>
            <el-button type="primary" size="small" @click="router.push({ name: 'channels', params: { id: nodeId } })">去配置</el-button>
          </div>
          <div v-else-if="playerError" style="position:absolute;inset:0;display:flex;align-items:center;justify-content:center;flex-direction:column;gap:8px;color:#ef4444">
            <div style="font-size:32px">⚠️</div>
            <div style="font-size:13px">{{ playerError }}</div>
          </div>
        </div>

        <!-- 播放控制栏 -->
        <div style="margin-top:12px;display:flex;align-items:center;gap:12px;flex-wrap:wrap">
          <el-button :type="playing ? 'danger' : 'success'" @click="togglePlay">
            {{ playing ? '停止播放' : '开始播放' }}
          </el-button>
          <el-button @click="copyPlayUrl" size="small">{{ playerMode === 'native' ? '复制播放地址' : '复制FLV地址' }}</el-button>
          <el-button @click="takeSnapshot" size="small" :disabled="!playing">快照</el-button>
          <el-button @click="ptzVisible = !ptzVisible" size="small" :type="ptzVisible ? 'warning' : ''">云台控制</el-button>
          <el-button
            :type="talkActive ? 'warning' : ''"
            @click="talkActive ? stopTalk() : startTalk()"
            size="small"
            :disabled="!channel?.has_media"
          >
            {{ talkActive ? '🔴 结束对讲' : '🎤 开始对讲' }}
          </el-button>
          <span v-if="talkError" style="color:#f59e0b;font-size:12px">{{ talkError }}</span>
        </div>

        <!-- PTZ 面板 -->
        <div v-if="ptzVisible" style="margin-top:16px;background:#111827;border-radius:8px;padding:16px">
          <div style="margin-bottom:12px;font-size:13px;color:#9ca3af">云台控制 · 速度 {{ ptzSpeed }}%</div>
          <el-slider v-model="ptzSpeed" :min="10" :max="100" style="max-width:300px;margin-bottom:16px" />
          <div style="display:grid;grid-template-columns:repeat(5,60px);grid-template-rows:repeat(3,50px);gap:6px;width:fit-content">
            <div></div>
            <el-button
              style="grid-column:2;grid-row:1"
              @mousedown="startPtz('up')" @mouseup="stopPtz('up')"
              @mouseleave="stopPtz('up')"
            >↑</el-button>
            <div></div>
            <el-button
              style="grid-column:4;grid-row:1"
              @mousedown="startPtz('zoom-in')" @mouseup="stopPtz('zoom-in')"
              @mouseleave="stopPtz('zoom-in')"
            >+Z</el-button>
            <div></div>

            <el-button
              style="grid-column:1;grid-row:2"
              @mousedown="startPtz('left')" @mouseup="stopPtz('left')"
              @mouseleave="stopPtz('left')"
            >←</el-button>
            <el-button style="grid-column:2;grid-row:2" type="danger" @click="ptzStop()">■</el-button>
            <el-button
              style="grid-column:3;grid-row:2"
              @mousedown="startPtz('left-up')" @mouseup="stopPtz('left-up')"
              @mouseleave="stopPtz('left-up')"
            >↖</el-button>
            <el-button
              style="grid-column:4;grid-row:2"
              @mousedown="startPtz('right')" @mouseup="stopPtz('right')"
              @mouseleave="stopPtz('right')"
            >→</el-button>
            <div></div>

            <div></div>
            <el-button
              style="grid-column:2;grid-row:3"
              @mousedown="startPtz('down')" @mouseup="stopPtz('down')"
              @mouseleave="stopPtz('down')"
            >↓</el-button>
            <div></div>
            <el-button
              style="grid-column:4;grid-row:3"
              @mousedown="startPtz('zoom-out')" @mouseup="stopPtz('zoom-out')"
              @mouseleave="stopPtz('zoom-out')"
            >−Z</el-button>
            <div></div>
          </div>
          <div style="display:flex;gap:8px;margin-top:12px">
            <el-button size="small" @click="api.ptzControl(nodeId, channelId, {command:'iris-open',speed:ptzSpeed})">光圈+</el-button>
            <el-button size="small" @click="api.ptzControl(nodeId, channelId, {command:'iris-close',speed:ptzSpeed})">光圈−</el-button>
            <el-button size="small" @click="api.ptzControl(nodeId, channelId, {command:'focus-near',speed:ptzSpeed})">聚焦+</el-button>
            <el-button size="small" @click="api.ptzControl(nodeId, channelId, {command:'focus-far',speed:ptzSpeed})">聚焦−</el-button>
          </div>
        </div>
      </el-col>

      <!-- 通道信息侧栏 -->
      <el-col :span="8">
        <el-card shadow="never" style="background:#111827;border:1px solid #1F2937">
          <template #header><b style="color:#F9FAFB">通道信息</b></template>
          <div style="font-size:13px;line-height:2.2;color:#D1D5DB">
            <div><b style="color:#9ca3af">设备ID：</b>{{ nodeId }}</div>
            <div><b style="color:#9ca3af">通道ID：</b>{{ channelId }}</div>
            <div><b style="color:#9ca3af">状态：</b>
              <el-tag :type="channel?.status === 'on' ? 'success' : 'info'" size="small">{{ channel?.status }}</el-tag>
            </div>
            <div><b style="color:#9ca3af">媒体源：</b>{{ channel?.has_media ? '已配置' : '未配置' }}</div>
            <div v-if="channel?.online_url"><b style="color:#9ca3af">拉流URL：</b><code style="font-size:11px;word-break:break-all">{{ channel.online_url }}</code></div>
          </div>
          <div style="margin-top:16px;display:flex;flex-direction:column;gap:8px">
            <el-button type="primary" @click="router.push({ name: 'record', params: { id: nodeId, ch: channelId } })" size="small">录像回放</el-button>
            <el-button @click="router.push({ name: 'media', params: { id: nodeId } })" size="small">媒体源配置</el-button>
          </div>
        </el-card>

        <!-- 播放地址卡片（跟随实际播放模式） -->
        <el-card shadow="never" style="background:#111827;border:1px solid #1F2937;margin-top:12px">
          <template #header><b style="color:#F9FAFB">{{ playerMode === 'native' ? '播放地址（原生 MP4）' : '拉流地址（FLV）' }}</b></template>
          <code style="font-size:11px;word-break:break-all;color:#22C55E">{{ currentPlayUrl() }}</code>
          <div style="margin-top:8px">
            <el-button size="small" @click="copyPlayUrl">复制</el-button>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <!-- Snapshot dialog -->
    <el-dialog v-model="snapshotVisible" title="快照" width="640px">
      <img v-if="snapshotData" :src="snapshotData" style="width:100%;border-radius:4px" />
      <template #footer>
        <el-button @click="snapshotVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>
