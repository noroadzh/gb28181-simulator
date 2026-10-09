<script setup>
import { ref, onMounted, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../api'
import { ElMessage, ElMessageBox } from 'element-plus'

const route = useRoute()
const router = useRouter()
const nodeId = computed(() => route.params.id)

const node = ref(null)
const channels = ref([])
const loading = ref(false)

async function refresh () {
  loading.value = true
  try {
    const [n, cs] = await Promise.all([
      api.getNode(nodeId.value),
      api.listChannels(nodeId.value)
    ])
    node.value = n
    channels.value = cs
  } catch (e) {
    ElMessage.error(`加载失败：${e.message}`)
  } finally {
    loading.value = false
  }
}

function statusType (s) {
  if (s === 'on' || s === 'online') return 'success'
  if (s === 'off' || s === 'offline') return 'info'
  return 'warning'
}

function enter (ch) {
  router.push({ name: 'channel', params: { id: nodeId.value, ch: ch.id } })
}

async function setMedia (ch) {
  const { value } = await ElMessageBox.prompt('输入媒体源 URL（留空清空）', `配置通道 ${ch.id} 媒体源`, {
    confirmButtonText: '保存',
    cancelButtonText: '取消',
    inputValue: '',
    inputPlaceholder: 'rtsp://... 或 file://... 或 https://.../playlist.m3u8'
  }).catch(() => ({}))
  if (value === undefined) return
  try {
    if (!value) {
      await api.deleteChannelMedia(nodeId.value, ch.id)
      ElMessage.success('已清空')
    } else {
      const cfg = parseMediaInput(value)
      await api.putChannelMedia(nodeId.value, ch.id, cfg)
      ElMessage.success('已保存')
    }
    refresh()
  } catch (e) {
    ElMessage.error(`配置失败：${e.message}`)
  }
}

// ─── 上传媒体文件 ─────────────────────────────────────────────────────────────

const uploadDialogVisible = ref(false)
const uploadTarget = ref(null) // 上传后要绑定媒体源的通道
const uploadFile = ref(null)
const uploading = ref(false)

function openUpload (ch) {
  uploadTarget.value = ch
  uploadFile.value = null
  uploadDialogVisible.value = true
}

function onUploadChange (file) {
  uploadFile.value = file.raw || file
}

async function submitUpload () {
  if (!uploadFile.value) {
    ElMessage.warning('请先选择文件')
    return
  }
  uploading.value = true
  try {
    const j = await api.uploadMedia(nodeId.value, uploadFile.value)
    ElMessage.success(`已上传：${j.path}`)
    uploadDialogVisible.value = false
    // 若从通道卡片进入，自动绑定为该通道的媒体源
    if (uploadTarget.value) {
      await api.putChannelMedia(nodeId.value, uploadTarget.value.id, { kind: 'local_file', path: j.path })
      ElMessage.success(`已绑定为通道 ${uploadTarget.value.id} 的媒体源`)
    }
    refresh()
  } catch (e) {
    ElMessage.error(`上传失败：${e.message}`)
  } finally {
    uploading.value = false
  }
}

// ─── 新增 / 删除通道 ─────────────────────────────────────────────────────────

const addChDialogVisible = ref(false)
const addChForm = ref({ id: '', name: '', status: 'ON' })
const addChFormRef = ref(null)

const chIdRule = (rule, value, callback) => {
  if (!/^\d{20}$/.test(value)) callback(new Error('必须是 20 位数字国标编码'))
  else callback()
}

const addChRules = {
  id: [
    { required: true, message: '请输入通道 ID', trigger: 'blur' },
    { validator: chIdRule, trigger: 'blur' }
  ],
  name: [{ required: true, message: '请输入通道名称', trigger: 'blur' }]
}

function openAddChannel () {
  addChForm.value = { id: '', name: '', status: 'ON' }
  addChDialogVisible.value = true
}

async function submitAddChannel () {
  try {
    await addChFormRef.value.validate()
  } catch (_) {
    return
  }
  try {
    await api.addChannel(nodeId.value, addChForm.value)
    ElMessage.success('通道已添加')
    addChDialogVisible.value = false
    refresh()
  } catch (e) {
    ElMessage.error(`添加失败：${e.message}`)
  }
}

async function removeChannel (ch) {
  try {
    await ElMessageBox.confirm(`确定删除通道 ${ch.id} 吗？删除后不可恢复。`, '删除确认', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消'
    })
  } catch (_) {
    return
  }
  try {
    await api.removeChannel(nodeId.value, ch.id)
    ElMessage.success('通道已删除')
    refresh()
  } catch (e) {
    ElMessage.error(`删除失败：${e.message}`)
  }
}

function handleMediaCommand (cmd, ch) {
  if (cmd === 'input') setMedia(ch)
  else if (cmd === 'upload') openUpload(ch)
}

