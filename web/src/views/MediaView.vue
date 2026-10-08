<script setup>
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useNodeStore } from '../stores/nodes'
import MediaPanel from './MediaPanel.vue'

const route = useRoute()
const router = useRouter()
const store = useNodeStore()

const nodeId = computed(() => route.params.id || store.selectedId)
const node = computed(() => store.list.find(n => n.id === nodeId.value) || null)
</script>

<template>
  <div>
    <div style="margin-bottom:12px">
      <el-button size="small" @click="router.push({ name: 'nodes' })">← 返回节点概览</el-button>
      <span v-if="nodeId" style="margin-left:12px;color:#666">节点：{{ nodeId }}</span>
    </div>

    <el-empty v-if="!nodeId" description="请从节点概览页进入媒体源配置" />
    <MediaPanel v-else-if="node" :node-id="nodeId" :kind="node.kind" />
    <el-empty v-else description="节点未找到" />
  </div>
</template>
