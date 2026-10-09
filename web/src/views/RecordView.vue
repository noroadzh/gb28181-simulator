<script setup>
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../api'
import { ElMessage } from 'element-plus'
import flvjs from 'flv.js'

const route = useRoute()
const router = useRouter()
const nodeId = computed(() => route.params.id)
const channelId = computed(() => route.params.ch)

const startTime = ref(formatTime(-86400))  // 默认查最近 24 小时
const endTime = ref(formatTime(0))
const records = ref([])
const loading = ref(false)
const selected = ref(null)
const playerRef = ref(null)
let player = null
const playing = ref(false)
const playError = ref('')

function formatTime (offsetSec) {
  const d = new Date(Date.now() + offsetSec * 1000)
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth()+1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

async function query () {
  loading.value = true
  try {
    records.value = await api.listRecords(nodeId.value, channelId.value, startTime.value, endTime.value)
  } catch (e) {
    ElMessage.error(`查询失败：${e.message}`)
  } finally {
    loading.value = false
  }
}

function fmtDuration (sec) {
  if (sec < 60) return `${sec}秒`
  if (sec < 3600) return `${Math.floor(sec/60)}分${sec%60}秒`
  return `${Math.floor(sec/3600)}时${Math.floor((sec%3600)/60)}分`
}

function playRecord (rec) {
  if (!rec || !rec.playback_url) {
    playError.value = '该录像没有回放URL'
    return
  }
  destroyPlayer()
  if (!flvjs.isSupported()) {
    playError.value = '浏览器不支持 FLV'
    return
  }
  player = flvjs.createPlayer({
    type: 'flv',
    url: rec.playback_url,
    isLive: false
  }, {
    enableWorker: true,
    stashInitialSize: 256
  })
  player.attachMediaElement(playerRef.value)
  player.load()
  player.play().then(() => {
    playing.value = true
    playError.value = ''
  }).catch(e => {
    playError.value = `播放失败：${e.message}`
    playing.value = false
  })
  player.on(flvjs.Events.ERROR, (e, _, info) => {
    playError.value = `解码错误：${info || e}`
    playing.value = false
  })
}

function destroyPlayer () {
  if (!player) return
  player.pause()
  player.unload()
  player.detachMediaElement()
  player.destroy()
  player = null
  playing.value = false
}

function setSpeed (s) {
  if (playerRef.value) playerRef.value.playbackRate = s
}

onMounted(query)
onBeforeUnmount(destroyPlayer)
</script>

<template>
  <div style="min-height:100vh;background:#0B1120;color:#F9FAFB;padding:16px">
    <div style="display:flex;align-items:center;gap:12px;margin-bottom:16px">
      <el-button @click="router.back()" size="small">返回</el-button>
      <span style="font-size:18px;font-weight:600">{{ nodeId }} / {{ channelId }} · 录像回放</span>
    </div>

    <el-row :gutter="16">
      <!-- 录像播放器 -->
      <el-col :span="16">
        <div style="position:relative;background:#000;border-radius:8px;overflow:hidden;aspect-ratio:16/9">
          <video ref="playerRef" style="width:100%;height:100%;display:block" controls></video>
          <div v-if="playError" style="position:absolute;inset:0;display:flex;align-items:center;justify-content:center;color:#ef4444;font-size:14px;background:rgba(0,0,0,0.6)">
            {{ playError }}
          </div>
        </div>
        <div style="margin-top:12px;display:flex;gap:8px;align-items:center">
          <span style="font-size:13px;color:#9ca3af">倍速：</span>
          <el-button size="small" @click="setSpeed(0.5)">0.5×</el-button>
          <el-button size="small" @click="setSpeed(1)">1×</el-button>
          <el-button size="small" @click="setSpeed(2)">2×</el-button>
          <el-button size="small" @click="setSpeed(4)">4×</el-button>
          <el-button size="small" @click="setSpeed(8)">8×</el-button>
        </div>
      </el-col>

      <!-- 录像查询面板 -->
      <el-col :span="8">
        <el-card shadow="never" style="background:#111827;border:1px solid #1F2937">
          <template #header><b style="color:#F9FAFB">录像查询</b></template>
          <el-form label-width="60px" label-position="left">
            <el-form-item label="开始">
              <el-input v-model="startTime" placeholder="YYYY-MM-DD HH:mm:ss" size="small" />
            </el-form-item>
            <el-form-item label="结束">
              <el-input v-model="endTime" placeholder="YYYY-MM-DD HH:mm:ss" size="small" />
            </el-form-item>
            <el-button type="primary" @click="query" :loading="loading" style="width:100%">查询</el-button>
          </el-form>
        </el-card>

        <el-card shadow="never" style="background:#111827;border:1px solid #1F2937;margin-top:12px" v-loading="loading">
          <template #header>
            <div style="display:flex;justify-content:space-between;align-items:center">
              <b style="color:#F9FAFB">录像列表</b>
              <el-tag size="small">{{ records.length }}</el-tag>
            </div>
          </template>
          <el-empty v-if="records.length === 0" description="无录像" :image-size="60" />
          <div v-else style="max-height:500px;overflow-y:auto">
            <div
              v-for="r in records"
              :key="r.id || r.start_time"
              @click="playRecord(r)"
              :style="{
                padding:'8px 10px',
                borderRadius:'4px',
                marginBottom:'6px',
                cursor:'pointer',
                background: selected?.id === r.id ? '#1F2937' : 'transparent',
                border: selected?.id === r.id ? '1px solid #1890FF' : '1px solid transparent'
              }"
            >
              <div style="display:flex;justify-content:space-between;font-size:13px">
                <span style="color:#22C55E">▶ {{ r.start_time }}</span>
                <el-tag size="small" type="info">{{ fmtDuration(r.duration) }}</el-tag>
              </div>
              <div style="font-size:11px;color:#9ca3af;margin-top:2px">
                {{ r.type || '录像' }} · 大小 {{ r.size || '—' }}
              </div>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>
