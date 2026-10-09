<script setup>
import { ref, onMounted, onBeforeUnmount, computed } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'
import { ElMessage } from 'element-plus'

const router = useRouter()
const version = ref('…')
const commit = ref('…')
const goVersion = ref('…')
const platform = ref('…')
const status = ref('unknown')
const logs = ref([])

const nodes = ref([])
const channels = ref([])
const loading = ref(false)
let ws = null

const stats = computed(() => {
  const all = channels.value
  return {
    total: all.length,
    online: all.filter(c => c.status === 'on' || c.status === 'online').length,
    offline: all.filter(c => c.status === 'off' || c.status === 'offline').length,
    withMedia: all.filter(c => c.has_media).length
  }
})

async function loadMeta () {
  try {
    const j = await api.version()
    version.value = j.version
    commit.value = j.commit
    goVersion.value = j.go_version
    platform.value = j.platform
  } catch (e) {
    version.value = 'unreachable'
  }
}

async function pingHealth () {
  try {
    const j = await api.health()
    status.value = j.status
  } catch (e) {
    status.value = 'down'
  }
}

async function loadChannels () {
  loading.value = true
  try {
    const ns = await api.listNodes()
    nodes.value = ns
    const all = []
    for (const n of ns) {
      if (n.kind !== 'device') continue
      try {
        const cs = await api.listChannels(n.id)
        for (const c of (Array.isArray(cs) ? cs : [])) {
          all.push({ ...c, nodeId: n.id })
        }
      } catch (_) {}
    }
    channels.value = all
  } catch (e) {
    ElMessage.error(`通道加载失败：${e.message}`)
  } finally {
    loading.value = false
  }
}

function connectLogs () {
  ws = api.logsStream(
    (obj) => {
      logs.value.unshift(obj)
      if (logs.value.length > 200) logs.value.pop()
    },
    () => { status.value = 'ws-closed' }
  )
}

function openChannel (c) {
  router.push({ name: 'channel', params: { id: c.nodeId, ch: c.id } })
}

function copyFlv (c) {
  const url = api.flvUrl(c.nodeId, c.id)
  navigator.clipboard.writeText(url).then(() => ElMessage.success('已复制')).catch(() => {})
}

onMounted(() => {
  loadMeta()
  pingHealth()
  loadChannels()
  connectLogs()
})

onBeforeUnmount(() => {
  if (ws) ws.close()
})
</script>