function parseMediaInput (s) {
  if (s.startsWith('rtsp://') || s.startsWith('rtsps://')) return { kind: 'rtsp', url: s }
  if (s.startsWith('http://') || s.startsWith('https://')) {
    if (s.endsWith('.m3u8')) return { kind: 'hls', url: s }
    return { kind: 'remote_file', url: s }
  }
  if (s.startsWith('file://') || s.startsWith('/')) {
    const path = s.replace(/^file:\/\//, '')
    return { kind: 'local_file', path }
  }
  return { kind: 'synthetic', text: s }
}

async function copyFlv (ch) {
  const url = api.flvUrl(nodeId.value, ch.id)
  try {
    await navigator.clipboard.writeText(url)
    ElMessage.success(`已复制：${url}`)
  } catch (_) {
    ElMessage.warning('复制失败，请手动选取')
  }
}

onMounted(refresh)
</script>

<template>
  <el-card shadow="never" v-loading="loading">
    <template #header>
      <div style="display:flex;align-items:center;justify-content:space-between">
        <div>
          <el-button @click="router.back()" size="small" style="margin-right:8px">返回</el-button>
          <b>{{ nodeId }}</b>
          <el-tag v-if="node" :type="statusType(node.status)" size="small" style="margin-left:8px">{{ node.status }}</el-tag>
          <el-tag v-if="node" size="small" type="info" style="margin-left:4px">{{ node.kind }}</el-tag>
        </div>
        <div style="display:flex;gap:8px">
          <el-button @click="refresh" size="small" :loading="loading">刷新</el-button>
          <el-button type="primary" size="small" @click="openAddChannel">新增通道</el-button>
        </div>
      </div>
    </template>

    <el-empty v-if="channels.length === 0" description="该节点无通道" />
    <el-row v-else :gutter="12">
      <el-col :span="6" v-for="ch in channels" :key="ch.id" style="margin-bottom:12px">
        <el-card shadow="hover" @click="enter(ch)" style="cursor:pointer">
          <template #header>
            <div style="display:flex;justify-content:space-between;align-items:center">
              <span>{{ ch.name || ch.id }}</span>
              <el-tag :type="statusType(ch.status)" size="small">{{ ch.status }}</el-tag>
            </div>
          </template>
          <div style="font-size:12px;line-height:1.8;color:#9ca3af">
            <div><b>通道ID：</b>{{ ch.id }}</div>
            <div><b>媒体源：</b>{{ ch.has_media ? '已配置' : '未配置' }}</div>
            <div v-if="ch.online_url"><b>在线：</b>{{ ch.online_url }}</div>
          </div>
          <div style="margin-top:10px;display:flex;gap:6px;flex-wrap:wrap">
            <el-button size="small" type="primary" @click.stop="enter(ch)">播放</el-button>
            <el-dropdown split-button type="default" size="small" @click="setMedia(ch)" @command="(cmd) => handleMediaCommand(cmd, ch)">
              <span>媒体源</span>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item command="input">手动输入地址</el-dropdown-item>
                  <el-dropdown-item command="upload">上传文件</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
            <el-button size="small" type="danger" plain @click.stop="removeChannel(ch)">删除</el-button>
            <el-button size="small" @click.stop="copyFlv(ch)">复制FLV</el-button>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <!-- 上传媒体文件对话框 -->
    <el-dialog
      v-model="uploadDialogVisible"
      :title="uploadTarget ? `上传媒体文件并绑定到通道 ${uploadTarget.id}` : '上传媒体文件'"
      width="480px"
    >
      <el-upload
        drag
        :auto-upload="false"
        :on-change="onUploadChange"
        :limit="1"
        accept=".mp4,.ts,.mkv,.flv,.h264,.h265"
      >
        <el-icon style="font-size:40px;color:#909399;display:flex;justify-content:center;width:100%">+</el-icon>
        <div style="margin-top:8px;color:#606266">拖拽文件到此处，或 <em>点击选择</em></div>
        <template #tip>
          <div style="font-size:12px;color:#909399;margin-top:6px">
            支持 mp4 / ts / mkv / flv / h264 / h265，单文件不超过 2GB
          </div>
        </template>
      </el-upload>
      <template #footer>
        <el-button @click="uploadDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="uploading" @click="submitUpload">上传{{ uploadTarget ? '并绑定' : '' }}</el-button>
      </template>
    </el-dialog>

    <!-- 新增通道对话框 -->
    <el-dialog v-model="addChDialogVisible" title="新增通道" width="460px">
      <el-form ref="addChFormRef" :model="addChForm" :rules="addChRules" label-width="90px">
        <el-form-item label="通道 ID" prop="id">
          <el-input v-model="addChForm.id" placeholder="20 位数字国标编码，如 34020000001320000001" maxlength="20" />
        </el-form-item>
        <el-form-item label="通道名称" prop="name">
          <el-input v-model="addChForm.name" placeholder="如：大门摄像头" maxlength="64" />
        </el-form-item>
        <el-form-item label="状态">
          <el-radio-group v-model="addChForm.status">
            <el-radio label="ON">在线（ON）</el-radio>
            <el-radio label="OFF">离线（OFF）</el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="addChDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="submitAddChannel">添加</el-button>
      </template>
    </el-dialog>
  </el-card>
</template>
