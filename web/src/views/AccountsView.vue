<script setup>
import { ref, onMounted, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../api'
import { ElMessage, ElMessageBox } from 'element-plus'

const route = useRoute()
const router = useRouter()

const nodes = ref([])
const nodeId = ref(route.params.id || '')
const accounts = ref([])
const loading = ref(false)

// 新增账号对话框
const addDialogVisible = ref(false)
const addForm = ref({ username: '', password: '', confirm: '' })
const addFormRef = ref(null)

// 改密码对话框
const pwdDialogVisible = ref(false)
const pwdForm = ref({ username: '', password: '', confirm: '' })
const pwdFormRef = ref(null)

const isPlatform = computed(() => {
  const n = nodes.value.find((n) => n.id === nodeId.value)
  return n && n.kind && n.kind.startsWith('platform')
})

const validateUsername = (rule, value, callback) => {
  if (!/^\d{20}$/.test(value)) {
    callback(new Error('必须是 20 位数字的国标编码'))
  } else {
    callback()
  }
}

const validateAddConfirm = (rule, value, callback) => {
  if (value !== addForm.value.password) {
    callback(new Error('两次输入的密码不一致'))
  } else {
    callback()
  }
}

const validatePwdConfirm = (rule, value, callback) => {
  if (value !== pwdForm.value.password) {
    callback(new Error('两次输入的密码不一致'))
  } else {
    callback()
  }
}

const addRules = {
  username: [
    { required: true, message: '请输入用户名（20 位国标编码）', trigger: 'blur' },
    { validator: validateUsername, trigger: 'blur' }
  ],
  password: [{ required: true, message: '请输入密码', trigger: 'blur' }],
  confirm: [{ required: true, message: '请再次输入密码', trigger: 'blur' }, { validator: validateAddConfirm, trigger: 'blur' }]
}

const pwdRules = {
  password: [{ required: true, message: '请输入新密码', trigger: 'blur' }],
  confirm: [{ required: true, message: '请再次输入新密码', trigger: 'blur' }, { validator: validatePwdConfirm, trigger: 'blur' }]
}

async function loadNodes () {
  try {
    const j = await api.listNodes()
    nodes.value = j.nodes || []
    // 默认选中第一个 platform 节点
    if (!nodeId.value) {
      const p = nodes.value.find((n) => n.kind && n.kind.startsWith('platform'))
      if (p) nodeId.value = p.id
    }
    if (nodeId.value) loadAccounts()
  } catch (e) {
    ElMessage.error('加载节点失败: ' + e.message)
  }
}

async function loadAccounts () {
  if (!nodeId.value) return
  loading.value = true
  try {
    const j = await api.listAccounts(nodeId.value)
    accounts.value = j.accounts || []
  } catch (e) {
    ElMessage.error('加载账号失败: ' + e.message)
  } finally {
    loading.value = false
  }
}

function openAdd () {
  addForm.value = { username: '', password: '', confirm: '' }
  addDialogVisible.value = true
}

async function submitAdd () {
  try {
    await addFormRef.value.validate()
  } catch (_) {
    return
  }
  try {
    await api.addAccount(nodeId.value, addForm.value.username, addForm.value.password)
    ElMessage.success('账号已添加')
    addDialogVisible.value = false
    loadAccounts()
  } catch (e) {
    ElMessage.error('添加失败: ' + e.message)
  }
}

async function removeAccount (row) {
  try {
    await ElMessageBox.confirm(`确定删除账号 ${row.username} 吗？删除后该设备将无法再注册。`, '删除确认', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消'
    })
  } catch (_) {
    return
  }
  try {
    await api.removeAccount(nodeId.value, row.username)
    ElMessage.success('账号已删除')
    loadAccounts()
  } catch (e) {
    ElMessage.error('删除失败: ' + e.message)
  }
}

function openPwd (row) {
  pwdForm.value = { username: row.username, password: '', confirm: '' }
  pwdDialogVisible.value = true
}

async function submitPwd () {
  try {
    await pwdFormRef.value.validate()
  } catch (_) {
    return
  }
  try {
    await api.setAccountPassword(nodeId.value, pwdForm.value.username, pwdForm.value.password)
    ElMessage.success('密码已更新')
    pwdDialogVisible.value = false
  } catch (e) {
    ElMessage.error('更新失败: ' + e.message)
  }
}

onMounted(loadNodes)
</script>

<template>
  <el-card shadow="never">
    <template #header>
      <div style="display:flex;align-items:center;justify-content:space-between">
        <span style="font-weight:600">平台账号管理</span>
        <div style="display:flex;gap:8px;align-items:center">
          <el-select
            v-model="nodeId"
            placeholder="选择平台节点"
            style="width:280px"
            @change="loadAccounts"
          >
            <el-option
              v-for="n in nodes.filter(n => n.kind && n.kind.startsWith('platform'))"
              :key="n.id"
              :label="`${n.id}（${n.kind}）`"
              :value="n.id"
            />
          </el-select>
          <el-button @click="loadAccounts">刷新</el-button>
          <el-button type="primary" :disabled="!nodeId" @click="openAdd">新增账号</el-button>
        </div>
      </div>
    </template>

    <el-alert
      v-if="nodeId && !isPlatform"
      type="warning"
      :closable="false"
      title="当前选中的不是平台节点，请选择 platform 类型节点"
      style="margin-bottom:12px"
    />

    <el-alert
      type="info"
      :closable="false"
      style="margin-bottom:12px"
    >
      账号用于第三方设备/系统通过 SIP REGISTER 注册到本平台。此处新增的账号保存在
      SQLite 中，重启后仍然生效；运行时改动即时生效，无需重启。
    </el-alert>

    <el-table :data="accounts" v-loading="loading" style="width:100%" empty-text="暂无账号">
      <el-table-column prop="Username" label="用户名（20 位国标编码）" min-width="240" />
      <el-table-column prop="CreatedAt" label="创建时间" width="200">
        <template #default="{ row }">
          {{ (row.CreatedAt || row.created_at || '').replace('T', ' ').slice(0, 19) || '—' }}
        </template>
      </el-table-column>
      <el-table-column label="操作" width="160">
        <template #default="{ row }">
          <el-button size="small" @click="openPwd(row)">改密码</el-button>
          <el-button size="small" type="danger" plain @click="removeAccount(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="addDialogVisible" title="新增账号" width="440px">
      <el-form ref="addFormRef" :model="addForm" :rules="addRules" label-width="90px">
        <el-form-item label="用户名" prop="username">
          <el-input v-model="addForm.username" placeholder="20 位数字国标编码，如 34020000001310000001" maxlength="20" />
        </el-form-item>
        <el-form-item label="密码" prop="password">
          <el-input v-model="addForm.password" type="password" show-password placeholder="SIP 注册密码" />
        </el-form-item>
        <el-form-item label="确认密码" prop="confirm">
          <el-input v-model="addForm.confirm" type="password" show-password placeholder="再次输入密码" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="addDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="submitAdd">添加</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="pwdDialogVisible" :title="`修改密码：${pwdForm.username}`" width="440px">
      <el-form ref="pwdFormRef" :model="pwdForm" :rules="pwdRules" label-width="90px">
        <el-form-item label="新密码" prop="password">
          <el-input v-model="pwdForm.password" type="password" show-password placeholder="新密码" />
        </el-form-item>
        <el-form-item label="确认密码" prop="confirm">
          <el-input v-model="pwdForm.confirm" type="password" show-password placeholder="再次输入新密码" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="pwdDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="submitPwd">更新</el-button>
      </template>
    </el-dialog>
  </el-card>
</template>
