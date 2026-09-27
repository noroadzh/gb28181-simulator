<script setup>
import { ref, reactive, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../api'
import { ElMessage } from 'element-plus'

const route = useRoute()
const loading = ref(false)
const state = reactive({ status: 'idle', profile: {}, counters: {} })

const profileForm = reactive({
  canned: '',
  delayBase: 0,
  delayJitter: 0,
  drop: 0,
  blackhole: '',
  unsupportedMethod: 0
})

async function loadFault () {
  const id = route.params.id
  if (!id) return
  loading.value = true
  try {
    const data = await api.getFault(id)
    state.status = data.status
    state.profile = data.profile || {}
    state.counters = data.counters || {}
  } catch (e) {
    if (e.message !== 'no fault profile installed') ElMessage.error(e.message)
    state.status = 'idle'
    state.profile = {}
    state.counters = {}
  } finally {
    loading.value = false
  }
}

watch(() => route.params.id, loadFault, { immediate: true })

function parseCanned (text) {
  const obj = {}
  text.split(',').forEach(pair => {
    const [m, c] = pair.split(':').map(s => s.trim())
    if (m && c) obj[m] = Number(c)
  })
  return obj
}

function validateProfile (payload) {
  if (payload.canned) {
    for (const code of Object.values(payload.canned)) {
      if (code < 400 || code > 699) {
        return 'Canned 状态码必须在 400–699 区间'
      }
    }
  }
  if (payload.drop !== undefined && (payload.drop < 0 || payload.drop > 1)) {
    return '丢弃概率必须在 [0,1] 区间内'
  }
  if (profileForm.delayBase < 0 || profileForm.delayJitter < 0) {
    return 'Delay 时长必须为非负值'
  }
  return ''
}

async function install () {
  const id = route.params.id
  if (!id) return
  const payload = {}
  if (profileForm.canned) payload.canned = parseCanned(profileForm.canned)
  if (profileForm.delayBase > 0) {
    payload.delay = {
      base: `${profileForm.delayBase}ms`,
      jitter: profileForm.delayJitter ? `${profileForm.delayJitter}ms` : '0ms'
    }
  }
  if (profileForm.drop > 0) payload.drop = profileForm.drop
  if (profileForm.blackhole) payload.blackhole = profileForm.blackhole.split(',').map(s => s.trim()).filter(Boolean)
  if (profileForm.unsupportedMethod > 0) payload.unsupportedMethod = profileForm.unsupportedMethod
  const invalid = validateProfile(payload)
  if (invalid) {
    ElMessage.error(invalid)
    return
  }
  try {
    await api.installFault(id, payload)
    ElMessage.success('故障 profile 已安装')
    await loadFault()
  } catch (e) {
    ElMessage.error(e.message)
  }
}

async function clear () {
  const id = route.params.id
  if (!id) return
  try {
    await api.clearFault(id)
    ElMessage.success('故障 profile 已清除')
    await loadFault()
  } catch (e) {
    ElMessage.error(e.message)
  }
}

const total = () => Object.values(state.counters).reduce((a, b) => a + b, 0)
</script>

<template>
  <el-card shadow="never">
    <template #header>
      <b>故障注入</b>
      <span v-if="route.params.id" style="margin-left:12px;color:#888">{{ route.params.id }}</span>
    </template>

    <el-empty v-if="!route.params.id" description="请从节点概览页进入故障注入面板" />

    <template v-else>
      <el-row :gutter="16">
        <el-col :span="12">
          <el-card shadow="never" header="安装故障 Profile">
            <el-form label-width="120px">
              <el-form-item label="Canned (方法:状态码)">
                <el-input v-model="profileForm.canned" placeholder="REGISTER:403,INVITE:486" />
              </el-form-item>
              <el-form-item label="Delay Base (ms)">
                <el-input-number v-model="profileForm.delayBase" :min="0" :max="10000" />
              </el-form-item>
              <el-form-item label="Delay Jitter (ms)">
                <el-input-number v-model="profileForm.delayJitter" :min="0" :max="10000" />
              </el-form-item>
              <el-form-item label="Drop 概率">
                <el-input-number v-model="profileForm.drop" :min="0" :max="1" :step="0.1" />
              </el-form-item>
              <el-form-item label="Blackhole 方法">
                <el-input v-model="profileForm.blackhole" placeholder="MESSAGE,SUBSCRIBE" />
              </el-form-item>
              <el-form-item label="UnsupportedMethod">
                <el-input-number v-model="profileForm.unsupportedMethod" :min="0" :max="699" />
              </el-form-item>
              <el-form-item>
                <el-button type="primary" @click="install">安装</el-button>
              </el-form-item>
            </el-form>
          </el-card>
        </el-col>

        <el-col :span="12">
          <el-card shadow="never" header="当前 Profile">
            <el-descriptions border :column="1" v-if="Object.keys(state.profile).length">
              <el-descriptions-item label="Canned">{{ JSON.stringify(state.profile.canned || {}) }}</el-descriptions-item>
              <el-descriptions-item label="Delay">{{ JSON.stringify(state.profile.delay || {}) }}</el-descriptions-item>
              <el-descriptions-item label="Drop">{{ state.profile.drop ?? 0 }}</el-descriptions-item>
              <el-descriptions-item label="Blackhole">{{ (state.profile.blackhole || []).join(', ') }}</el-descriptions-item>
              <el-descriptions-item label="UnsupportedMethod">{{ state.profile.unsupportedMethod || 0 }}</el-descriptions-item>
            </el-descriptions>
            <el-empty v-else description="未安装故障 Profile" :image-size="60" />

            <div style="margin-top:12px">
              <b>异常计数：</b>{{ total() }}
              <el-tag v-for="(v, k) in state.counters" :key="k" style="margin-left:8px" size="small">{{ k }}={{ v }}</el-tag>
            </div>

            <div style="margin-top:16px">
              <el-button type="danger" @click="clear" :disabled="Object.keys(state.profile).length === 0">清除故障</el-button>
            </div>
          </el-card>
        </el-col>
      </el-row>
    </template>
  </el-card>
</template>
