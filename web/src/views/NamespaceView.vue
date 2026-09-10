<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { ChevronRight, Pencil, TriangleAlert } from '@lucide/vue'
import { useDrStore } from '@/stores/dr'
import { useAuthStore } from '@/stores/auth'
import { useIntervalWhile } from '@/composables/usePolling'
import { confirm } from '@/lib/confirm'
import { formatTime } from '@/lib/time'
import { useToastStore } from '@/stores/toast'

const route = useRoute()
const router = useRouter()
const dr = useDrStore()
const auth = useAuthStore()
const ns = computed(() => route.params.ns as string)

type StatusFilter = '' | 'synced' | 'drifted' | 'missing' | 'pending' | 'warnings'

const filter = ref('')
const statusFilter = ref<StatusFilter>('')

const resources = computed(() => dr.namespaceDetail[ns.value] || [])
const loading = computed(() => dr.namespaceLoading[ns.value] ?? false)
const canDeploy = computed(() => auth.me?.role === 'admin' || auth.me?.role === 'operator')

const warningResources = computed(() => resources.value.filter((r) => (r.warnings?.length ?? 0) > 0))

const warningSummary = computed(() => {
  let storage = 0
  let ingress = 0
  for (const r of warningResources.value) {
    for (const w of r.warnings || []) {
      if (w.code === 'ingress') ingress++
      else storage++
    }
  }
  return { resources: warningResources.value.length, storage, ingress }
})

const filtered = computed(() => {
  return resources.value.filter((r) => {
    if (statusFilter.value === 'warnings') {
      if (!(r.warnings?.length)) return false
    } else if (statusFilter.value && r.status !== statusFilter.value) {
      return false
    }
    const q = filter.value.toLowerCase()
    if (!q) return true
    return r.kind.toLowerCase().includes(q) || r.name.toLowerCase().includes(q)
  })
})

watch(ns, (n) => {
  dr.refreshNamespace(n)
}, { immediate: true })

useIntervalWhile(() => dr.refreshNamespace(ns.value, { background: true }), dr.drPollInterval, () => true)

const statusCounts = computed(() => {
  const counts = { all: resources.value.length, synced: 0, drifted: 0, missing: 0, pending: 0 }
  for (const r of resources.value) {
    if (r.status in counts) counts[r.status as keyof typeof counts] += 1
  }
  return counts
})

function setStatus(next: StatusFilter) {
  statusFilter.value = statusFilter.value === next ? '' : next
}

function filterClass(id: StatusFilter) {
  const active = statusFilter.value === id
  return active ? 'filter-tab filter-tab-active' : 'filter-tab filter-tab-inactive'
}

function statusBadge(status: string) {
  if (status === 'synced') return 'badge-synced'
  if (status === 'drifted') return 'badge-drifted'
  if (status === 'pending') return 'badge-pending'
  return 'badge-missing'
}

function warningTitle(r: { warnings?: { message: string }[] }) {
  return (r.warnings || []).map((w) => w.message).join('\n')
}

function goResource(kind: string, name: string) {
  router.push(`/resource/${ns.value}/${kind}/${name}`)
}

async function deploy(filter: string) {
  if (!canDeploy.value) return
  const label = filter === 'all' ? 'all resources' : filter
  if (!await confirm({
    title: 'Deploy to DR?',
    message: `Deploy ${label} in namespace ${ns.value} to the DR cluster.`,
    confirmLabel: 'Deploy',
  })) return
  try {
    await dr.deploy({ namespace: ns.value, filter })
    await dr.refreshNamespace(ns.value)
  } catch (e: unknown) {
    const err = e as { response?: { data?: string } }
    useToastStore().error(err.response?.data || 'Deploy failed')
  }
}
</script>

