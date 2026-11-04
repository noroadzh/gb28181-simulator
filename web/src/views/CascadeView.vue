<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'
import { useNodeStore } from '../stores/nodes'
import { ElMessage } from 'element-plus'

const router = useRouter()
const nodeStore = useNodeStore()
const nodeId = ref('')
const loading = ref(false)
const sessions = ref([])

const platformNodes = computed(() =>
  (nodeStore.list || []).filter(n => String(n.kind || '').startsWith('platform'))
)

async function load () {
  if (!nodeId.value) {
    sessions.value = []
    return
  }
  loading.value = true
  try {
    const body = await api.listCascadeSessions(nodeId.value)
    sessions.value = Array.isArray(body?.sessions) ? body.sessions : []
  } catch (e) {
    ElMessage.error(`加载失败：${e.message}`)
    sessions.value = []
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  await nodeStore.refresh()
  if (!nodeId.value && platformNodes.value.length) {
    nodeId.value = platformNodes.value[0].id
  }
  await load()
})

watch(nodeId, load)
</script>

<template>
  <div>
    <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:16px;gap:12px;flex-wrap:wrap">
      <div>
        <h2 style="margin:0">级联媒体中继</h2>
        <div style="font-size:13px;color:#666;margin-top:4px">只读观测上级 Call-ID ↔ 下级通道；拆链请走 SIP BYE / 停止拉流</div>
      </div>
      <div style="display:flex;gap:8px;align-items:center">
        <el-select v-model="nodeId" placeholder="选择平台节点" style="width:280px" filterable>
          <el-option v-for="n in platformNodes" :key="n.id" :label="`${n.id} (${n.kind})`" :value="n.id" />
        </el-select>
        <el-button @click="load" :loading="loading">刷新</el-button>
        <el-button v-if="nodeId" @click="router.push({ name: 'capture', params: { id: nodeId } })">抓包</el-button>
      </div>
    </div>

    <el-table :data="sessions" v-loading="loading" empty-text="当前无活动中继" stripe>
      <el-table-column prop="upstream_call_id" label="上级 Call-ID" min-width="180" />
      <el-table-column prop="channel_id" label="通道" min-width="160" />
      <el-table-column prop="peer" label="对端" min-width="140" />
      <el-table-column prop="transport" label="传输" width="90" />
      <el-table-column prop="local_port" label="本地端口" width="100" />
      <el-table-column prop="started_at" label="开始时间" min-width="180" />
    </el-table>
  </div>
</template>
