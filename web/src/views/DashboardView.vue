<script setup lang="ts">
import { computed, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { useDrStore } from '@/stores/dr'
import { formatTime } from '@/lib/time'

type StatusFilter = 'all' | 'synced' | 'drifted' | 'missing' | 'warnings'

const dr = useDrStore()
const statusFilter = ref<StatusFilter>('all')
const query = ref('')

const totals = computed(() => dr.totals)

const lastScrape = computed(() => formatTime(dr.scrapeStatus?.lastScrapeAt))

const lastCompared = computed(() => formatTime(dr.lastComparedAt))

const titleMeta = computed(() => {
  const parts: string[] = []
  const count = dr.scrapeStatus?.resourceCount
  if (count != null) {
    parts.push(`${count} resources`)
  }
  if (lastScrape.value) {
    parts.push(`last scrape ${lastScrape.value}`)
  }
  return parts
})

const warningTotal = computed(() => dr.overview.reduce((n, ns) => n + (ns.warningCount ?? 0), 0))

const namespaces = computed(() => {
  const q = query.value.trim().toLowerCase()
  return [...dr.overview]
    .sort((a, b) => a.namespace.localeCompare(b.namespace))
    .filter((ns) => {
      if (statusFilter.value === 'warnings') return (ns.warningCount ?? 0) > 0
      if (statusFilter.value !== 'all' && ns[statusFilter.value] === 0) return false
      if (q && !ns.namespace.toLowerCase().includes(q)) return false
      return true
    })
})

function setFilter(next: StatusFilter) {
  statusFilter.value = statusFilter.value === next ? 'all' : next
}

function filterClass(id: StatusFilter) {
  return statusFilter.value === id ? 'filter-tab filter-tab-active' : 'filter-tab filter-tab-inactive'
}

function pct(n: number, d: number) {
  if (d === 0) return 0
  return Math.round((n / d) * 100)
}

function barPct(n: number, d: number) {
  if (d === 0) return 0
  return (n / d) * 100
}

function countClass(n: number, kind: 'synced' | 'drifted' | 'missing' | 'warnings') {
  if (n === 0) return 'text-muted'
  if (kind === 'synced') return 'text-emerald-700 dark:text-emerald-400'
  if (kind === 'drifted' || kind === 'warnings') return 'text-amber-700 dark:text-amber-400'
  return 'text-red-700 dark:text-red-400'
}

function coverageTitle(ns: { synced: number; drifted: number; missing: number; total: number }) {
  return `${pct(ns.synced, ns.total)}% synced · ${ns.synced} synced, ${ns.drifted} drifted, ${ns.missing} missing`
}
</script>

<template>
  <div>
    <div class="mb-6 flex flex-wrap items-end justify-between gap-4">
      <div>
        <h1 class="text-2xl font-semibold tracking-tight">Overview</h1>
        <p v-if="titleMeta.length" class="mt-1 text-sm text-muted">
          {{ titleMeta.join(' · ') }}
        </p>
      </div>
      <input v-model="query" type="search" placeholder="Search namespaces..." class="input-field max-w-xs" />
    </div>

    <div v-if="dr.overviewLoading" class="card space-y-3 p-5" aria-busy="true">
      <div v-for="i in 7" :key="i" class="skeleton h-9" />
    </div>

    <template v-else>
      <div class="mb-4 flex flex-wrap items-center gap-1 border-b border-default pb-3">
        <button type="button" :class="filterClass('all')" @click="setFilter('all')">
          All {{ totals.total }}
        </button>
        <button type="button" :class="filterClass('synced')" @click="setFilter('synced')">
          Synced <span :class="countClass(totals.synced, 'synced')">{{ totals.synced }}</span>
        </button>
        <button type="button" :class="filterClass('drifted')" @click="setFilter('drifted')">
          Drifted <span :class="countClass(totals.drifted, 'drifted')">{{ totals.drifted }}</span>
        </button>
        <button type="button" :class="filterClass('missing')" @click="setFilter('missing')">
          Missing <span :class="countClass(totals.missing, 'missing')">{{ totals.missing }}</span>
        </button>
        <button v-if="warningTotal" type="button" :class="filterClass('warnings')" @click="setFilter('warnings')">
          Warnings <span :class="countClass(warningTotal, 'warnings')">{{ warningTotal }}</span>
        </button>
        <span v-if="lastCompared" class="ml-auto text-xs text-muted">compared {{ lastCompared }}</span>
      </div>

      <div class="card p-5">
        <h2 class="sr-only">
          Namespaces
          <span v-if="statusFilter !== 'all'"> · {{ statusFilter }}</span>
        </h2>
        <div v-if="namespaces.length === 0" class="empty-state">
          <template v-if="dr.overview.length === 0">No data yet. Run a scrape from the header.</template>
          <template v-else>No namespaces match this filter.</template>
        </div>
        <div v-else class="table-scroll">
          <table class="data-table">
            <thead class="text-muted">
              <tr>
                <th>Namespace</th>
                <th>Synced</th>
                <th>Drifted</th>
                <th>Missing</th>
                <th>Warnings</th>
                <th>Total</th>
                <th>Coverage</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="ns in namespaces" :key="ns.namespace" class="table-row-hover">
                <td>
                  <RouterLink :to="`/namespace/${ns.namespace}`" class="font-medium text-heading hover:text-accent">
                    {{ ns.namespace }}
                  </RouterLink>
                </td>
                <td :class="countClass(ns.synced, 'synced')">{{ ns.synced }}</td>
                <td :class="countClass(ns.drifted, 'drifted')">{{ ns.drifted }}</td>
                <td :class="countClass(ns.missing, 'missing')">{{ ns.missing }}</td>
                <td :class="countClass(ns.warningCount ?? 0, 'warnings')">{{ ns.warningCount ?? 0 }}</td>
                <td>{{ ns.total }}</td>
                <td>
                  <div class="flex items-center gap-2.5" :title="coverageTitle(ns)">
                    <div class="coverage-track" aria-hidden="true">
                      <span class="coverage-seg coverage-seg-synced" :style="{ width: barPct(ns.synced, ns.total) + '%' }" />
                      <span class="coverage-seg coverage-seg-drifted" :style="{ width: barPct(ns.drifted, ns.total) + '%' }" />
                      <span class="coverage-seg coverage-seg-missing" :style="{ width: barPct(ns.missing, ns.total) + '%' }" />
                    </div>
                    <span class="w-8 text-right text-xs tabular-nums text-muted">{{ pct(ns.synced, ns.total) }}%</span>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </template>
  </div>
</template>
