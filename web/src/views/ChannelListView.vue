<script setup>
import { ref, onMounted, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../api'
import { ElMessage, ElMessageBox } from 'element-plus'

const route = useRoute()
const router = useRouter()
const nodeId = computed(() => route.params.id)

const node = ref(null)
const channels = ref([])
const loading = ref(false)

async function refresh () {
  loading.value = true
  try {
    const [n, cs] = await Promise.all([
      api.getNode(nodeId.value),
      api.listChannels(nodeId.value)
    ])
    node.value = n
    channels.value = cs
  } catch (e) {
    ElMessage.error(`加载失败：${e.message}`)
  } finally {
    loading.value = false
  }
}

function statusType (s) {
  if (s === 'on' || s === 'online') return 'success'
  if (s === 'off' || s === 'offline') return 'info'
  return 'warning'
}

function enter (ch) {
  router.push({ name: 'channel', params: { id: nodeId.value, ch: ch.id } })
}

async function setMedia (ch) {
  const { value } = await ElMessageBox.prompt('输入媒体源 URL（留空清空）', `配置通道 ${ch.id} 媒体源`, {
    confirmButtonText: '保存',
    cancelButtonText: '取消',
    inputValue: '',
    inputPlaceholder: 'rtsp://... 或 file://... 或 https://.../playlist.m3u8'
  }).catch(() => ({}))
  if (value === undefined) return
  try {
    if (!value) {
      await api.deleteChannelMedia(nodeId.value, ch.id)
      ElMessage.success('已清空')
    } else {
      const cfg = parseMediaInput(value)
      await api.putChannelMedia(nodeId.value, ch.id, cfg)
      ElMessage.success('已保存')
    }
    refresh()
  } catch (e) {
    ElMessage.error(`配置失败：${e.message}`)
  }
}

function parseMediaInput (s) {
  if (s.startsWith('rtsp://') || s.startsWith('rtsps://')) return { kind: 'rtsp', url: s }
  if (s.startsWith('http://') || s.startsWith('https://')) {
    if (s.endsWith('.m3u8')) return { kind: 'hls', url: s }
    return { kind: 'remote_file', url: s }
  }
  if (s.startsWith('file://') || s.startsWith('/')) {
    const path = s.replace(/^file:\/\//, '')
    return { kind: 'local_file', path }
  }
  return { kind: 'synthetic', text: s }
}

async function copyFlv (ch) {
  const url = api.flvUrl(nodeId.value, ch.id)
  try {
    await navigator.clipboard.writeText(url)
    ElMessage.success(`已复制：${url}`)
  } catch (_) {
    ElMessage.warning('复制失败，请手动选取')
  }
}

onMounted(refresh)
</script>

<template>
  <el-card shadow="never" v-loading="loading">
    <template #header>
      <div style="display:flex;align-items:center;justify-content:space-between">
        <div>
          <el-button @click="router.back()" size="small" style="margin-right:8px">返回</el-button>
          <b>{{ nodeId }}</b>
          <el-tag v-if="node" :type="statusType(node.status)" size="small" style="margin-left:8px">{{ node.status }}</el-tag>
          <el-tag v-if="node" size="small" type="info" style="margin-left:4px">{{ node.kind }}</el-tag>
        </div>
        <el-button @click="refresh" size="small" :loading="loading">刷新</el-button>
      </div>
    </template>

    <el-empty v-if="channels.length === 0" description="该节点无通道" />
    <el-row v-else :gutter="12">
      <el-col :span="6" v-for="ch in channels" :key="ch.id" style="margin-bottom:12px">
        <el-card shadow="hover" @click="enter(ch)" style="cursor:pointer">
          <template #header>
            <div style="display:flex;justify-content:space-between;align-items:center">
              <span>{{ ch.name || ch.id }}</span>
              <el-tag :type="statusType(ch.status)" size="small">{{ ch.status }}</el-tag>
            </div>
          </template>
          <div style="font-size:12px;line-height:1.8;color:#9ca3af">
            <div><b>通道ID：</b>{{ ch.id }}</div>
            <div><b>媒体源：</b>{{ ch.has_media ? '已配置' : '未配置' }}</div>
            <div v-if="ch.online_url"><b>在线：</b>{{ ch.online_url }}</div>
          </div>
          <div style="margin-top:10px;display:flex;gap:6px;flex-wrap:wrap">
            <el-button size="small" type="primary" @click.stop="enter(ch)">播放</el-button>
            <el-button size="small" @click.stop="setMedia(ch)">媒体源</el-button>
            <el-button size="small" @click.stop="copyFlv(ch)">复制FLV</el-button>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </el-card>
</template>
