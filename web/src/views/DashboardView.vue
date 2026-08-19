<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import { useDrStore } from '@/stores/dr'

const dr = useDrStore()

const totals = computed(() => dr.totals)

const lastScrape = computed(() => {
  const at = dr.scrapeStatus?.lastScrapeAt
  if (!at) return 'Never'
  return new Date(at).toLocaleString()
})

const lastCompared = computed(() => {
  if (!dr.lastComparedAt) return 'Not yet'
  return new Date(dr.lastComparedAt).toLocaleString()
})

function pct(n: number, d: number) {
  if (d === 0) return 0
  return Math.round((n / d) * 100)
}
</script>

<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <h1 class="text-xl font-semibold">Overview</h1>
        <p class="mt-1 text-sm text-muted">
          DR coverage vs stored manifests
          <span v-if="dr.scrapeStatus"> · Last scrape {{ lastScrape }}</span>
          <span v-if="dr.scrapeStatus"> · {{ dr.scrapeStatus.resourceCount }} resources</span>
          <span> · DR compared {{ lastCompared }}</span>
          <span v-if="dr.comparing"> · comparing now...</span>
        </p>
      </div>
    </div>

    <div v-if="dr.overviewLoading" class="text-sm text-muted">Loading DR status...</div>

    <div v-else class="grid gap-4 md:grid-cols-4">
      <div class="card">
        <div class="text-sm text-muted">Total</div>
        <div class="text-2xl font-semibold">{{ totals.total }}</div>
      </div>
      <div class="card">
        <div class="text-sm text-muted">Synced</div>
        <div class="text-2xl font-semibold text-emerald-600 dark:text-emerald-400">{{ totals.synced }}</div>
        <div class="text-xs text-muted">{{ pct(totals.synced, totals.total) }}%</div>
      </div>
      <div class="card">
        <div class="text-sm text-muted">Drifted</div>
        <div class="text-2xl font-semibold text-amber-600 dark:text-amber-400">{{ totals.drifted }}</div>
        <div class="text-xs text-muted">{{ pct(totals.drifted, totals.total) }}%</div>
      </div>
      <div class="card">
        <div class="text-sm text-muted">Missing</div>
        <div class="text-2xl font-semibold text-red-600 dark:text-red-400">{{ totals.missing }}</div>
        <div class="text-xs text-muted">{{ pct(totals.missing, totals.total) }}%</div>
      </div>
    </div>

    <div class="card">
      <h2 class="mb-3 text-sm font-medium text-heading">Namespaces</h2>
      <div class="table-scroll">
        <table class="data-table">
          <thead class="text-muted">
            <tr>
              <th>Namespace</th>
              <th>Synced</th>
              <th>Drifted</th>
              <th>Missing</th>
              <th>Total</th>
              <th>Coverage</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="ns in dr.overview" :key="ns.namespace" class="table-row-hover">
              <td>
                <RouterLink :to="`/namespace/${ns.namespace}`" class="text-accent hover:underline">
                  {{ ns.namespace }}
                </RouterLink>
              </td>
              <td>{{ ns.synced }}</td>
              <td>{{ ns.drifted }}</td>
              <td>{{ ns.missing }}</td>
              <td>{{ ns.total }}</td>
              <td>{{ pct(ns.synced, ns.total) }}%</td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-if="!dr.overviewLoading && dr.overview.length === 0" class="mt-3 text-sm text-muted">
        No data yet. Run a scrape from the sidebar.
      </p>
    </div>
  </div>
</template>
