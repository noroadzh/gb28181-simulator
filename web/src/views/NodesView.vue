<script setup>
import { ref, reactive, onMounted, onBeforeUnmount, computed } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useNodeStore } from '../stores/nodes'
import { api } from '../api'

const router = useRouter()
const store = useNodeStore()
const filterKind = ref('')

// 节点控制操作的 loading 状态：每节点一项，按 node.id 索引，
// 避免在卡片被轮询替换时丢失按钮态。
const actionLoading = ref({})

async function refresh () {
  await store.refresh()
}

function goNode (id) {
  router.push({ name: 'capture', params: { id } })
}

// 状态 → el-tag type 映射（design D6）。registering 与 registered 同色
// warning，靠 effect="dark" 与 "light" 区分动态过程与静态就绪。
function statusType (status) {
  switch (status) {
    case 'online': return 'success'
    case 'fault': return 'danger'
    case 'registering': return 'warning'
    case 'registered': return 'warning'
    case 'idle': return 'info'
    case 'offline': return 'info'
    default: return 'info'
  }
}

function statusEffect (status) {
  if (status === 'registering') return 'dark'
  return 'light'
}

// 按钮显隐策略。设计 D1/D5：重试 = 再次 Start，不新增端点；注销按钮
// 按 (kind, status, has_registration) 三条件推导，避免出现点击必然
// 失败的按钮。
const startLabel = (n) => (n.status === 'fault' ? '重试' : '启动')
const canStart = (n) => ['idle', 'offline', 'fault'].includes(n.status)
const canStop = (n) => ['registering', 'registered', 'online'].includes(n.status)
const canUnregister = (n) =>
  (n.kind === 'device' || n.kind === 'platform-small') &&
  n.status === 'online' &&
  n.has_registration === true

async function callApi (node, fn) {
  const key = node.id
  actionLoading.value = { ...actionLoading.value, [key]: true }
  try {
    await fn()
    await refresh()
  } catch (e) {
    ElMessage.error(e.message || '操作失败')
  } finally {
    actionLoading.value = { ...actionLoading.value, [key]: false }
  }
}

function doStart (n) { return callApi(n, () => api.startNode(n.id)) }
function doStop (n) { return callApi(n, () => api.stopNode(n.id)) }
function doUnregister (n) { return callApi(n, () => api.unregisterNode(n.id)) }

// ─── 节点定义 CRUD（runtime-node-configuration 6.1）───────────────────────────
//
// 表单只表达"用户能改的东西"，提交前把 UI 状态翻成 API 的指针语义：
// PUT 中省略一个键 = 保留库里原值。注册密码 API 永不回显，因此编辑时
// 只有勾选"重新配置注册"才会携带 registration —— 否则空密码会被
// model.NewRegistration 拒绝（allow_no_auth=false 时）。

// 注册/平台表单的默认值，与 DB 迁移脚本里的列默认值保持一致，
// 用户不改就落库默认，不会出现 UI 默认与后端默认打架。
function defaultRegistration () {
  return {
    server: '',
    server_id: '',
    username: '',
    password: '',
    gb_version: '2016',
    expires: 3600,
    timeout_ms: 30000,
    transport: 'UDP',
    allow_no_auth: false,
    heartbeat_interval_sec: 60,
    heartbeat_timeout_sec: 30,
    heartbeat_max_failures: 3,
    options_enabled: false
  }
}

function defaultPlatform () {
  return {
    realm: '',
    min_expires: 60,
    default_expires: 3600,
    max_expires: 86400,
    allow_no_auth: false,
    catalog_poll_interval_ms: 0
  }
}

// 级联候选 = 除自身外的全部节点；parent 可清空（拓扑顶端）。
const cascadeCandidates = computed(() => store.list.map(n => n.id))

const createDialog = reactive({ visible: false, saving: false })
const createForm = reactive({})

function openCreate () {
  Object.assign(createForm, {
    id: '',
    kind: 'device',
    domain: '',
    addr: '',
    vendor: '',
    use_registration: false,
    use_platform: false,
    registration: defaultRegistration(),
    platform: defaultPlatform()
  })
  createDialog.visible = true
}

