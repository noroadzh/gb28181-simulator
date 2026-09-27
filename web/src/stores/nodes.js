import { defineStore } from 'pinia'
import { api } from '../api'

export const useNodeStore = defineStore('nodes', {
  state: () => ({
    list: [],
    selectedId: null,
    loading: false,
    error: null
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
    }
  }
})
