<script setup>
import { ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '../stores/auth'

const auth = useAuthStore()
const router = useRouter()
const route = useRoute()

const username = ref('admin')
const password = ref('')
const loading = ref(false)

async function submit () {
  if (!username.value || !password.value) {
    ElMessage.warning('请输入用户名和密码')
    return
  }
  loading.value = true
  try {
    await auth.login(username.value.trim(), password.value)
    ElMessage.success('登录成功')
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/dashboard'
    router.replace(redirect || '/dashboard')
  } catch (e) {
    ElMessage.error(e.message || '登录失败')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login-page">
    <div class="login-panel">
      <div class="brand">
        <div class="title">gb28181-simulator</div>
        <div class="sub">操作台登录</div>
      </div>
      <el-form label-position="top" @submit.prevent="submit">
        <el-form-item label="用户名">
          <el-input v-model="username" autocomplete="username" placeholder="admin" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input
            v-model="password"
            type="password"
            show-password
            autocomplete="current-password"
            placeholder="请输入密码"
            @keyup.enter="submit"
          />
        </el-form-item>
        <el-button type="primary" style="width:100%" :loading="loading" @click="submit">
          登录
        </el-button>
      </el-form>
      <p class="hint">此账号保护 Web 管理面与 HTTP API，与 SIP 设备注册账号相互独立。</p>
    </div>
  </div>
</template>

<style scoped>
.login-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background:
    radial-gradient(ellipse at 20% 10%, rgba(56, 120, 160, 0.25), transparent 50%),
    radial-gradient(ellipse at 80% 90%, rgba(30, 60, 40, 0.3), transparent 45%),
    linear-gradient(160deg, #0b1220 0%, #152033 45%, #0f1a14 100%);
  color: #e8eef5;
}
.login-panel {
  width: 100%;
  max-width: 380px;
  padding: 32px 28px 24px;
  border: 1px solid rgba(255, 255, 255, 0.12);
  background: rgba(12, 18, 28, 0.82);
  backdrop-filter: blur(8px);
}
.brand {
  margin-bottom: 24px;
}
.title {
  font-size: 22px;
  font-weight: 700;
  letter-spacing: 0.02em;
}
.sub {
  margin-top: 6px;
  font-size: 13px;
  color: rgba(232, 238, 245, 0.65);
}
.hint {
  margin: 18px 0 0;
  font-size: 12px;
  line-height: 1.5;
  color: rgba(232, 238, 245, 0.45);
}
:deep(.el-form-item__label) {
  color: rgba(232, 238, 245, 0.8);
}
</style>
