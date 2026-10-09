<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useNodeStore } from '../stores/nodes'

const router = useRouter()
const store = useNodeStore()
const filterKind = ref('')

async function refresh () {
  await store.refresh()
}

function goNode (id) {
  router.push({ name: 'capture', params: { id } })
}

onMounted(refresh)
</script>

<template>
  <el-card shadow="never">
    <template #header>
      <div style="display:flex;align-items:center;justify-content:space-between">
        <span><b>节点概览</b></span>
        <el-button type="primary" size="small" @click="refresh" :loading="store.loading">刷新</el-button>
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

    <el-empty v-if="!store.loading && store.list.length === 0" description="暂无节点" />
    <el-row :gutter="12" v-else>
      <el-col :span="8" v-for="node in store.list.filter(n => !filterKind || n.kind === filterKind)" :key="node.id" style="margin-bottom:12px">
        <el-card shadow="hover" @click="goNode(node.id)" style="cursor:pointer">
          <template #header>
            <div style="display:flex;justify-content:space-between;align-items:center">
              <span>{{ node.id }}</span>
              <el-tag :type="node.status === 'online' ? 'success' : (node.status === 'fault' ? 'danger' : 'info')" size="small">{{ node.status }}</el-tag>
            </div>
          </template>
          <div style="font-size:13px;line-height:2">
            <div><b>类型：</b>{{ node.kind }}</div>
            <div><b>地址：</b>{{ node.addr }}</div>
            <div><b>故障计数：</b>{{ Object.values(node.fault_counters || {}).reduce((a, b) => a + b, 0) || 0 }}</div>
          </div>
          <div style="margin-top:8px;display:flex;gap:8px;flex-wrap:wrap">
            <el-button size="small" @click.stop="router.push({ name: 'capture', params: { id: node.id } })">抓包</el-button>
            <el-button size="small" @click.stop="router.push({ name: 'fault', params: { id: node.id } })">故障注入</el-button>
            <el-button
              size="small"
              type="primary"
              @click.stop="router.push({ name: 'channels', params: { id: node.id } })"
            >通道</el-button>
            <el-button
              v-if="node.kind === 'device'"
              size="small"
              type="success"
              @click.stop="router.push({ name: 'media', params: { id: node.id } })"
            >媒体源</el-button>
            <el-button
              v-if="node.kind && node.kind.startsWith('platform')"
              size="small"
              type="warning"
              @click.stop="router.push({ name: 'accounts', params: { id: node.id } })"
            >账号</el-button>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </el-card>
</template>
