<script setup>
import { ref, reactive, watch, onMounted } from 'vue'
import { api } from '../api'
import { ElMessage } from 'element-plus'

const props = defineProps({
  nodeId: { type: String, required: true },
  kind: { type: String, default: '' }
})

const loading = ref(false)
const uploading = ref(false)
const state = reactive({
  hasConfig: false,
  config: {
    kind: 'synthetic',
    path: '',
    loop: false,
    mtu: 1400,
    fps: 25,
    clock: 90000
  }
})

const form = reactive({
  kind: 'synthetic',
  path: '',
  loop: false,
  mtu: 1400,
  fps: 25,
  clock: 90000
})

const sourceKinds = [
  { label: '本地文件 (file)', value: 'file' },
  { label: 'RTSP 拉流 (rtsp)', value: 'rtsp' },
  { label: 'HLS 拉流 (hls)', value: 'hls' },
  { label: '合成图 (synthetic)', value: 'synthetic' }
]

async function onUploadFile (uploadFile) {
  if (!uploadFile || !uploadFile.raw) return
  uploading.value = true
  try {
    const res = await api.uploadMedia(props.nodeId, uploadFile.raw)
    form.path = res.path
    form.kind = 'file'
    ElMessage.success('上传成功，路径已回填')
  } catch (e) {
    ElMessage.error('上传失败：' + (e.message || e))
  } finally {
    uploading.value = false
  }
}

async function load () {
  if (!props.nodeId) return
  if (props.kind && props.kind !== 'device') {
    // 平台节点不支持媒体源，提示并清空
    state.hasConfig = false
    return
  }
  loading.value = true
  try {
    const data = await api.getMedia(props.nodeId)
    if (data && data.kind) {
      state.hasConfig = true
      state.config = { ...form, ...data }
      Object.assign(form, state.config)
    } else {
      state.hasConfig = false
      // 重置表单为默认
      form.kind = 'synthetic'
      form.path = ''
      form.loop = false
      form.mtu = 1400
      form.fps = 25
      form.clock = 90000
    }
  } catch (e) {
    if (e.message && e.message.includes('no media configured')) {
      state.hasConfig = false
    } else {
      ElMessage.error(e.message)
    }
  } finally {
    loading.value = false
  }
}

watch(() => [props.nodeId, props.kind], load, { immediate: true })
onMounted(load)

function pathRequired () {
  if (form.kind === 'synthetic') return false
  return !form.path || form.path.trim() === ''
}

async function save () {
  if (!props.nodeId) return
  if (pathRequired()) {
    ElMessage.error(`${form.kind} 源必须填写 path`)
    return
  }
  if (form.mtu < 100 || form.mtu > 1500) {
    ElMessage.error('MTU 必须在 [100, 1500] 区间')
    return
  }
  if (form.fps < 1 || form.fps > 60) {
    ElMessage.error('FPS 必须在 [1, 60] 区间')
    return
  }
  if (form.clock < 1000) {
    ElMessage.error('Clock 必须 ≥ 1000')
    return
  }
  if (form.loop && form.kind !== 'file') {
    ElMessage.error('循环播放仅支持本地文件 (file) 源，其他类型不支持循环')
    return
  }
  const payload = {
    kind: form.kind,
    path: form.path,
    loop: form.loop,
    mtu: form.mtu,
    fps: form.fps,
    clock: form.clock
  }
  try {
    await api.putMedia(props.nodeId, payload)
    ElMessage.success('媒体源已保存')
    await load()
  } catch (e) {
    ElMessage.error(e.message)
  }
}

async function clear () {
  if (!props.nodeId) return
  try {
    await api.deleteMedia(props.nodeId)
    ElMessage.success('媒体源已清除')
    await load()
  } catch (e) {
    ElMessage.error(e.message)
  }
}
</script>

<template>
  <el-card shadow="never" header="媒体源配置" v-loading="loading">
    <el-alert
      v-if="kind && kind !== 'device'"
      type="info"
      :closable="false"
      title="媒体源仅 device 节点可配置"
      description="平台节点（platform-small / platform-large）只接收外部 RTP 流，无需配置本地源。"
      style="margin-bottom:12px"
    />

    <template v-else>
      <el-empty
        v-if="!state.hasConfig"
        description="当前未配置媒体源 — 保存后将作为 RTP 推流源响应 INVITE"
        :image-size="60"
      />

      <el-form label-width="100px" v-else>
        <el-form-item label="类型">
          <el-tag size="small">{{ state.config.kind }}</el-tag>
        </el-form-item>
        <el-form-item label="路径">
          <span>{{ state.config.path || '(无 — synthetic 内置生成)' }}</span>
        </el-form-item>
        <el-form-item label="循环">
          <el-tag v-if="state.config.loop" type="success" size="small">是</el-tag>
          <el-tag v-else type="info" size="small">否</el-tag>
        </el-form-item>
        <el-form-item label="MTU">
          {{ state.config.mtu }}
        </el-form-item>
        <el-form-item label="FPS">
          {{ state.config.fps }}
        </el-form-item>
        <el-form-item label="Clock">
          {{ state.config.clock }}
        </el-form-item>
      </el-form>

      <el-divider />

      <el-form label-width="100px">
        <el-form-item label="源类型">
          <el-select v-model="form.kind">
            <el-option
              v-for="k in sourceKinds"
              :key="k.value"
              :label="k.label"
              :value="k.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="路径">
          <div style="display:flex;gap:8px;width:100%">
            <el-input
              v-model="form.path"
              :disabled="form.kind === 'synthetic'"
              placeholder="synthetic 无需路径；file/RTSP/HLS 必填"
              style="flex:1"
            />
            <el-upload
              v-if="form.kind === 'file'"
              :show-file-list="false"
              :auto-upload="false"
              :on-change="onUploadFile"
              accept=".mp4,.ts,.mkv,.flv,.h264,.h265,.avi,.mov,.webm"
            >
              <el-button :loading="uploading">上传</el-button>
            </el-upload>
          </div>
          <div v-if="form.kind === 'file'" style="font-size:12px;color:#909399;margin-top:4px">
            可手动填写容器内路径，或点击"上传"选择本地 MP4 等文件（≤2GB），上传后自动回填路径
          </div>
        </el-form-item>
        <el-form-item label="循环播放">
          <el-tooltip
            :content="form.kind === 'file' ? '文件播放到末尾后自动从头重播' : '仅本地文件 (file) 源支持循环播放，其他类型无效'"
            placement="top"
          >
            <el-switch v-model="form.loop" :disabled="form.kind !== 'file'" />
          </el-tooltip>
          <span style="margin-left:8px;color:#888;font-size:12px">仅 file 源生效</span>
        </el-form-item>
        <el-form-item label="MTU">
          <el-input-number v-model="form.mtu" :min="100" :max="1500" />
        </el-form-item>
        <el-form-item label="FPS">
          <el-input-number v-model="form.fps" :min="1" :max="60" />
        </el-form-item>
        <el-form-item label="Clock">
          <el-input-number v-model="form.clock" :min="1000" :max="120000" :step="1000" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="save">保存</el-button>
          <el-button type="danger" @click="clear" :disabled="!state.hasConfig">清除</el-button>
        </el-form-item>
      </el-form>
    </template>
  </el-card>
</template>