<template>
  <div style="min-height:100vh;background:#0B1120;color:#F9FAFB">
    <div style="background:linear-gradient(135deg,#0B1120 0%,#1F2937 100%);padding:24px 32px;border-bottom:1px solid #1F2937">
      <div style="display:flex;align-items:center;justify-content:space-between">
        <div>
          <h1 style="margin:0;font-size:24px;font-weight:600;letter-spacing:0.5px">gb28181-simulator</h1>
          <div style="margin-top:6px;font-size:13px;color:#9CA3AF">
            <el-tag size="small" :type="status === 'ok' ? 'success' : 'danger'" style="margin-right:8px">{{ status }}</el-tag>
            v{{ version }} · {{ commit }} · Go {{ goVersion }} · {{ platform }}
          </div>
        </div>
        <div style="display:flex;gap:8px">
          <el-button @click="router.push({ name: 'nodes' })" size="small">节点</el-button>
          <el-button @click="router.push({ name: 'scenarios' })" size="small">场景</el-button>
          <el-button @click="loadChannels" size="small" :loading="loading">刷新</el-button>
        </div>
      </div>
    </div>

    <div style="padding:24px 32px">
      <!-- 统计卡片 -->
      <el-row :gutter="16" style="margin-bottom:24px">
        <el-col :span="6">
          <div style="background:#111827;border:1px solid #1F2937;border-radius:8px;padding:20px">
            <div style="color:#9CA3AF;font-size:13px">设备节点</div>
            <div style="font-size:28px;font-weight:600;margin-top:8px;color:#1890FF">{{ nodes.filter(n => n.kind === 'device').length }}</div>
          </div>
        </el-col>
        <el-col :span="6">
          <div style="background:#111827;border:1px solid #1F2937;border-radius:8px;padding:20px">
            <div style="color:#9CA3AF;font-size:13px">通道总数</div>
            <div style="font-size:28px;font-weight:600;margin-top:8px;color:#F9FAFB">{{ stats.total }}</div>
          </div>
        </el-col>
        <el-col :span="6">
          <div style="background:#111827;border:1px solid #1F2937;border-radius:8px;padding:20px">
            <div style="color:#9CA3AF;font-size:13px">在线 / 离线</div>
            <div style="font-size:28px;font-weight:600;margin-top:8px">
              <span style="color:#22C55E">{{ stats.online }}</span>
              <span style="color:#9CA3AF;font-size:18px"> / </span>
              <span style="color:#6B7280">{{ stats.offline }}</span>
            </div>
          </div>
        </el-col>
        <el-col :span="6">
          <div style="background:#111827;border:1px solid #1F2937;border-radius:8px;padding:20px">
            <div style="color:#9CA3AF;font-size:13px">已配媒体源</div>
            <div style="font-size:28px;font-weight:600;margin-top:8px;color:#F59E0B">{{ stats.withMedia }}</div>
          </div>
        </el-col>
      </el-row>

      <!-- 通道列表 + 日志 -->
      <el-row :gutter="16">
        <el-col :span="16">
          <el-card shadow="never" style="background:#111827;border:1px solid #1F2937" v-loading="loading">
            <template #header>
              <div style="display:flex;justify-content:space-between;align-items:center">
                <b style="color:#F9FAFB">通道列表</b>
                <el-tag size="small">{{ channels.length }}</el-tag>
              </div>
            </template>
            <el-empty v-if="channels.length === 0" description="暂无通道" :image-size="80" />
            <el-row v-else :gutter="12">
              <el-col :span="8" v-for="c in channels" :key="`${c.nodeId}:${c.id}`" style="margin-bottom:12px">
                <div
                  @click="openChannel(c)"
                  style="background:#0B1120;border:1px solid #1F2937;border-radius:6px;padding:12px;cursor:pointer;transition:all 0.2s"
                  onmouseover="this.style.borderColor='#1890FF';this.style.transform='translateY(-2px)'"
                  onmouseout="this.style.borderColor='#1F2937';this.style.transform='translateY(0)'"
                >
                  <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:6px">
                    <span style="font-size:13px;font-weight:500">{{ c.name || c.id }}</span>
                    <el-tag
                      :type="(c.status === 'on' || c.status === 'online') ? 'success' : 'info'"
                      size="small"
                    >{{ c.status }}</el-tag>
                  </div>
                  <div style="font-size:11px;color:#9CA3AF">
                    <div>设备：{{ c.nodeId }}</div>
                    <div>媒体源：{{ c.has_media ? '✓ 已配置' : '✗ 未配置' }}</div>
                  </div>
                  <div style="margin-top:8px;display:flex;gap:6px">
                    <el-button size="small" type="primary" @click.stop="openChannel(c)">播放</el-button>
                    <el-button size="small" @click.stop="copyFlv(c)">复制FLV</el-button>
                  </div>
                </div>
              </el-col>
            </el-row>
          </el-card>
        </el-col>

        <el-col :span="8">
          <el-card shadow="never" style="background:#111827;border:1px solid #1F2937">
            <template #header>
              <b style="color:#F9FAFB">实时日志</b>
            </template>
            <div style="max-height:520px;overflow-y:auto;font-family:monospace;font-size:11px;line-height:1.6">
              <div v-if="logs.length === 0" style="color:#6B7280;text-align:center;padding:20px">等待日志…</div>
              <div
                v-for="(l, i) in logs"
                :key="i"
                style="padding:4px 8px;border-bottom:1px solid #1F2937"
              >
                <span style="color:#6B7280">{{ l.time }}</span>
                <el-tag
                  size="small"
                  :type="l.level === 'ERROR' ? 'danger' : (l.level === 'WARN' ? 'warning' : 'info')"
                  style="margin:0 6px"
                >{{ l.level }}</el-tag>
                <span style="color:#D1D5DB">{{ l.msg }}</span>
              </div>
            </div>
          </el-card>
        </el-col>
      </el-row>
    </div>
  </div>
</template>