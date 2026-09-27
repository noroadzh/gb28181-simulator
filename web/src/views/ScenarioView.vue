<script setup>
import { ref } from 'vue'
import { ElMessage } from 'element-plus'

const scenarios = ref([
  { id: 's1', name: '正常注册流程', description: '设备正常注册、keepalive、注销', status: 'ready' },
  { id: 's2', name: '级联路由验证', description: '小平台转发至大平台', status: 'ready' }
])

async function run (item) {
  try {
    const resp = await fetch('/v1/scenarios/run', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id: item.id })
    })
    if (resp.status === 501) {
      ElMessage.warning('该能力由 #15 scenario-engine 提供，当前版本尚不支持')
      return
    }
    if (!resp.ok) throw new Error('请求失败')
    ElMessage.success('场景已提交')
  } catch (e) {
    ElMessage.error(e.message)
  }
}
</script>

<template>
  <el-card shadow="never">
    <template #header><b>场景管理</b></template>
    <el-empty v-if="scenarios.length === 0" description="暂无场景" />
    <el-row :gutter="12" v-else>
      <el-col :span="12" v-for="item in scenarios" :key="item.id" style="margin-bottom:12px">
        <el-card shadow="hover">
          <template #header>
            <div style="display:flex;justify-content:space-between;align-items:center">
              <span>{{ item.name }}</span>
              <el-tag type="info" size="small">{{ item.status }}</el-tag>
            </div>
          </template>
          <div style="font-size:13px;line-height:2">{{ item.description }}</div>
          <div style="margin-top:8px">
            <el-button size="small" type="primary" @click="run(item)">执行</el-button>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </el-card>
</template>
