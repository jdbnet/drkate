import { defineStore } from 'pinia'

export type ToastKind = 'success' | 'error' | 'info'

export interface Toast {
  id: string
  kind: ToastKind
  title: string
  detail?: string
  persist?: boolean
}

const DEFAULT_MS: Record<ToastKind, number> = {
  success: 8000,
  info: 8000,
  error: 12000,
}

let seq = 0
const timers = new Map<string, ReturnType<typeof setTimeout>>()

export const useToastStore = defineStore('toast', {
  state: () => ({
    items: [] as Toast[],
  }),
  actions: {
    show(kind: ToastKind, title: string, opts: { detail?: string; id?: string; persist?: boolean; ms?: number } = {}) {
      const id = opts.id || `toast-${++seq}`
      this.dismiss(id)
      this.items.push({
        id,
        kind,
        title,
        detail: opts.detail,
        persist: opts.persist,
      })
      if (!opts.persist) {
        const ms = opts.ms ?? DEFAULT_MS[kind]
        timers.set(id, setTimeout(() => this.dismiss(id), ms))
      }
      return id
    },
    success(title: string, opts?: { detail?: string; id?: string; persist?: boolean }) {
      return this.show('success', title, opts)
    },
    error(title: string, opts?: { detail?: string; id?: string; persist?: boolean }) {
      return this.show('error', title, opts)
    },
    info(title: string, opts?: { detail?: string; id?: string; persist?: boolean }) {
      return this.show('info', title, opts)
    },
    dismiss(id: string) {
      const timer = timers.get(id)
      if (timer) {
        clearTimeout(timer)
        timers.delete(id)
      }
      this.items = this.items.filter((t) => t.id !== id)
    },
  },
})
