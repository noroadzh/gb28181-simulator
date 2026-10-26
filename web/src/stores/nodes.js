import { defineStore } from 'pinia'
import { api } from '../api'

const RECONNECT_DELAY_MS = 3000

export const useNodeStore = defineStore('nodes', {
  state: () => ({
    list: [],
    selectedId: null,
    loading: false,
    error: null,
    // WS 状态推送连接。socket 与重连句柄都收在 store 里，保证多组件
    // 共享一条连接；connected 供 UI 显示实时/轮询模式。
    ws: null,
    reconnectHandle: null,
    connected: false
  }),
  getters: {
    selected (state) {
      return state.list.find(n => n.id === state.selectedId) || null
    },
    kinds () {
      const set = new Set(this.list.map(n => n.kind))
      return Array.from(set)
    }
  },
  actions: {
    async refresh () {
      this.loading = true
      this.error = null
      try {
        this.list = await api.listNodes()
      } catch (e) {
        this.error = e.message
      } finally {
        this.loading = false
      }
    },
    select (id) {
      this.selectedId = id
    },
    async refreshDetail (id) {
      this.select(id)
      try {
        const node = await api.getNode(id)
        const idx = this.list.findIndex(n => n.id === id)
        if (idx >= 0) this.list[idx] = node
        else this.list.push(node)
        return node
      } catch (e) {
        this.error = e.message
      }
    },

    // ─── 节点状态 WS 推送 ────────────────────────────────────────────────────
    //
    // 后端事件：node_status_changed（改本地 status）、node_created /
    // node_updated（整体 refresh，保证派生字段一致）、node_deleted（本地
    // 移除）。断线 3 秒后自动重连；WS 挂掉时退回现有 4 秒轮询兜底。

    connectStream () {
      if (this.ws || this.reconnectHandle) return
      this.ws = api.nodesStream(ev => this.applyEvent(ev), () => this.onStreamClose())
      this.connected = true
    },
    disconnectStream () {
      if (this.reconnectHandle) {
        clearTimeout(this.reconnectHandle)
        this.reconnectHandle = null
      }
      if (this.ws) {
        const ws = this.ws
        this.ws = null
        this.connected = false
        ws.onclose = null // 主动关闭不触发重连
        ws.close()
      }
    },
    onStreamClose () {
      this.ws = null
      this.connected = false
      if (!this.reconnectHandle) {
        this.reconnectHandle = setTimeout(() => {
          this.reconnectHandle = null
          this.connectStream()
        }, RECONNECT_DELAY_MS)
      }
    },
    applyEvent (ev) {
      if (!ev || !ev.type) return
      switch (ev.type) {
        case 'node_status_changed': {
          const idx = this.list.findIndex(n => n.id === ev.id)
          if (idx >= 0) this.list[idx] = { ...this.list[idx], status: ev.status }
          else this.refresh() // 未知节点：整体刷新补齐
          break
        }
        case 'node_deleted': {
          this.list = this.list.filter(n => n.id !== ev.id)
          if (this.selectedId === ev.id) this.selectedId = null
          break
        }
        // created / updated 携带不了完整定义（密码等敏感字段不下发），
        // 直接整体 refresh 最省事也最不会错。
        case 'node_created':
        case 'node_updated':
          this.refresh()
          break
      }
    }
  }
})
