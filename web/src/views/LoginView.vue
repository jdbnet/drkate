<script setup lang="ts">
import { ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useThemeStore } from '@/stores/theme'
import { Moon, Sun } from '@lucide/vue'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
const theme = useThemeStore()
const username = ref('admin')
const password = ref('')
const error = ref('')
const loading = ref(false)

function isSafeRedirect(path: unknown) {
  return typeof path === 'string' && path.startsWith('/') && !path.startsWith('//')
}

async function login() {
  error.value = ''
  loading.value = true
  try {
    await auth.login(username.value, password.value)
    const redirect = route.query.redirect
    router.push(isSafeRedirect(redirect) ? redirect : '/')
  } catch {
    error.value = 'Invalid username or password'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="relative flex min-h-screen items-center justify-center overflow-hidden bg-surface px-4">
    <div class="pointer-events-none absolute inset-0" aria-hidden="true">
      <div class="absolute left-1/2 top-[-18%] h-[32rem] w-[32rem] -translate-x-1/2 rounded-full bg-accent/20 blur-3xl dark:bg-accent/10" />
    </div>
    <button
      type="button"
      class="btn-secondary absolute right-4 top-4 p-1.5"
      :aria-label="theme.dark ? 'Switch to light theme' : 'Switch to dark theme'"
      @click="theme.toggle()"
    >
      <Sun v-if="theme.dark" class="h-4 w-4" />
      <Moon v-else class="h-4 w-4" />
    </button>
    <div class="relative w-full max-w-md">
      <div class="mb-8 text-center">
        <img src="/favicon.png" alt="" class="brand-mark mx-auto mb-4 h-12 w-12" />
        <h1 class="text-2xl font-semibold tracking-tight text-heading">DrKate</h1>
        <p class="mt-1 text-sm text-muted">Kubernetes disaster recovery</p>
      </div>
      <form class="card space-y-4 p-8" @submit.prevent="login">
        <div v-if="error" class="rounded-lg border border-red-500/40 bg-red-500/10 px-3 py-2 text-center text-sm text-red-600 dark:text-red-300">
          {{ error }}
        </div>
        <div>
          <label class="mb-1 block text-sm text-muted" for="login-username">Username</label>
          <input id="login-username" v-model="username" type="text" required autofocus autocomplete="username" class="input-field" />
        </div>
        <div>
          <label class="mb-1 block text-sm text-muted" for="login-password">Password</label>
          <input id="login-password" v-model="password" type="password" required autocomplete="current-password" class="input-field" />
        </div>
        <button type="submit" class="btn-primary w-full py-2.5" :disabled="loading">
          {{ loading ? 'Signing in...' : 'Sign in' }}
        </button>
      </form>
    </div>
  </div>
</template>