async function submitCreate () {
  if (!createForm.id || !createForm.addr) {
    ElMessage.warning('节点 ID 与监听地址必填')
    return
  }
  if (createForm.use_registration &&
      !createForm.registration.password &&
      !createForm.registration.allow_no_auth) {
    ElMessage.warning('注册密码必填（或勾选允许无认证）')
    return
  }
  createDialog.saving = true
  try {
    const payload = {
      id: createForm.id.trim(),
      kind: createForm.kind,
      domain: createForm.domain.trim(),
      addr: createForm.addr.trim(),
      vendor: createForm.vendor.trim(),
      registration: createForm.use_registration ? stripRegistration(createForm.registration) : null,
      platform: createForm.use_platform ? { ...createForm.platform } : null
    }
    await api.createNode(payload)
    createDialog.visible = false
    ElMessage.success('节点已创建')
    await refresh()
  } catch (e) {
    ElMessage.error(e.message || '创建失败')
  } finally {
    createDialog.saving = false
  }
}

// stripRegistration 去掉前后空白；数字字段来自 el-input-number 已是 Number。
function stripRegistration (r) {
  return {
    ...r,
    server: r.server.trim(),
    server_id: r.server_id.trim(),
    username: r.username.trim(),
    password: r.password
  }
}

const editDialog = reactive({ visible: false, saving: false, id: '' })
const editForm = reactive({
  kind: '', domain: '', addr: '', vendor: '',
  edit_registration: false, registration: defaultRegistration(),
  edit_platform: false, platform: defaultPlatform()
})

async function openEdit (node) {
  let detail = null
  try {
    detail = await api.getNode(node.id)
  } catch (e) {
    ElMessage.error(e.message || '读取节点定义失败')
    return
  }
  Object.assign(editForm, {
    kind: detail.kind || node.kind,
    domain: detail.domain || '',
    addr: detail.addr || node.addr,
    vendor: detail.vendor || '',
    edit_registration: false,
    registration: detail.registration
      ? { ...defaultRegistration(), ...detail.registration }
      : defaultRegistration(),
    edit_platform: false,
    platform: detail.platform
      ? { ...defaultPlatform(), ...detail.platform }
      : defaultPlatform()
  })
  editDialog.id = node.id
  editDialog.visible = true
}

async function submitEdit () {
  if (!editForm.addr) {
    ElMessage.warning('监听地址必填')
    return
  }
  if (editForm.edit_registration && editForm.registration.server &&
      !editForm.registration.password &&
      !editForm.registration.allow_no_auth) {
    ElMessage.warning('修改注册配置需重输密码（API 不回显），或勾选允许无认证')
    return
  }
  editDialog.saving = true
  try {
    const payload = {
      kind: editForm.kind,
      domain: editForm.domain.trim(),
      addr: editForm.addr.trim(),
      vendor: editForm.vendor.trim()
    }
    // 指针语义：未勾选的段省略键 = 保留库中原值（含密码）。
    if (editForm.edit_registration) payload.registration = stripRegistration(editForm.registration)
    if (editForm.edit_platform) payload.platform = { ...editForm.platform }
    await api.updateNode(editDialog.id, payload)
    editDialog.visible = false
    ElMessage.success('节点已更新')
    await refresh()
  } catch (e) {
    ElMessage.error(e.message || '更新失败')
  } finally {
    editDialog.saving = false
  }
}

