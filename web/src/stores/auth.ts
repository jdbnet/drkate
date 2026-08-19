import { defineStore } from 'pinia'
import api, { type User } from '@/api/client'

export const useAuthStore = defineStore('auth', {
  state: () => ({
    authenticated: false,
    checked: false,
    me: null as User | null,
  }),
  actions: {
    async check() {
      if (this.checked && this.authenticated) {
        return
      }
      try {
        const { data } = await api.get<User>('/auth/me')
        this.me = data
        this.authenticated = true
      } catch {
        this.me = null
        this.authenticated = false
      }
      this.checked = true
    },
    async login(username: string, password: string) {
      const { data } = await api.post<User>('/auth/login', { username, password })
      this.me = data
      this.authenticated = true
      this.checked = true
    },
    async logout() {
      await api.post('/auth/logout')
      this.authenticated = false
      this.me = null
      this.checked = true
    },
  },
})
