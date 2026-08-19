<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { Pencil } from '@lucide/vue'
import { useDrStore } from '@/stores/dr'
import { useAuthStore } from '@/stores/auth'
import { useIntervalWhile } from '@/composables/usePolling'
import { confirm } from '@/lib/confirm'

const route = useRoute()
const router = useRouter()
const dr = useDrStore()
const auth = useAuthStore()
const ns = route.params.ns as string

const filter = ref('')
const statusFilter = ref('')

const resources = computed(() => dr.namespaceDetail[ns] || [])
const loading = computed(() => dr.namespaceLoading[ns] ?? false)
const refreshing = computed(() => dr.comparing)
const canDeploy = computed(() => auth.me?.role === 'admin' || auth.me?.role === 'operator')

const filtered = computed(() => {
  return resources.value.filter((r) => {
    if (statusFilter.value && r.status !== statusFilter.value) return false
    const q = filter.value.toLowerCase()
    if (!q) return true
    return r.kind.toLowerCase().includes(q) || r.name.toLowerCase().includes(q)
  })
})

onMounted(() => {
  dr.ensureNamespace(ns)
})

useIntervalWhile(() => dr.refreshNamespace(ns, { background: true }), dr.drPollInterval, () => true)

function statusBadge(status: string) {
  if (status === 'synced') return 'badge-running'
  if (status === 'drifted') return 'badge-warn'
  return 'badge-error'
}

function goResource(kind: string, name: string) {
  router.push(`/resource/${ns}/${kind}/${name}`)
}

async function deploy(filter: string) {
  if (!canDeploy.value) return
  const label = filter === 'all' ? 'all resources' : filter
  if (!await confirm({
    title: 'Deploy to DR?',
    message: `Deploy ${label} in namespace ${ns} to the DR cluster.`,
    confirmLabel: 'Deploy',
  })) return
  try {
    const result = await dr.deploy({ namespace: ns, filter })
    const failed = (result as { failed?: string[] }).failed?.length ?? 0
    const ok = (result as { success?: string[] }).success?.length ?? 0
    dr.scrapeMessage = `Deploy complete: ${ok} succeeded, ${failed} failed`
  } catch (e: unknown) {
    const err = e as { response?: { data?: string } }
    dr.scrapeError = err.response?.data || 'Deploy failed'
  }
}
</script>

<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <RouterLink to="/" class="text-sm text-muted hover:text-heading">Overview</RouterLink>
        <h1 class="text-xl font-semibold">{{ ns }}</h1>
        <p v-if="refreshing" class="mt-1 text-xs text-muted">Comparing with DR cluster...</p>
      </div>
      <div v-if="canDeploy" class="flex flex-wrap gap-2">
        <button type="button" class="btn-secondary" @click="deploy('missing')">Deploy missing</button>
        <button type="button" class="btn-secondary" @click="deploy('drifted')">Deploy drifted</button>
        <button type="button" class="btn-primary" @click="deploy('all')">Deploy all</button>
      </div>
    </div>

    <div class="flex flex-wrap gap-2">
      <input v-model="filter" placeholder="Filter..." class="input-field max-w-xs" />
      <select v-model="statusFilter" class="input-field max-w-xs">
        <option value="">All statuses</option>
        <option value="synced">Synced</option>
        <option value="drifted">Drifted</option>
        <option value="missing">Missing</option>
      </select>
    </div>

    <div v-if="loading" class="text-sm text-muted">Loading...</div>

    <div v-else class="card">
      <div class="table-scroll">
        <table class="data-table">
          <thead class="text-muted">
            <tr>
              <th>Kind</th>
              <th>Name</th>
              <th>Status</th>
              <th>Last scraped</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="r in filtered" :key="r.kind + '/' + r.name" class="table-row-hover">
              <td>{{ r.kind }}</td>
              <td class="font-mono text-sm">{{ r.name }}</td>
              <td><span class="badge" :class="statusBadge(r.status)">{{ r.status }}</span></td>
              <td class="text-muted">{{ r.scrapedAt || '—' }}</td>
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
