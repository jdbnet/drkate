<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import {
  Menu, X, LayoutDashboard, Users, LogOut, Sun, Moon, LoaderCircle, Download,
} from '@lucide/vue'
import { useAuthStore } from '@/stores/auth'
import { applyTheme, useThemeStore } from '@/stores/theme'
import { useDrStore } from '@/stores/dr'
import { usePolling } from '@/composables/usePolling'
import ConfirmDialog from '@/components/ConfirmDialog.vue'

const route = useRoute()
const auth = useAuthStore()
const theme = useThemeStore()
const dr = useDrStore()
const sidebarOpen = ref(false)

const nav = computed(() => {
  const items = [
    { to: '/', label: 'Overview', icon: LayoutDashboard, match: (p: string) => p === '/' },
  ]
  if (auth.me?.role === 'admin') {
    items.push({ to: '/users', label: 'Users', icon: Users, match: (p: string) => p.startsWith('/users') })
  }
  return items
})

const pageTitle = computed(() => {
  if (route.path.startsWith('/namespace/')) return route.params.ns as string
  if (route.path.startsWith('/resource/')) return 'Resource'
  return nav.value.find((n) => n.match(route.path))?.label || 'DrKate'
})

const canScrape = computed(() => auth.me?.role === 'admin' || auth.me?.role === 'operator')

onMounted(() => {
  applyTheme(theme.dark)
  dr.startPolling()
})

usePolling(() => dr.refreshOverview({ background: true }), dr.drPollInterval, { pauseWhenHidden: true, immediate: false })
usePolling(() => dr.refreshScrapeStatus(), dr.scrapePollInterval, { pauseWhenHidden: true, immediate: false })

async function logout() {
  await auth.logout()
  window.location.href = '/login'
}

async function scrape() {
  if (!canScrape.value || dr.scrapeBusy) return
  await dr.runScrape()
}
</script>

<template>
  <div class="flex h-screen overflow-hidden bg-surface">
    <div v-if="sidebarOpen" class="fixed inset-0 z-40 bg-black/50 lg:hidden" @click="sidebarOpen = false" />
    <aside
      class="fixed inset-y-0 left-0 z-50 flex h-screen w-64 shrink-0 flex-col border-r border-default bg-surface-raised transition-transform lg:static lg:translate-x-0"
      :class="sidebarOpen ? 'translate-x-0' : '-translate-x-full'"
    >
      <div class="flex items-center gap-3 border-b border-default p-4">
        <div class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-accent/20 text-sm font-bold text-accent">K8</div>
        <div class="min-w-0 flex-1">
          <div class="truncate text-sm font-semibold text-heading">DrKate</div>
          <div class="text-xs text-muted">k8s disaster recovery</div>
        </div>
        <button type="button" class="text-muted lg:hidden" @click="sidebarOpen = false"><X class="h-5 w-5" /></button>
      </div>
      <nav class="flex-1 overflow-y-auto p-2">
        <RouterLink
          v-for="item in nav"
          :key="item.to"
          :to="item.to"
          class="mb-0.5 flex items-center gap-2 rounded-lg px-3 py-2 text-sm transition"
          :class="item.match(route.path) ? 'nav-item-active' : 'nav-item-inactive'"
          @click="sidebarOpen = false"
        >
          <component :is="item.icon" class="h-4 w-4 shrink-0" />
          {{ item.label }}
        </RouterLink>
      </nav>
      <div class="border-t border-default p-3 space-y-2">
        <button
          v-if="canScrape"
          type="button"
          class="btn-primary w-full"
          :disabled="dr.scrapeBusy"
          @click="scrape"
        >
          <LoaderCircle v-if="dr.scrapeBusy" class="h-4 w-4 animate-spin" />
          <Download v-else class="h-4 w-4" />
          {{ dr.scrapeWaiting ? 'Scraping...' : dr.serverScraping ? 'Scrape running...' : 'Scrape source' }}
        </button>
        <div class="flex items-stretch gap-1.5">
          <button type="button" class="btn-secondary shrink-0 px-2.5 py-2" @click="theme.toggle()">
            <Sun v-if="theme.dark" class="h-4 w-4" />
            <Moon v-else class="h-4 w-4" />
          </button>
          <button type="button" class="btn-secondary min-w-0 flex-1 px-2.5 py-2 text-xs" @click="logout">
            <LogOut class="h-4 w-4 shrink-0" />
            <span>Sign out</span>
          </button>
        </div>
      </div>
    </aside>
    <div class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
      <header class="flex shrink-0 items-center gap-3 border-b border-default bg-surface-raised px-4 py-3 lg:hidden">
        <button type="button" class="text-muted" @click="sidebarOpen = true"><Menu class="h-5 w-5" /></button>
        <span class="truncate font-semibold text-heading">{{ pageTitle }}</span>
      </header>
      <main class="min-h-0 min-w-0 flex-1 overflow-x-hidden overflow-y-auto p-4 md:p-6 text-heading">
        <p v-if="dr.scrapeMessage" class="mb-4 rounded-lg border border-accent/40 bg-accent/10 px-3 py-2 text-sm text-accent-muted">{{ dr.scrapeMessage }}</p>
        <p v-if="dr.scrapeError" class="mb-4 rounded-lg border border-red-500/40 bg-red-500/10 px-3 py-2 text-sm text-red-600 dark:text-red-300">{{ dr.scrapeError }}</p>
        <p v-if="dr.comparing" class="mb-2 text-xs text-muted">Comparing with DR cluster...</p>
        <slot />
      </main>
    </div>
    <ConfirmDialog />
  </div>
</template>
