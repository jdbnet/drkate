<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import loader from '@monaco-editor/loader'
import api from '@/api/client'
import { useDrStore } from '@/stores/dr'
import { useAuthStore } from '@/stores/auth'
import { useIntervalWhile } from '@/composables/usePolling'

const route = useRoute()
const dr = useDrStore()
const auth = useAuthStore()

const ns = route.params.ns as string
const kind = route.params.kind as string
const name = route.params.name as string

const editorEl = ref<HTMLElement | null>(null)
const loading = ref(true)
const drStatus = ref('missing')
const meta = ref<Record<string, unknown> | null>(null)
const error = ref('')
const saving = ref(false)
const deploying = ref(false)

let editor: import('monaco-editor').editor.IStandaloneCodeEditor | null = null

const canWrite = () => auth.me?.role === 'admin' || auth.me?.role === 'operator'

async function loadResource() {
  try {
    const { data } = await api.get<{ yaml: string; meta: Record<string, unknown>; drStatus: string }>(
      `/resources/${encodeURIComponent(ns)}/${encodeURIComponent(kind)}/${encodeURIComponent(name)}`,
    )
    drStatus.value = data.drStatus
    meta.value = data.meta
    if (editor) {
      editor.setValue(data.yaml)
    }
    return data.yaml
  } catch (e: unknown) {
    const err = e as { response?: { data?: string } }
    error.value = err.response?.data || 'Failed to load resource'
    return ''
  }
}

onMounted(async () => {
  const yaml = await loadResource()
  loading.value = false
  const monaco = await loader.init()
  if (editorEl.value) {
    editor = monaco.editor.create(editorEl.value, {
      value: yaml,
      language: 'yaml',
      theme: 'vs-dark',
      automaticLayout: true,
      minimap: { enabled: false },
      readOnly: !canWrite(),
    })
  }
})

onUnmounted(() => {
  if (editor) editor.dispose()
})

useIntervalWhile(async () => {
  await loadResource()
}, 10000, () => true)

function statusBadge(status: string) {
  if (status === 'synced') return 'badge-running'
  if (status === 'drifted') return 'badge-warn'
  return 'badge-error'
}

async function save() {
  if (!editor || !canWrite()) return
  saving.value = true
  error.value = ''
  try {
    await api.put(
      `/resources/${encodeURIComponent(ns)}/${encodeURIComponent(kind)}/${encodeURIComponent(name)}`,
      { yaml: editor.getValue() },
    )
    dr.scrapeMessage = 'Resource saved'
  } catch (e: unknown) {
    const err = e as { response?: { data?: string } }
    error.value = err.response?.data || 'Save failed'
  } finally {
    saving.value = false
  }
}

async function deploy() {
  if (!canWrite()) return
  deploying.value = true
  error.value = ''
  try {
    await dr.deploy({ namespace: ns, kind, name })
    dr.scrapeMessage = 'Deployed to DR'
    await loadResource()
  } catch (e: unknown) {
    const err = e as { response?: { data?: string } }
    error.value = err.response?.data || 'Deploy failed'
  } finally {
    deploying.value = false
  }
}
</script>

<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <RouterLink :to="`/namespace/${ns}`" class="text-sm text-muted hover:text-heading">{{ ns }}</RouterLink>
        <h1 class="text-xl font-semibold font-mono">{{ kind }}/{{ name }}</h1>
      </div>
      <div v-if="canWrite()" class="flex gap-2">
        <button type="button" class="btn-secondary" :disabled="saving" @click="save">
          {{ saving ? 'Saving...' : 'Save' }}
        </button>
        <button type="button" class="btn-primary" :disabled="deploying" @click="deploy">
          {{ deploying ? 'Deploying...' : 'Deploy to DR' }}
        </button>
      </div>
    </div>

    <p v-if="error" class="rounded-lg border border-red-500/40 bg-red-500/10 px-3 py-2 text-sm text-red-600 dark:text-red-300">{{ error }}</p>

    <div v-if="loading" class="text-sm text-muted">Loading...</div>

    <div v-else class="grid gap-4 lg:grid-cols-3">
      <div class="card lg:col-span-2 min-h-[60vh] p-0 overflow-hidden">
        <div ref="editorEl" class="h-[60vh]" />
      </div>
      <div class="card space-y-4">
        <div>
          <div class="text-sm text-muted">DR status</div>
          <span class="badge mt-1" :class="statusBadge(drStatus)">{{ drStatus }}</span>
        </div>
        <div v-if="meta">
          <div class="text-sm text-muted">API version</div>
          <div class="font-mono text-sm">{{ meta.apiVersion }}</div>
        </div>
        <div v-if="meta?.scrapedAt">
          <div class="text-sm text-muted">Last scraped</div>
          <div class="text-sm">{{ meta.scrapedAt }}</div>
        </div>
        <div v-if="meta?.updatedAt">
          <div class="text-sm text-muted">Last updated</div>
          <div class="text-sm">{{ meta.updatedAt }}</div>
        </div>
      </div>
    </div>
  </div>
</template>
