<script setup lang="ts">
import { nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { ChevronRight, TriangleAlert } from '@lucide/vue'
import loader from '@monaco-editor/loader'
import api, { type ResourceWarning } from '@/api/client'
import { useDrStore } from '@/stores/dr'
import { useAuthStore } from '@/stores/auth'
import { useThemeStore } from '@/stores/theme'
import { useToastStore } from '@/stores/toast'
import { useIntervalWhile } from '@/composables/usePolling'
import { formatTime } from '@/lib/time'
import { confirm } from '@/lib/confirm'

type Monaco = Awaited<ReturnType<typeof loader.init>>

const route = useRoute()
const dr = useDrStore()
const auth = useAuthStore()
const theme = useThemeStore()

const ns = route.params.ns as string
const kind = route.params.kind as string
const name = route.params.name as string

const toast = useToastStore()
const editorEl = ref<HTMLElement | null>(null)
const loading = ref(true)
const drStatus = ref('missing')
const drift = ref('')
const warnings = ref<ResourceWarning[]>([])
const meta = ref<Record<string, unknown> | null>(null)
const edited = ref(false)
const scrapedYaml = ref('')
const saving = ref(false)
const deploying = ref(false)
const reverting = ref(false)

let monacoApi: Monaco | null = null
let editor: import('monaco-editor').editor.IStandaloneCodeEditor | null = null
let diffEditor: import('monaco-editor').editor.IStandaloneDiffEditor | null = null
let originalModel: import('monaco-editor').editor.ITextModel | null = null
let modifiedModel: import('monaco-editor').editor.ITextModel | null = null
let lastSyncedYaml = ''

const canWrite = () => auth.me?.role === 'admin' || auth.me?.role === 'operator'

function editorTheme() {
  return theme.dark ? 'vs-dark' : 'vs'
}

function currentYaml() {
  if (diffEditor) return diffEditor.getModifiedEditor().getValue()
  if (editor) return editor.getValue()
  return ''
}

function isDirty() {
  const value = currentYaml()
  return value !== '' && value !== lastSyncedYaml
}

function disposeEditors() {
  diffEditor?.dispose()
  editor?.dispose()
  originalModel?.dispose()
  modifiedModel?.dispose()
  diffEditor = null
  editor = null
  originalModel = null
  modifiedModel = null
}

function applyEditorOptions() {
  const common = {
    automaticLayout: true,
    minimap: { enabled: false },
    readOnly: !canWrite(),
    fontSize: 13,
    scrollBeyondLastLine: false,
  }
  if (editor) {
    editor.updateOptions(common)
  }
  if (diffEditor) {
    diffEditor.updateOptions({
      ...common,
      originalEditable: false,
      renderSideBySide: false,
      useInlineViewWhenSpaceIsLimited: true,
    })
  }
}

async function mountEditor(yaml: string, scraped: string, showDiff: boolean) {
  if (!editorEl.value) return
  if (!monacoApi) {
    monacoApi = await loader.init()
  }
  const monaco = monacoApi
  disposeEditors()
  if (showDiff) {
    originalModel = monaco.editor.createModel(scraped, 'yaml')
    modifiedModel = monaco.editor.createModel(yaml, 'yaml')
    diffEditor = monaco.editor.createDiffEditor(editorEl.value, {
      theme: editorTheme(),
      automaticLayout: true,
      minimap: { enabled: false },
      readOnly: !canWrite(),
      originalEditable: false,
      renderSideBySide: false,
      useInlineViewWhenSpaceIsLimited: true,
      renderMarginRevertIcon: false,
      fontSize: 13,
      scrollBeyondLastLine: false,
      padding: { top: 16, bottom: 16 },
    })
    diffEditor.setModel({ original: originalModel, modified: modifiedModel })
    return
  }
  editor = monaco.editor.create(editorEl.value, {
    value: yaml,
    language: 'yaml',
    theme: editorTheme(),
    automaticLayout: true,
    minimap: { enabled: false },
    readOnly: !canWrite(),
    padding: { top: 16, bottom: 16 },
    fontSize: 13,
    scrollBeyondLastLine: false,
  })
}

function syncEditor(yaml: string, scraped: string, showDiff: boolean) {
  const dirty = isDirty()
  const modeChanged = showDiff !== !!diffEditor
  if (modeChanged || (!editor && !diffEditor)) {
    void mountEditor(dirty ? currentYaml() || yaml : yaml, scraped, showDiff)
    if (!dirty) lastSyncedYaml = yaml
    return
  }
  if (diffEditor && originalModel) {
    if (originalModel.getValue() !== scraped) {
      originalModel.setValue(scraped)
    }
    if (!dirty && modifiedModel && modifiedModel.getValue() !== yaml) {
      modifiedModel.setValue(yaml)
      lastSyncedYaml = yaml
    }
    return
  }
  if (editor && !dirty && editor.getValue() !== yaml) {
    editor.setValue(yaml)
    lastSyncedYaml = yaml
  }
}

async function fetchResource() {
  const { data } = await api.get<{
    yaml: string
    scrapedYaml?: string
    edited?: boolean
    meta: Record<string, unknown>
    drStatus: string
    drift?: string
    warnings?: ResourceWarning[]
  }>(
    `/resources/${encodeURIComponent(ns)}/${encodeURIComponent(kind)}/${encodeURIComponent(name)}`,
  )
  drStatus.value = data.drStatus
  drift.value = data.drift || ''
  warnings.value = data.warnings || []
  meta.value = data.meta
  edited.value = !!data.edited
  scrapedYaml.value = data.scrapedYaml || data.yaml
  return data
}

async function loadResource() {
  try {
    const data = await fetchResource()
    syncEditor(data.yaml, scrapedYaml.value, edited.value)
    if (!isDirty()) {
      lastSyncedYaml = data.yaml
    }
    return data.yaml
  } catch (e: unknown) {
    const err = e as { response?: { data?: string } }
    toast.error(err.response?.data || 'Failed to load resource')
    return ''
  }
}

onMounted(async () => {
  try {
    const data = await fetchResource()
    loading.value = false
    await nextTick()
    await mountEditor(data.yaml, scrapedYaml.value || data.yaml, edited.value)
    lastSyncedYaml = data.yaml
  } catch (e: unknown) {
    loading.value = false
    const err = e as { response?: { data?: string } }
    toast.error(err.response?.data || 'Failed to load resource')
  }
})

watch(() => theme.dark, async () => {
  if (!monacoApi) {
    monacoApi = await loader.init()
  }
  monacoApi.editor.setTheme(editorTheme())
  applyEditorOptions()
})

onUnmounted(() => {
  disposeEditors()
})

useIntervalWhile(async () => {
  await loadResource()
}, 10000, () => true)

function statusBadge(status: string) {
  if (status === 'synced') return 'badge-synced'
  if (status === 'drifted') return 'badge-drifted'
  if (status === 'pending') return 'badge-pending'
  return 'badge-missing'
}

async function save() {
  if (!canWrite()) return
  saving.value = true
  try {
    await api.put(
      `/resources/${encodeURIComponent(ns)}/${encodeURIComponent(kind)}/${encodeURIComponent(name)}`,
      { yaml: currentYaml() },
    )
    lastSyncedYaml = currentYaml()
    toast.success('Local copy saved. Scrapes will not overwrite it.')
    await loadResource()
  } catch (e: unknown) {
    const err = e as { response?: { data?: string } }
    toast.error(err.response?.data || 'Save failed')
  } finally {
    saving.value = false
  }
}

async function revert() {
  if (!canWrite()) return
  if (!await confirm({
    title: 'Discard local edits?',
    message: 'This restores the last scraped copy from source. Your overlay will be deleted.',
    confirmLabel: 'Discard edits',
  })) return
  reverting.value = true
  try {
    await api.delete(`/resources/${encodeURIComponent(ns)}/${encodeURIComponent(kind)}/${encodeURIComponent(name)}/edit`)
    lastSyncedYaml = ''
    toast.success('Restored scraped copy')
    await loadResource()
  } catch (e: unknown) {
    const err = e as { response?: { data?: string } }
    toast.error(err.response?.data || 'Revert failed')
  } finally {
    reverting.value = false
  }
}

async function deploy() {
  if (!canWrite()) return
  deploying.value = true
  try {
    await dr.deploy({ namespace: ns, kind, name })
    await loadResource()
  } catch (e: unknown) {
    const err = e as { response?: { data?: string } }
    toast.error(err.response?.data || 'Deploy failed')
  } finally {
    deploying.value = false
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
          <RouterLink :to="`/namespace/${ns}`">{{ ns }}</RouterLink>
          <ChevronRight class="h-3.5 w-3.5" />
        </nav>
        <h1 class="font-mono text-2xl font-semibold tracking-tight">{{ kind }}/{{ name }}</h1>
      </div>
      <div v-if="canWrite()" class="flex flex-wrap gap-2">
        <button v-if="edited" type="button" class="btn-secondary" :disabled="reverting" @click="revert">
          {{ reverting ? 'Reverting...' : 'Discard edits' }}
        </button>
        <button type="button" class="btn-secondary" :disabled="saving" @click="save">
          {{ saving ? 'Saving...' : 'Save' }}
        </button>
        <button type="button" class="btn-primary" :disabled="deploying" @click="deploy">
          {{ deploying ? 'Deploying...' : 'Deploy to DR' }}
        </button>
      </div>
    </div>

    <div v-if="edited" class="callout-warn mb-4">
      <p class="font-medium text-heading">Local copy</p>
      <p class="mt-1 text-muted">
        Red lines are the scraped source. Green lines are your changes. Scrapes update the source only. Deploy uses your copy.
      </p>
    </div>

    <div v-if="loading" class="card space-y-3 p-5" aria-busy="true">
      <div class="skeleton h-64" />
    </div>

    <div v-else class="grid gap-4 lg:grid-cols-3">
      <div class="min-h-[60vh] overflow-hidden lg:col-span-2">
        <div class="card overflow-hidden p-0">
          <div ref="editorEl" class="h-[60vh]" />
        </div>
      </div>
      <div class="card space-y-5 p-5">
        <div>
          <div class="meta-label">DR status</div>
          <span class="badge mt-1.5" :class="statusBadge(drStatus)">{{ drStatus }}</span>
        </div>
        <div v-if="edited">
          <div class="meta-label">Local edit</div>
          <div class="mt-1 text-sm">{{ formatTime(meta?.editedAt) || 'saved' }}</div>
        </div>
        <div v-if="warnings.length">
          <div class="meta-label">DR caveats</div>
          <ul class="mt-1.5 space-y-2">
            <li v-for="(w, i) in warnings" :key="i" class="flex gap-2 text-sm">
              <TriangleAlert class="mt-0.5 h-3.5 w-3.5 shrink-0 text-amber-600 dark:text-amber-400" />
              <span>{{ w.message }}</span>
            </li>
          </ul>
        </div>
        <div v-if="drift">
          <div class="meta-label">Drift</div>
          <div class="mt-1.5 break-all font-mono text-xs leading-relaxed text-muted">{{ drift }}</div>
        </div>
        <div v-if="meta">
          <div class="meta-label">API version</div>
          <div class="mt-1 font-mono text-sm">{{ meta.apiVersion }}</div>
        </div>
        <div v-if="meta?.scrapedAt">
          <div class="meta-label">Last scraped</div>
          <div class="mt-1 text-sm">{{ formatTime(meta.scrapedAt) || '—' }}</div>
        </div>
        <div v-if="meta?.updatedAt">
          <div class="meta-label">Last updated</div>
          <div class="mt-1 text-sm">{{ formatTime(meta.updatedAt) || '—' }}</div>
        </div>
      </div>
    </div>
  </div>
</template>
