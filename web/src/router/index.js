import { createRouter, createWebHashHistory } from 'vue-router'
import NodesView from '../views/NodesView.vue'
import CaptureView from '../views/CaptureView.vue'
import FaultView from '../views/FaultView.vue'
import ScenarioView from '../views/ScenarioView.vue'
import DashboardView from '../views/DashboardView.vue'
import MediaView from '../views/MediaView.vue'

const routes = [
  { path: '/', redirect: '/nodes' },
  { path: '/nodes', name: 'nodes', component: NodesView, meta: { title: '节点概览' } },
  { path: '/capture/:id?', name: 'capture', component: CaptureView, meta: { title: '抓包面板' } },
  { path: '/fault/:id?', name: 'fault', component: FaultView, meta: { title: '故障注入' } },
  { path: '/scenarios', name: 'scenarios', component: ScenarioView, meta: { title: '场景管理' } },
  { path: '/dashboard', name: 'dashboard', component: DashboardView, meta: { title: '仪表盘' } },
  { path: '/media/:id?', name: 'media', component: MediaView, meta: { title: '媒体源配置' } }
]

const router = createRouter({
  history: createWebHashHistory(),
  routes
})

router.beforeEach((to) => {
  document.title = `gb28181-simulator - ${to.meta.title || ''}`
})

export default router
