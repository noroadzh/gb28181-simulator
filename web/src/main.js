import { createApp } from 'vue'
import ElementPlus, { ElMessage } from 'element-plus'
import 'element-plus/dist/index.css'
import { createPinia } from 'pinia'
import router from './router'
import App from './App.vue'
import { installAuthGuard } from './auth-guard'
import { useAuthStore } from './stores/auth'

const app = createApp(App)
app.use(ElementPlus)
app.use(createPinia())

installAuthGuard(async ({ reason, navigate }) => {
  const auth = useAuthStore()
  if (auth.enabled) auth.markUnauthenticated()
  const tip = reason === 'expired'
    ? '登录已过期，请重新登录'
    : '未登录或授权无效，请先登录'
  ElMessage.warning(tip)
  if (!navigate || router.currentRoute.value.name === 'login') return
  const cur = router.currentRoute.value.fullPath
  const redirect = (!cur || cur.startsWith('/login')) ? '/dashboard' : cur
  await router.replace({ path: '/login', query: { redirect } })
})

app.use(router)
app.mount('#app')
