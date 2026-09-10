<script setup lang="ts">
import { Check, Info, LoaderCircle, TriangleAlert, X } from '@lucide/vue'
import { useToastStore, type ToastKind } from '@/stores/toast'

const toasts = useToastStore()

const kindClass: Record<ToastKind, string> = {
  success: 'border-l-accent',
  error: 'border-l-red-500',
  info: 'border-l-neutral-400 dark:border-l-neutral-500',
}

function icon(kind: ToastKind) {
  if (kind === 'success') return Check
  if (kind === 'error') return TriangleAlert
  return Info
}

function iconClass(kind: ToastKind) {
  if (kind === 'success') return 'text-accent'
  if (kind === 'error') return 'text-red-500'
  return 'text-muted'
}
</script>

<template>
  <Teleport to="body">
    <div class="pointer-events-none fixed right-4 bottom-4 z-[80] flex w-96 max-w-[calc(100vw-2rem)] flex-col-reverse gap-2">
      <div
        v-for="t in toasts.items"
        :key="t.id"
        class="pointer-events-auto card border-l-4 py-3 shadow-lg"
        :class="kindClass[t.kind]"
        role="status"
      >
        <div class="flex items-start gap-2">
          <LoaderCircle v-if="t.persist" class="mt-0.5 h-4 w-4 shrink-0 animate-spin text-muted" />
          <component v-else :is="icon(t.kind)" class="mt-0.5 h-4 w-4 shrink-0" :class="iconClass(t.kind)" />
          <div class="min-w-0 flex-1">
            <p class="text-sm font-medium">{{ t.title }}</p>
            <pre v-if="t.detail" class="mt-1 max-h-36 overflow-y-auto whitespace-pre-wrap font-sans text-xs text-muted">{{ t.detail }}</pre>
          </div>
          <button
            v-if="!t.persist"
            type="button"
            class="focus-ring shrink-0 rounded-md text-muted hover:text-heading"
            aria-label="Dismiss notification"
            @click="toasts.dismiss(t.id)"
          >
            <X class="h-4 w-4" />
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
