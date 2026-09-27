<script setup>
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { api } from '../api'
import { ElMessage } from 'element-plus'

const version = ref('…')
const commit = ref('…')
const goVersion = ref('…')
const platform = ref('…')
const status = ref('unknown')
const logs = ref([])
let ws = null

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

function connectLogs () {
  ws = api.logsStream(
    (obj) => {
      logs.value.unshift(obj)
      if (logs.value.length > 200) logs.value.pop()
    },
    () => { status.value = 'ws-closed' }
  )
}

onMounted(() => {
  loadMeta()
  pingHealth()
  connectLogs()
})

onBeforeUnmount(() => {
  if (ws) ws.close()
})
</script>

<template>
  <el-container style="min-height:100vh">
    <el-header style="background:#1f2d40;color:#fff;display:flex;align-items:center">
      <h2 style="margin:0">gb28181-simulator</h2>
      <el-tag style="margin-left:16px" :type="status === 'ok' ? 'success' : 'danger'">{{ status }}</el-tag>
    </el-header>
    <el-main>
      <el-row :gutter="16">
        <el-col :span="8">
          <el-card shadow="hover">
            <template #header>Version</template>
            <p><b>Version:</b> {{ version }}</p>
            <p><b>Commit:</b> {{ commit }}</p>
            <p><b>Go:</b> {{ goVersion }}</p>
            <p><b>Platform:</b> {{ platform }}</p>
          </el-card>
        </el-col>
        <el-col :span="16">
          <el-card shadow="hover">
            <template #header>Live log stream (WS /v1/logs/stream)</template>
            <el-table :data="logs" height="420" stripe>
              <el-table-column prop="time" label="Time" width="220" />
              <el-table-column prop="level" label="Level" width="100" />
              <el-table-column prop="msg" label="Message" />
            </el-table>
          </el-card>
        </el-col>
      </el-row>
    </el-main>
  </el-container>
</template>
