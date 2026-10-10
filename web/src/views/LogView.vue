<script setup>
import { ref, onMounted, onUnmounted, watch } from 'vue'
import { api } from '../api'

const logs = ref([])
const paused = ref(false)
const filterLevel = ref('')
const keyword = ref('')
const ws = ref(null)
const reconnectTimer = ref(null)
const MAX_LOGS = 500

const LEVELS = ['trace', 'debug', 'info', 'warn', 'error']

function open () {
  if (ws.value) return
  ws.value = api.logsStream(
    (raw) => {
      if (paused.value) return
      const entry = parseLog(raw)
      if (!entry) return
      if (filterLevel.value && entry.level !== filterLevel.value) return
      if (keyword.value && !matchKeyword(entry, keyword.value)) return
      logs.value.push(entry)
      if (logs.value.length > MAX_LOGS) logs.value.splice(0, logs.value.length - MAX_LOGS)
    },
    () => {
      scheduleReconnect()
    }
  )
}

function scheduleReconnect () {
  if (reconnectTimer.value) return
  reconnectTimer.value = setTimeout(() => {
    reconnectTimer.value = null
    open()
  }, 2000)
}

function close () {
  if (ws.value) {
    ws.value.close()
    ws.value = null
  }
  if (reconnectTimer.value) {
    clearTimeout(reconnectTimer.value)
    reconnectTimer.value = null
  }
}

function parseLog (raw) {
  if (typeof raw !== 'object' || !raw) return null
  const ts = raw.timestamp || raw.ts || raw.time || ''
  const msg = raw.msg || raw.message || raw.content || JSON.stringify(raw)
  const level = String(raw.level || raw.severity || '').toLowerCase()
  const module = raw.module || raw.logger || raw.source || ''
  return { ts: String(ts), level, msg: String(msg), module: String(module), raw }
}

function matchKeyword (entry, kw) {
  if (!kw) return true
  const q = kw.toLowerCase()
  return entry.msg.toLowerCase().includes(q) || entry.module.toLowerCase().includes(q)
}

function clearLogs () {
  logs.value = []
}

function togglePause () {
  paused.value = !paused.value
}

function levelTagType (lvl) {
  const m = { trace: 'info', debug: 'info', info: 'success', warn: 'warning', error: 'danger' }
  return m[lvl] || 'info'
}

watch([filterLevel, keyword], () => {
  if (paused.value) return
  const entries = []
  for (const raw of logs.value) {
    if (filterLevel.value && raw.level !== filterLevel.value) continue
    if (keyword.value && !matchKeyword(raw, keyword.value)) continue
    entries.push(raw)
  }
  logs.value = entries
})

onMounted(() => { open() })
onUnmounted(() => { close() })
</script>

<template>
  <div style="padding:16px">
    <el-card>
      <template #header>
        <div style="display:flex;align-items:center;justify-content:space-between">
          <span>实时日志</span>
          <div>
            <el-select v-model="filterLevel" placeholder="级别筛选" clearable style="width:140px;margin-right:8px">
              <el-option v-for="l in LEVELS" :key="l" :label="l.toUpperCase()" :value="l" />
            </el-select>
            <el-input v-model="keyword" placeholder="关键字搜索" style="width:180px;margin-right:8px" clearable />
            <el-button :type="paused ? 'warning' : 'primary'" @click="togglePause">
              {{ paused ? '继续' : '暂停' }}
            </el-button>
            <el-button @click="clearLogs">清空</el-button>
          </div>
        </div>
      </template>

      <div style="background:#0b1120;color:#d9e2ec;font-family:Menlo,Consolas,monospace;font-size:13px;padding:12px;border-radius:4px;max-height:70vh;overflow-y:auto">
        <div v-if="logs.length === 0" style="color:#829ab1">等待日志输入...</div>
        <div v-for="(it, idx) in logs" :key="idx" style="padding:2px 0;border-bottom:1px dashed #1d2d44">
          <span style="color:#829ab1;margin-right:8px">{{ it.ts }}</span>
          <el-tag v-if="it.level" :type="levelTagType(it.level)" size="small" style="margin-right:6px;font-family:inherit">{{ it.level.toUpperCase() }}</el-tag>
          <span v-if="it.module" style="color:#66d9ef;margin-right:8px">[{{ it.module }}]</span>
          <span style="white-space:pre-wrap;word-break:break-all">{{ it.msg }}</span>
        </div>
      </div>
    </el-card>
  </div>
</template>
