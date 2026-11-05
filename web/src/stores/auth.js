import { defineStore } from 'pinia'
import { api } from '../api'

export const useAuthStore = defineStore('auth', {
  state: () => ({
    ready: false,
    enabled: false,
    authenticated: true,
    username: ''
  }),
  getters: {
    needsLogin (state) {
      return state.ready && state.enabled && !state.authenticated
    }
  },
  actions: {
    async refresh () {
      const s = await api.authStatus()
      this.enabled = !!s.enabled
      this.authenticated = !!s.authenticated
      this.username = s.username || ''
      this.ready = true
      return s
    },
    async login (username, password) {
      const s = await api.login(username, password)
      this.enabled = !!s.enabled
      this.authenticated = !!s.authenticated
      this.username = s.username || username
      this.ready = true
      if (s.token) {
        sessionStorage.setItem('gbsim_token', s.token)
      }
      return s
    },
    markUnauthenticated () {
      sessionStorage.removeItem('gbsim_token')
      this.authenticated = false
      this.username = ''
    },
    async logout () {
      try {
        await api.logout()
      } finally {
        this.markUnauthenticated()
        if (!this.enabled) this.authenticated = true
      }
    }
  }
})
