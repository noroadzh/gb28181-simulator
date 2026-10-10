<script setup>
import { ref, onMounted, onBeforeUnmount, computed } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useNodeStore } from '../stores/nodes'
import { api } from '../api'

const router = useRouter()
const store = useNodeStore()
const filterKind = ref('')

// 节点控制操作的 loading 状态：每节点一项，按 node.id 索引，
// 避免在卡片被轮询替换时丢失按钮态。
const actionLoading = ref({})

async function refresh () {
  await store.refresh()
}

function goNode (id) {
  router.push({ name: 'capture', params: { id } })
}

// 状态 → el-tag type 映射（design D6）。registering 与 registered 同色
// warning，靠 effect="dark" 与 "light" 区分动态过程与静态就绪。
function statusType (status) {
  switch (status) {
    case 'online': return 'success'
    case 'fault': return 'danger'
    case 'registering': return 'warning'
    case 'registered': return 'warning'
    case 'idle': return 'info'
    case 'offline': return 'info'
    default: return 'info'
  }
}

function statusEffect (status) {
  if (status === 'registering') return 'dark'
  return 'light'
}

// 按钮显隐策略。设计 D1/D5：重试 = 再次 Start，不新增端点；注销按钮
// 按 (kind, status, has_registration) 三条件推导，避免出现点击必然
// 失败的按钮。
const startLabel = (n) => (n.status === 'fault' ? '重试' : '启动')
const canStart = (n) => ['idle', 'offline', 'fault'].includes(n.status)
const canStop = (n) => ['registering', 'registered', 'online'].includes(n.status)
const canUnregister = (n) =>
  (n.kind === 'device' || n.kind === 'platform-small') &&
  n.status === 'online' &&
  n.has_registration === true

async function callApi (node, fn) {
  const key = node.id
  actionLoading.value = { ...actionLoading.value, [key]: true }
  try {
    await fn()
    await refresh()
  } catch (e) {
    ElMessage.error(e.message || '操作失败')
  } finally {
    actionLoading.value = { ...actionLoading.value, [key]: false }
  }
}

function doStart (n) { return callApi(n, () => api.startNode(n.id)) }
function doStop (n) { return callApi(n, () => api.stopNode(n.id)) }
function doUnregister (n) { return callApi(n, () => api.unregisterNode(n.id)) }

const filteredList = computed(() =>
  store.list.filter(n => !filterKind.value || n.kind === filterKind.value)
)

onMounted(refresh)

// 节点列表轮询（design D4）：4 秒间隔，离开页时清理。
let pollHandle = null
onMounted(() => {
  pollHandle = setInterval(refresh, 4000)
})
onBeforeUnmount(() => {
  if (pollHandle) clearInterval(pollHandle)
})
</script>

<template>
  <el-card shadow="never">
    <template #header>
      <div style="display:flex;align-items:center;justify-content:space-between">
        <span><b>节点概览</b></span>
        <el-button type="primary" size="small" @click="refresh" :loading="store.loading">刷新</el-button>
      </div>
    </template>

    <el-row :gutter="12" style="margin-bottom:12px">
      <el-col :span="6">
        <el-select v-model="filterKind" placeholder="筛选类型" clearable style="width:100%">
          <el-option label="全部" value="" />
          <el-option label="device" value="device" />
          <el-option label="platform-small" value="platform-small" />
          <el-option label="platform-large" value="platform-large" />
        </el-select>
      </el-col>
    </el-row>

    <el-empty v-if="!store.loading && store.list.length === 0" description="暂无节点" />
    <el-row :gutter="12" v-else>
      <el-col :span="8" v-for="node in filteredList" :key="node.id" style="margin-bottom:12px">
        <el-card shadow="hover" @click="goNode(node.id)" style="cursor:pointer">
          <template #header>
            <div style="display:flex;justify-content:space-between;align-items:center">
              <span>{{ node.id }}</span>
              <el-tag :type="statusType(node.status)" :effect="statusEffect(node.status)" size="small">
                {{ node.status }}
              </el-tag>
            </div>
          </template>
          <div style="font-size:13px;line-height:2">
            <div><b>类型：</b>{{ node.kind }}</div>
            <div><b>地址：</b>{{ node.addr }}</div>
            <div><b>故障计数：</b>{{ Object.values(node.fault_counters || {}).reduce((a, b) => a + b, 0) || 0 }}</div>
          </div>
          <div style="margin-top:8px;display:flex;gap:8px;flex-wrap:wrap" @click.stop>
            <el-button size="small" @click="router.push({ name: 'capture', params: { id: node.id } })">抓包</el-button>
            <el-button size="small" @click="router.push({ name: 'fault', params: { id: node.id } })">故障注入</el-button>
            <el-button
              size="small"
              type="primary"
              @click="router.push({ name: 'channels', params: { id: node.id } })"
            >通道</el-button>
            <el-button
              v-if="node.kind === 'device'"
              size="small"
              type="success"
              @click="router.push({ name: 'media', params: { id: node.id } })"
            >媒体源</el-button>
            <el-button
              v-if="node.kind && node.kind.startsWith('platform')"
              size="small"
              type="warning"
              @click="router.push({ name: 'accounts', params: { id: node.id } })"
            >账号</el-button>
            <el-button
              v-if="canStart(node)"
              size="small"
              type="success"
              :loading="!!actionLoading[node.id]"
              :disabled="!!actionLoading[node.id]"
              @click="doStart(node)"
            >{{ startLabel(node) }}</el-button>
            <el-button
              v-if="canStop(node)"
              size="small"
              :loading="!!actionLoading[node.id]"
              :disabled="!!actionLoading[node.id]"
              @click="doStop(node)"
            >停止</el-button>
            <el-button
              v-if="canUnregister(node)"
              size="small"
              type="warning"
              :loading="!!actionLoading[node.id]"
              :disabled="!!actionLoading[node.id]"
              @click="doUnregister(node)"
            >注销</el-button>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </el-card>
</template>
