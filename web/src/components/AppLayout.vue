<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import {
  Menu, X, LogOut, Sun, Moon, LoaderCircle, Download,
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
const menuOpen = ref(false)

const nav = computed(() => {
  const items = [
    { to: '/', label: 'Overview', match: (p: string) => p === '/' || p.startsWith('/namespace/') || p.startsWith('/resource/') },
  ]
  if (auth.me?.role === 'admin') {
    items.push({ to: '/users', label: 'Users', match: (p: string) => p.startsWith('/users') })
  }
  return items
})

const pageTitle = computed(() => {
  if (route.path.startsWith('/namespace/')) return route.params.ns as string
  if (route.path.startsWith('/resource/')) return 'Resource'
  return nav.value.find((n) => n.match(route.path))?.label || 'DrKate'
})

const canScrape = computed(() => auth.me?.role === 'admin' || auth.me?.role === 'operator')

const scrapeLabel = computed(() => {
  if (dr.scrapeWaiting) return 'Scraping...'
  if (dr.serverScraping) return 'Scrape running...'
  return 'Scrape source'
})

watch(() => route.path, () => {
  menuOpen.value = false
})

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
  menuOpen.value = false
  await dr.runScrape()
}
</script>

<template>
  <div class="flex h-screen flex-col overflow-hidden bg-surface">
    <header class="flex h-14 shrink-0 items-center gap-3 border-b border-default bg-surface-raised px-4 md:gap-6 md:px-8">
      <button
        type="button"
        class="focus-ring rounded-lg p-1 text-muted md:hidden"
        :aria-label="menuOpen ? 'Close menu' : 'Open menu'"
        :aria-expanded="menuOpen"
        @click="menuOpen = !menuOpen"
      >
        <X v-if="menuOpen" class="h-5 w-5" />
        <Menu v-else class="h-5 w-5" />
      </button>
      <RouterLink to="/" class="focus-ring flex items-center gap-2.5 rounded-lg">
        <img src="/favicon.png" alt="" class="brand-mark" />
        <span class="text-sm font-semibold tracking-tight text-heading">DrKate</span>
      </RouterLink>
      <nav class="hidden h-full items-stretch md:flex" aria-label="Primary">
        <RouterLink
          v-for="item in nav"
          :key="item.to"
          :to="item.to"
          class="nav-tab"
          :class="item.match(route.path) ? 'nav-tab-active' : 'nav-tab-inactive'"
          :aria-current="item.match(route.path) ? 'page' : undefined"
        >
          {{ item.label }}
        </RouterLink>
      </nav>
      <span class="min-w-0 truncate text-sm font-medium text-heading md:hidden">{{ pageTitle }}</span>
      <div class="ml-auto flex items-center gap-2">
        <button
          v-if="canScrape"
          type="button"
          class="btn-primary hidden sm:inline-flex"
          :disabled="dr.scrapeBusy"
          @click="scrape"
        >
          <LoaderCircle v-if="dr.scrapeBusy" class="h-4 w-4 animate-spin" />
          <Download v-else class="h-4 w-4" />
          {{ scrapeLabel }}
        </button>
        <div v-if="auth.me" class="hidden items-center gap-2 border-l border-default pl-3 lg:flex">
          <span class="text-sm text-heading">{{ auth.me.username }}</span>
          <span class="role-chip">{{ auth.me.role }}</span>
        </div>
        <button
          type="button"
          class="btn-secondary p-1.5"
          :aria-label="theme.dark ? 'Switch to light theme' : 'Switch to dark theme'"
          @click="theme.toggle()"
        >
          <Sun v-if="theme.dark" class="h-4 w-4" />
          <Moon v-else class="h-4 w-4" />
        </button>
        <button type="button" class="btn-secondary hidden text-sm sm:inline-flex" @click="logout">
          <LogOut class="h-4 w-4 shrink-0" />
          Sign out
        </button>
      </div>
    </header>

    <div v-if="menuOpen" class="space-y-1 border-b border-default bg-surface-raised px-4 py-3 md:hidden">
      <RouterLink
        v-for="item in nav"
        :key="item.to"
        :to="item.to"
        class="block rounded-lg px-3 py-2 text-sm"
        :class="item.match(route.path) ? 'bg-accent/15 font-medium text-accent' : 'text-muted'"
        :aria-current="item.match(route.path) ? 'page' : undefined"
      >
        {{ item.label }}
      </RouterLink>
      <div v-if="auth.me" class="flex items-center gap-2 px-3 pt-2 text-sm">
        <span class="text-heading">{{ auth.me.username }}</span>
        <span class="role-chip">{{ auth.me.role }}</span>
      </div>
      <button
        v-if="canScrape"
        type="button"
        class="btn-primary mt-2 w-full"
        :disabled="dr.scrapeBusy"
        @click="scrape"
      >
        <LoaderCircle v-if="dr.scrapeBusy" class="h-4 w-4 animate-spin" />
        <Download v-else class="h-4 w-4" />
        {{ scrapeLabel }}
      </button>
      <button type="button" class="btn-secondary mt-1 w-full" @click="logout">
        <LogOut class="h-4 w-4 shrink-0" />
        Sign out
      </button>
    </div>

    <main class="min-h-0 min-w-0 flex-1 overflow-x-hidden overflow-y-auto p-4 text-heading md:p-8">
      <div class="page-wrap">
        <slot />
      </div>
    </main>
    <ConfirmDialog />
  </div>
</template>
