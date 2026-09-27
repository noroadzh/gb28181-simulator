<script setup>
import { ref, onMounted, onBeforeUnmount, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useNodeStore } from '../stores/nodes'
import { api } from '../api'
import { ElMessage } from 'element-plus'

const route = useRoute()
const store = useNodeStore()
const events = ref([])
const loading = ref(false)
let timer = null

async function load () {
  const id = route.params.id || store.selectedId || store.list[0]?.id
  if (!id) return
  loading.value = true
  try {
    events.value = await api.queryCapture(id, 50)
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    loading.value = false
  }
}

watch(() => route.params.id, () => {
  load()
  if (timer) clearInterval(timer)
  timer = setInterval(load, 3000)
}, { immediate: true })

onMounted(() => {
  timer = setInterval(load, 3000)
})

onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
})

function hex (str) {
  if (!str) return ''
  let h = ''
  for (let i = 0; i < str.length; i++) {
    const c = str.charCodeAt(i)
    h += c.toString(16).padStart(2, '0') + ' '
    if ((i + 1) % 16 === 0) h += '\n'
  }
  return h
}

function fmtDate (iso) {
  if (!iso) return ''
  const d = new Date(iso)
  return d.toLocaleString()
}

function selectNode (id) {
  store.select(id)
}
</script>

<template>
  <el-card shadow="never">
    <template #header>
      <div style="display:flex;align-items:center;justify-content:space-between">
        <span><b>抓包面板</b></span>
        <div>
          <el-select :model-value="route.params.id || store.selectedId" @change="selectNode" placeholder="选择节点" clearable style="width:260px;margin-right:8px">
            <el-option v-for="n in store.list" :key="n.id" :label="`${n.id} (${n.kind})`" :value="n.id" />
          </el-select>
          <el-button size="small" @click="load" :loading="loading">刷新</el-button>
          <el-button size="small" type="primary" @click="api.downloadPCAP(route.params.id || store.selectedId)" :disabled="!route.params.id && !store.selectedId">下载 pcap</el-button>
        </div>
      </div>
    </template>

    <el-table :data="events" stripe style="width:100%" v-loading="loading">
      <el-table-column prop="direction" label="方向" width="80">
        <template #default="{ row }">
          <el-tag :type="row.direction === 't' ? 'warning' : 'success'" size="small">{{ row.direction }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="at" label="时间" width="180">
        <template #default="{ row }">{{ fmtDate(row.at) }}</template>
      </el-table-column>
      <el-table-column prop="local" label="本地" width="160" show-overflow-tooltip />
      <el-table-column prop="remote" label="对端" width="160" show-overflow-tooltip />
      <el-table-column prop="transport" label="协议" width="80" />
      <el-table-column prop="bytes" label="Payload" show-overflow-tooltip>
        <template #default="{ row }">
          <el-popover placement="bottom" :width="500" trigger="click">
            <template #default>
              <pre style="white-space:pre-wrap;max-height:400px;overflow:auto;font-size:12px">{{ hex(row.bytes) }}</pre>
            </template>
            <template #reference>
              <span style="color:#409eff;cursor:pointer">{{ row.bytes.length }} bytes</span>
            </template>
          </el-popover>
        </template>
      </el-table-column>
    </el-table>

    <el-empty v-if="!loading && events.length === 0" description="暂无抓包数据" />
  </el-card>
</template>
