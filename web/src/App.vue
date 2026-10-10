<script setup>
import { ref, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useNodeStore } from './stores/nodes'
import { api } from './api'
import { ElMessage } from 'element-plus'

const router = useRouter()
const route = useRoute()
const nodeStore = useNodeStore()
const meta = ref({ version: '…', commit: '…', go: '…', platform: '…', status: 'unknown' })
const drawer = ref(false)

const menu = [
  { path: '/nodes', title: '节点概览' },
  { path: '/accounts', title: '账号管理' },
  { path: '/logs', title: '实时日志' },
  { path: '/help', title: '帮助' },
  { path: '/capture', title: '抓包面板' },
  { path: '/fault', title: '故障注入' },
  { path: '/scenarios', title: '场景管理' },
  { path: '/dashboard', title: '仪表盘' }
]

async function loadMeta () {
  try {
    const j = await api.version()
    meta.value.version = j.version
    meta.value.commit = j.commit
    meta.value.go = j.go_version
    meta.value.platform = j.platform
  } catch (_) {
    meta.value.version = 'unreachable'
  }
}

onMounted(() => {
  loadMeta()
  nodeStore.refresh()
})
</script>

<template>
  <el-container style="min-height:100vh">
    <el-aside width="200px" style="background:#001529;color:#fff">
      <div style="height:60px;display:flex;align-items:center;justify-content:center;font-size:16px;font-weight:bold">
        gb28181-simulator
      </div>
      <el-menu
        :default-active="route.path"
        router
        background-color="#001529"
        text-color="rgba(255,255,255,0.65)"
        active-text-color="#fff"
      >
        <el-menu-item v-for="item in menu" :key="item.path" :index="item.path">
          <span>{{ item.title }}</span>
        </el-menu-item>
      </el-menu>
    </el-aside>

    <el-container>
      <el-header style="background:#fff;border-bottom:1px solid #eee;display:flex;align-items:center;justify-content:space-between">
        <div style="font-size:14px;color:#666">
          <el-breadcrumb separator="/">
            <el-breadcrumb-item>{{ route.meta.title || '首页' }}</el-breadcrumb-item>
          </el-breadcrumb>
        </div>
        <div style="font-size:12px;color:#888">
          <el-tag type="success" size="small">{{ meta.status }}</el-tag>
          <span style="margin-left:12px">v{{ meta.version }}</span>
        </div>
      </el-header>

      <el-main style="background:#f5f7fa">
        <router-view />
      </el-main>
    </el-container>
  </el-container>
</template>
