import { defineStore } from 'pinia'

const STORAGE_KEY = 'gb28181.ui.roleMode'

// roleMode: 'all' | 'platform' | 'device'
// Single shell; only the default filter / menu emphasis changes.
export const useRoleStore = defineStore('role', {
  state: () => ({
    mode: localStorage.getItem(STORAGE_KEY) || 'all'
  }),
  getters: {
    label (state) {
      switch (state.mode) {
        case 'platform': return '平台视角'
        case 'device': return '设备视角'
        default: return '全部角色'
      }
    },
    isPlatformFocused (state) { return state.mode === 'platform' },
    isDeviceFocused (state) { return state.mode === 'device' }
  },
  actions: {
    setMode (mode) {
      if (!['all', 'platform', 'device'].includes(mode)) mode = 'all'
      this.mode = mode
      localStorage.setItem(STORAGE_KEY, mode)
    },
    nodeMatches (kind) {
      const k = String(kind || '')
      if (this.mode === 'platform') return k.startsWith('platform')
      if (this.mode === 'device') return k === 'device'
      return true
    },
    menuVisible (item) {
      // item.roles: ['platform','device'] — omit means always visible
      if (!item.roles || !item.roles.length) return true
      if (this.mode === 'all') return true
      return item.roles.includes(this.mode)
    }
  }
})