<template>
  <div>
    <div class="mb-6 flex flex-wrap items-start justify-between gap-3">
      <div>
        <nav class="crumb" aria-label="Breadcrumb">
          <RouterLink to="/">Overview</RouterLink>
          <ChevronRight class="h-3.5 w-3.5" />
        </nav>
        <h1 class="text-2xl font-semibold tracking-tight">{{ ns }}</h1>
      </div>
      <div v-if="canDeploy" class="flex flex-wrap gap-2">
        <button type="button" class="btn-secondary" @click="deploy('missing')">Deploy missing</button>
        <button type="button" class="btn-secondary" @click="deploy('drifted')">Deploy drifted</button>
        <button type="button" class="btn-primary" @click="deploy('all')">Deploy all</button>
      </div>
    </div>

    <div class="mb-4 flex flex-wrap items-center gap-2">
      <input v-model="filter" placeholder="Filter kind or name..." class="input-field max-w-xs" />
      <div class="flex flex-wrap gap-1">
        <button type="button" :class="filterClass('')" @click="setStatus('')">
          All {{ statusCounts.all }}
        </button>
        <button type="button" :class="filterClass('synced')" @click="setStatus('synced')">
          Synced {{ statusCounts.synced }}
        </button>
        <button type="button" :class="filterClass('drifted')" @click="setStatus('drifted')">
          Drifted {{ statusCounts.drifted }}
        </button>
        <button type="button" :class="filterClass('missing')" @click="setStatus('missing')">
          Missing {{ statusCounts.missing }}
        </button>
        <button v-if="statusCounts.pending" type="button" :class="filterClass('pending')" @click="setStatus('pending')">
          Pending {{ statusCounts.pending }}
        </button>
        <button v-if="warningSummary.resources" type="button" :class="filterClass('warnings')" @click="setStatus('warnings')">
          Warnings {{ warningSummary.resources }}
        </button>
      </div>
    </div>

    <div v-if="!loading && warningSummary.resources" class="callout-warn">
      <div class="flex gap-3">
        <TriangleAlert class="mt-0.5 h-4 w-4 shrink-0 text-amber-600 dark:text-amber-400" />
        <div class="min-w-0">
          <p class="font-medium text-heading">DR caveats</p>
          <p class="mt-1 text-muted">
            {{ warningSummary.resources }} resource{{ warningSummary.resources === 1 ? '' : 's' }} may not restore the same way on DR.
            Deploy copies manifests only: disks stay behind, and controller-specific ingress settings may not apply.
          </p>
          <p class="mt-1 text-muted">
            <span v-if="warningSummary.storage">{{ warningSummary.storage }} storage</span>
            <span v-if="warningSummary.storage && warningSummary.ingress"> · </span>
            <span v-if="warningSummary.ingress">{{ warningSummary.ingress }} ingress</span>
          </p>
          <details class="mt-2">
            <summary class="cursor-pointer text-heading">Show details</summary>
            <ul class="mt-2 space-y-1.5">
              <li v-for="r in warningResources" :key="r.kind + '/' + r.name">
                <button type="button" class="text-left hover:text-accent" @click="goResource(r.kind, r.name)">
                  <span class="font-medium">{{ r.kind }}/{{ r.name }}</span>
                  <span class="text-muted"> · {{ r.warnings?.[0]?.message }}</span>
                </button>
              </li>
            </ul>
          </details>
        </div>
      </div>
    </div>

    <div v-if="loading" class="card space-y-3 p-5" aria-busy="true">
      <div v-for="i in 6" :key="i" class="skeleton h-9" />
    </div>

    <div v-else class="card p-5">
      <div v-if="filtered.length === 0" class="empty-state">
        <template v-if="resources.length === 0">No resources in this namespace.</template>
        <template v-else>No resources match this filter.</template>
      </div>
      <div v-else class="table-scroll">
        <table class="data-table">
          <thead class="text-muted">
            <tr>
              <th>Kind</th>
              <th>Name</th>
              <th>Status</th>
              <th>Drift</th>
              <th>Last scraped</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="r in filtered" :key="r.kind + '/' + r.name" class="table-row-hover">
              <td>
                <span class="inline-flex items-center gap-1.5">
                  {{ r.kind }}
                  <TriangleAlert
                    v-if="r.warnings?.length"
                    class="h-3.5 w-3.5 text-amber-600 dark:text-amber-400"
                    :title="warningTitle(r)"
                  />
                </span>
              </td>
              <td class="font-mono text-sm">
                <span class="inline-flex items-center gap-2">
                  {{ r.name }}
                  <span v-if="r.edited" class="badge badge-warn">edited</span>
                </span>
              </td>
              <td><span class="badge" :class="statusBadge(r.status)">{{ r.status }}</span></td>
              <td class="max-w-xs truncate text-muted" :title="r.drift || ''">{{ r.drift || '—' }}</td>
              <td class="text-muted">{{ formatTime(r.scrapedAt) || '—' }}</td>
              <td class="text-right">
                <button type="button" class="btn-row btn-row-edit" @click="goResource(r.kind, r.name)">
                  <Pencil class="h-3.5 w-3.5" />
                  Edit
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>
