<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useNodeStore } from './stores/nodes'
import { useRoleStore } from './stores/role'
import { useAuthStore } from './stores/auth'
import { api } from './api'

const router = useRouter()
const route = useRoute()
const nodeStore = useNodeStore()
const roleStore = useRoleStore()
const authStore = useAuthStore()
const meta = ref({ version: '…', commit: '…', go: '…', platform: '…', status: 'unknown' })

const menu = [
  { path: '/dashboard', title: '仪表盘' },
  { path: '/nodes', title: '节点概览' },
  { path: '/accounts', title: '账号管理', roles: ['platform'] },
  { path: '/cascade', title: '级联中继', roles: ['platform'] },
  { path: '/media', title: '媒体源', roles: ['device'] },
  { path: '/logs', title: '实时日志' },
  { path: '/capture', title: '抓包面板' },
  { path: '/fault', title: '故障注入' },
  { path: '/scenarios', title: '场景管理' },
  { path: '/help', title: '帮助' }
]

const isLoginPage = computed(() => route.name === 'login')
const visibleMenu = computed(() => menu.filter(item => roleStore.menuVisible(item)))

const roleMode = computed({
  get: () => roleStore.mode,
  set: (v) => roleStore.setMode(v)
})

const asideStyle = computed(() => {
  if (roleStore.mode === 'platform') {
    return { background: '#0f2744', color: '#fff' }
  }
  if (roleStore.mode === 'device') {
    return { background: '#1a2e1a', color: '#fff' }
  }
  return { background: '#001529', color: '#fff' }
})

async function loadMeta () {
  try {
    const j = await api.version()
    meta.value.version = j.version
    meta.value.commit = j.commit
    meta.value.go = j.go_version
    meta.value.platform = j.platform
    meta.value.status = 'ok'
  } catch (_) {
    meta.value.version = 'unreachable'
    meta.value.status = 'down'
  }
}

async function logout () {
  await authStore.logout()
  ElMessage.success('已退出登录')
  router.replace('/login')
}

watch(() => roleStore.mode, () => {
  const hit = menu.find(m => route.path === m.path || route.path.startsWith(m.path + '/'))
  if (hit && !roleStore.menuVisible(hit)) {
    router.push('/nodes')
  }
})

watch(() => authStore.authenticated, (ok) => {
  if (ok && !isLoginPage.value) {
    loadMeta()
    nodeStore.refresh()
  }
})

onMounted(async () => {
  if (!authStore.ready) {
    try { await authStore.refresh() } catch (_) {}
  }
  if (!authStore.needsLogin) {
    loadMeta()
    nodeStore.refresh()
  }
})
</script>

<template>
  <router-view v-if="isLoginPage" />

  <el-container v-else style="min-height:100vh">
    <el-aside width="200px" :style="asideStyle">
      <div style="height:60px;display:flex;flex-direction:column;align-items:center;justify-content:center;font-size:15px;font-weight:bold;line-height:1.2;padding:0 8px;text-align:center">
        <div>gb28181-simulator</div>
        <div style="font-size:11px;font-weight:normal;opacity:0.75;margin-top:4px">{{ roleStore.label }}</div>
      </div>
      <el-menu
        :default-active="route.path"
        router
        :background-color="asideStyle.background"
        text-color="rgba(255,255,255,0.65)"
        active-text-color="#fff"
      >
        <el-menu-item v-for="item in visibleMenu" :key="item.path" :index="item.path">
          <span>{{ item.title }}</span>
        </el-menu-item>
      </el-menu>
    </el-aside>

    <el-container>
      <el-header style="background:#fff;border-bottom:1px solid #eee;display:flex;align-items:center;justify-content:space-between;gap:12px">
        <div style="font-size:14px;color:#666">
          <el-breadcrumb separator="/">
            <el-breadcrumb-item>{{ route.meta.title || '首页' }}</el-breadcrumb-item>
          </el-breadcrumb>
        </div>
        <div style="display:flex;align-items:center;gap:12px;font-size:12px;color:#888">
          <el-radio-group v-model="roleMode" size="small">
            <el-radio-button label="all">全部</el-radio-button>
            <el-radio-button label="platform">平台</el-radio-button>
            <el-radio-button label="device">设备</el-radio-button>
          </el-radio-group>
          <el-tag type="success" size="small">{{ meta.status }}</el-tag>
          <span>v{{ meta.version }}</span>
          <template v-if="authStore.enabled">
            <span>{{ authStore.username || '已登录' }}</span>
            <el-button size="small" text type="primary" @click="logout">退出</el-button>
          </template>
        </div>
      </el-header>

      <el-main style="background:#f5f7fa">
        <router-view />
      </el-main>
    </el-container>
  </el-container>
</template>