async function doDelete (node) {
  try {
    await ElMessageBox.confirm(
      `删除节点 ${node.id}？其注册与平台配置将一并删除，运行中的信令栈会停止。`,
      '删除节点',
      { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' }
    )
  } catch (_) {
    return
  }
  actionLoading.value = { ...actionLoading.value, [node.id]: true }
  try {
    await api.deleteNode(node.id)
    ElMessage.success('节点已删除')
    await refresh()
  } catch (e) {
    ElMessage.error(e.message || '删除失败')
  } finally {
    actionLoading.value = { ...actionLoading.value, [node.id]: false }
  }
}

const cascadeDialog = reactive({ visible: false, saving: false, id: '' })
const cascadeForm = reactive({ parent: '', children: [] })

function openCascade (node) {
  cascadeDialog.id = node.id
  cascadeForm.parent = node.cascade_parent || ''
  cascadeForm.children = Array.isArray(node.cascade_children) ? [...node.cascade_children] : []
  cascadeDialog.visible = true
}

async function submitCascade () {
  if (cascadeForm.children.includes(cascadeDialog.id) || cascadeForm.parent === cascadeDialog.id) {
    ElMessage.warning('节点不能把自己设为上级或下级')
    return
  }
  cascadeDialog.saving = true
  try {
    await api.updateNodeCascade(cascadeDialog.id, {
      parent: cascadeForm.parent,
      children: cascadeForm.children
    })
    cascadeDialog.visible = false
    ElMessage.success('级联关系已更新')
    await refresh()
  } catch (e) {
    ElMessage.error(e.message || '级联更新失败')
  } finally {
    cascadeDialog.saving = false
  }
}

const filteredList = computed(() =>
  store.list.filter(n => !filterKind.value || n.kind === filterKind.value)
)

onMounted(refresh)

// 节点列表轮询（design D4）：4 秒间隔，离开页时清理。
let pollHandle = null
onMounted(() => {
  pollHandle = setInterval(refresh, 4000)
})
onBeforeUnmount(() => {
  if (pollHandle) clearInterval(pollHandle)
})
</script>

<template>
  <el-card shadow="never">
    <template #header>
      <div style="display:flex;align-items:center;justify-content:space-between">
        <span><b>节点概览</b></span>
        <div style="display:flex;gap:8px">
          <el-button size="small" @click="refresh" :loading="store.loading">刷新</el-button>
          <el-button type="primary" size="small" @click="openCreate">新建节点</el-button>
        </div>
      </div>
    </template>

    <el-row :gutter="12" style="margin-bottom:12px">
      <el-col :span="6">
        <el-select v-model="filterKind" placeholder="筛选类型" clearable style="width:100%">
          <el-option label="全部" value="" />
          <el-option label="device" value="device" />
          <el-option label="platform-small" value="platform-small" />
          <el-option label="platform-large" value="platform-large" />
        </el-select>
      </el-col>
    </el-row>

    <el-empty v-if="!store.loading && store.list.length === 0" description="暂无节点">
      <el-button type="primary" @click="openCreate">新建节点</el-button>
    </el-empty>
    <el-row :gutter="12" v-else>
      <el-col :span="8" v-for="node in filteredList" :key="node.id" style="margin-bottom:12px">
        <el-card shadow="hover" @click="goNode(node.id)" style="cursor:pointer">
          <template #header>
            <div style="display:flex;justify-content:space-between;align-items:center">
              <span>{{ node.id }}</span>
              <el-tag :type="statusType(node.status)" :effect="statusEffect(node.status)" size="small">
                {{ node.status }}
              </el-tag>
            </div>
          </template>
          <div style="font-size:13px;line-height:2">
            <div><b>类型：</b>{{ node.kind }}</div>
            <div><b>地址：</b>{{ node.addr }}</div>
            <div v-if="node.cascade_parent"><b>上级：</b>{{ node.cascade_parent }}</div>
            <div v-if="node.cascade_children && node.cascade_children.length"><b>下级：</b>{{ node.cascade_children.join(', ') }}</div>
            <div><b>故障计数：</b>{{ Object.values(node.fault_counters || {}).reduce((a, b) => a + b, 0) || 0 }}</div>
          </div>
          <div style="margin-top:8px;display:flex;gap:8px;flex-wrap:wrap" @click.stop>
            <el-button size="small" @click="router.push({ name: 'capture', params: { id: node.id } })">抓包</el-button>
            <el-button size="small" @click="router.push({ name: 'fault', params: { id: node.id } })">故障注入</el-button>
            <el-button
              size="small"
              type="primary"
              @click="router.push({ name: 'channels', params: { id: node.id } })"
            >通道</el-button>
            <el-button
              v-if="node.kind === 'device'"
              size="small"
              type="success"
              @click="router.push({ name: 'media', params: { id: node.id } })"
            >媒体源</el-button>
            <el-button
              v-if="node.kind && node.kind.startsWith('platform')"
              size="small"
              type="warning"
              @click="router.push({ name: 'accounts', params: { id: node.id } })"
            >账号</el-button>
            <el-button
              v-if="canStart(node)"
              size="small"
              type="success"
              :loading="!!actionLoading[node.id]"
              :disabled="!!actionLoading[node.id]"
              @click="doStart(node)"
            >{{ startLabel(node) }}</el-button>
            <el-button
              v-if="canStop(node)"
              size="small"
              :loading="!!actionLoading[node.id]"
              :disabled="!!actionLoading[node.id]"
              @click="doStop(node)"
            >停止</el-button>
            <el-button
              v-if="canUnregister(node)"
              size="small"
              type="warning"
              :loading="!!actionLoading[node.id]"
              :disabled="!!actionLoading[node.id]"
              @click="doUnregister(node)"
            >注销</el-button>
            <el-button size="small" @click="openEdit(node)">编辑</el-button>
            <el-button size="small" @click="openCascade(node)">级联</el-button>
            <el-button size="small" type="danger" plain :loading="!!actionLoading[node.id]" @click="doDelete(node)">删除</el-button>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <!-- 新建节点 -->
    <el-dialog v-model="createDialog.visible" title="新建节点" width="640px" destroy-on-close>
      <el-form label-width="140px" size="small">
        <el-form-item label="节点 ID" required>
          <el-input v-model="createForm.id" placeholder="20 位国标编号，如 34020000011310000002" />
        </el-form-item>
        <el-form-item label="类型">
          <el-select v-model="createForm.kind" style="width:100%">
            <el-option label="device（设备）" value="device" />
            <el-option label="platform-small（下级平台）" value="platform-small" />
            <el-option label="platform-large（上级平台）" value="platform-large" />
          </el-select>
        </el-form-item>
        <el-form-item label="SIP 域">
          <el-input v-model="createForm.domain" placeholder="留空则由 ID 前 8 位推导" />
        </el-form-item>
        <el-form-item label="监听地址" required>
          <el-input v-model="createForm.addr" placeholder="host:port，如 127.0.0.1:15070" />
        </el-form-item>
        <el-form-item label="厂商">
          <el-input v-model="createForm.vendor" placeholder="可选" />
        </el-form-item>

        <el-divider content-position="left">
          <el-checkbox v-model="createForm.use_registration">向上级注册</el-checkbox>
        </el-divider>
        <template v-if="createForm.use_registration">
          <el-form-item label="上级地址" required>
            <el-input v-model="createForm.registration.server" placeholder="host:port" />
          </el-form-item>
          <el-form-item label="上级 ID">
            <el-input v-model="createForm.registration.server_id" placeholder="20 位国标编号" />
          </el-form-item>
          <el-form-item label="认证用户名">
            <el-input v-model="createForm.registration.username" placeholder="默认节点 ID" />
          </el-form-item>
          <el-form-item label="密码">
            <el-input v-model="createForm.registration.password" show-password placeholder="allow_no_auth 关闭时必填" />
          </el-form-item>
          <el-form-item label="国标版本">
            <el-select v-model="createForm.registration.gb_version" style="width:100%">
              <el-option label="GB/T 28181-2016" value="2016" />
              <el-option label="GB/T 28181-2022" value="2022" />
            </el-select>
          </el-form-item>
          <el-form-item label="传输层">
            <el-select v-model="createForm.registration.transport" style="width:100%">
              <el-option label="UDP" value="UDP" />
              <el-option label="TCP" value="TCP" />
            </el-select>
          </el-form-item>
          <el-form-item label="注册有效期（秒）">
            <el-input-number v-model="createForm.registration.expires" :min="1" />
          </el-form-item>
          <el-form-item label="事务超时（毫秒）">
            <el-input-number v-model="createForm.registration.timeout_ms" :min="1000" />
          </el-form-item>
          <el-form-item label="允许无认证">
            <el-switch v-model="createForm.registration.allow_no_auth" />
          </el-form-item>
        </template>

        <el-divider content-position="left">
          <el-checkbox v-model="createForm.use_platform">对下级提供服务</el-checkbox>
        </el-divider>
        <template v-if="createForm.use_platform">
          <el-form-item label="对外 realm">
            <el-input v-model="createForm.platform.realm" placeholder="留空用节点自身域" />
          </el-form-item>
          <el-form-item label="expires 下限（秒）">
            <el-input-number v-model="createForm.platform.min_expires" :min="1" />
          </el-form-item>
          <el-form-item label="expires 默认（秒）">
            <el-input-number v-model="createForm.platform.default_expires" :min="1" />
          </el-form-item>
          <el-form-item label="expires 上限（秒）">
            <el-input-number v-model="createForm.platform.max_expires" :min="1" />
          </el-form-item>
          <el-form-item label="允许无认证">
            <el-switch v-model="createForm.platform.allow_no_auth" />
          </el-form-item>
          <el-form-item label="目录轮询（毫秒）">
            <el-input-number v-model="createForm.platform.catalog_poll_interval_ms" :min="0" :step="1000" />
          </el-form-item>
        </template>
      </el-form>
      <template #footer>
        <el-button @click="createDialog.visible = false">取消</el-button>
        <el-button type="primary" :loading="createDialog.saving" @click="submitCreate">创建</el-button>
      </template>
    </el-dialog>

    <!-- 编辑节点 -->
    <el-dialog v-model="editDialog.visible" :title="`编辑节点 ${editDialog.id}`" width="640px" destroy-on-close>
      <el-form label-width="140px" size="small">
        <el-form-item label="类型">
          <el-select v-model="editForm.kind" style="width:100%">
            <el-option label="device（设备）" value="device" />
            <el-option label="platform-small（下级平台）" value="platform-small" />
            <el-option label="platform-large（上级平台）" value="platform-large" />
          </el-select>
        </el-form-item>
        <el-form-item label="SIP 域">
          <el-input v-model="editForm.domain" />
        </el-form-item>
        <el-form-item label="监听地址" required>
          <el-input v-model="editForm.addr" />
        </el-form-item>
        <el-form-item label="厂商">
          <el-input v-model="editForm.vendor" />
        </el-form-item>

        <el-divider content-position="left">
          <el-checkbox v-model="editForm.edit_registration">重新配置上级注册</el-checkbox>
        </el-divider>
        <el-alert
          v-if="!editForm.edit_registration"
          type="info"
          :closable="false"
          show-icon
          title="不勾选则保留现有注册配置不变（密码不回显，改动需整体重填）"
          style="margin-bottom:12px"
        />
        <template v-if="editForm.edit_registration">
          <el-form-item label="上级地址" required>
            <el-input v-model="editForm.registration.server" placeholder="host:port" />
          </el-form-item>
          <el-form-item label="上级 ID">
            <el-input v-model="editForm.registration.server_id" />
          </el-form-item>
          <el-form-item label="认证用户名">
            <el-input v-model="editForm.registration.username" />
          </el-form-item>
          <el-form-item label="密码">
            <el-input v-model="editForm.registration.password" show-password placeholder="不回显，需重输；allow_no_auth 关闭时必填" />
          </el-form-item>
          <el-form-item label="国标版本">
            <el-select v-model="editForm.registration.gb_version" style="width:100%">
              <el-option label="GB/T 28181-2016" value="2016" />
              <el-option label="GB/T 28181-2022" value="2022" />
            </el-select>
          </el-form-item>
          <el-form-item label="传输层">
            <el-select v-model="editForm.registration.transport" style="width:100%">
              <el-option label="UDP" value="UDP" />
              <el-option label="TCP" value="TCP" />
            </el-select>
          </el-form-item>
          <el-form-item label="注册有效期（秒）">
            <el-input-number v-model="editForm.registration.expires" :min="1" />
          </el-form-item>
          <el-form-item label="事务超时（毫秒）">
            <el-input-number v-model="editForm.registration.timeout_ms" :min="1000" />
          </el-form-item>
          <el-form-item label="允许无认证">
            <el-switch v-model="editForm.registration.allow_no_auth" />
          </el-form-item>
        </template>

        <el-divider content-position="left">
          <el-checkbox v-model="editForm.edit_platform">重新配置下级服务</el-checkbox>
        </el-divider>
        <template v-if="editForm.edit_platform">
          <el-form-item label="对外 realm">
            <el-input v-model="editForm.platform.realm" placeholder="留空用节点自身域" />
          </el-form-item>
          <el-form-item label="expires 下限（秒）">
            <el-input-number v-model="editForm.platform.min_expires" :min="1" />
          </el-form-item>
          <el-form-item label="expires 默认（秒）">
            <el-input-number v-model="editForm.platform.default_expires" :min="1" />
          </el-form-item>
          <el-form-item label="expires 上限（秒）">
            <el-input-number v-model="editForm.platform.max_expires" :min="1" />
          </el-form-item>
          <el-form-item label="允许无认证">
            <el-switch v-model="editForm.platform.allow_no_auth" />
          </el-form-item>
          <el-form-item label="目录轮询（毫秒）">
            <el-input-number v-model="editForm.platform.catalog_poll_interval_ms" :min="0" :step="1000" />
          </el-form-item>
        </template>
      </el-form>
      <template #footer>
        <el-button @click="editDialog.visible = false">取消</el-button>
        <el-button type="primary" :loading="editDialog.saving" @click="submitEdit">保存</el-button>
      </template>
    </el-dialog>

    <!-- 级联编辑 -->
    <el-dialog v-model="cascadeDialog.visible" :title="`级联配置 ${cascadeDialog.id}`" width="520px" destroy-on-close>
      <el-form label-width="100px" size="small">
        <el-form-item label="上级节点">
          <el-select v-model="cascadeForm.parent" clearable placeholder="无（拓扑顶端）" style="width:100%">
            <el-option
              v-for="id in cascadeCandidates.filter(x => x !== cascadeDialog.id)"
              :key="id" :label="id" :value="id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="下级节点">
          <el-select v-model="cascadeForm.children" multiple style="width:100%">
            <el-option
              v-for="id in cascadeCandidates.filter(x => x !== cascadeDialog.id)"
              :key="id" :label="id" :value="id"
            />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="cascadeDialog.visible = false">取消</el-button>
        <el-button type="primary" :loading="cascadeDialog.saving" @click="submitCascade">保存</el-button>
      </template>
    </el-dialog>
  </el-card>
</template>
