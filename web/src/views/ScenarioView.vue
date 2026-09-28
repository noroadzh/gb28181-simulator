<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { api } from '../api'

const scenarios = ref([])
const loading = ref(false)
// 场景同步执行：同一时刻只允许一个运行中的场景，全部按钮禁用。
const runningName = ref('')
const report = ref(null)

async function load () {
  loading.value = true
  try {
    scenarios.value = await api.listScenarios()
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    loading.value = false
  }
}

async function loadLastRun () {
  try {
    report.value = await api.getLastRun()
  } catch (_) { /* 尚未运行过任何场景：忽略 404 */ }
}

async function run (item) {
  if (runningName.value) return
  runningName.value = item.Name
  try {
    report.value = await api.runScenario(item.Name)
    if (report.value.total === 'passed') {
      ElMessage.success(`场景 ${item.Name} 执行通过`)
    } else {
      ElMessage.error(`场景 ${item.Name} 执行失败`)
    }
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    runningName.value = ''
  }
}

function fmtDuration (r) {
  if (!r || !r.started_at || !r.finished_at) return '-'
  const ms = new Date(r.finished_at) - new Date(r.started_at)
  return ms >= 0 ? `${ms} ms` : '-'
}

function stepTagType (s) {
  if (s === 'passed') return 'success'
  if (s === 'failed') return 'danger'
  return 'info'
}

onMounted(() => {
  load()
  loadLastRun()
})
</script>

<template>
  <el-card shadow="never">
    <template #header><b>场景管理</b></template>
    <el-empty v-if="!loading && scenarios.length === 0" description="暂无场景" />
    <el-row v-else :gutter="12" v-loading="loading">
      <el-col v-for="item in scenarios" :key="item.Name" :span="12" style="margin-bottom:12px">
        <el-card shadow="hover">
          <template #header>
            <div style="display:flex;justify-content:space-between;align-items:center">
              <span>{{ item.Name }}</span>
              <el-tag v-if="runningName === item.Name" type="warning" size="small">执行中</el-tag>
            </div>
          </template>
          <div style="font-size:13px;line-height:2">{{ item.Description }}</div>
          <div style="margin-top:8px">
            <el-button
              size="small"
              type="primary"
              :loading="runningName === item.Name"
              :disabled="!!runningName"
              @click="run(item)"
            >执行</el-button>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <template v-if="report">
      <el-divider content-position="left">
        执行报告：{{ report.scenario_name }}
        <el-tag :type="report.total === 'passed' ? 'success' : 'danger'" size="small" style="margin-left:8px">
          {{ report.total }}
        </el-tag>
        <span style="font-size:12px;color:#909399;margin-left:8px">耗时 {{ fmtDuration(report) }}</span>
      </el-divider>
      <el-table :data="report.steps" size="small" border>
        <el-table-column prop="index" label="#" width="60" />
        <el-table-column prop="type" label="步骤类型" width="160" />
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="stepTagType(row.status)" size="small">{{ row.status }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="duration_ms" label="耗时(ms)" width="100" />
        <el-table-column label="错误">
          <template #default="{ row }">
            <span v-if="row.error" style="color:#f56c6c">{{ row.error }}</span>
            <span v-else style="color:#c0c4cc">-</span>
          </template>
        </el-table-column>
      </el-table>
    </template>
  </el-card>
</template>
