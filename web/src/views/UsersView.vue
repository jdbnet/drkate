<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Trash2 } from '@lucide/vue'
import api, { type User } from '@/api/client'
import { confirm } from '@/lib/confirm'

const items = ref<User[]>([])
const form = ref({ username: '', password: '', role: 'viewer' })
const error = ref('')

async function load() {
  const { data } = await api.get<User[]>('/users')
  items.value = data || []
}

async function save() {
  error.value = ''
  try {
    await api.post('/users', form.value)
    form.value = { username: '', password: '', role: 'viewer' }
    await load()
  } catch (e: unknown) {
    const err = e as { response?: { data?: string }; message?: string }
    error.value = err.response?.data || err.message || 'Failed to add user'
  }
}

async function askRemove(u: User) {
  if (!await confirm({
    title: 'Delete user?',
    message: `Remove "${u.username}"? They will no longer be able to sign in.`,
  })) return
  await api.delete(`/users/${encodeURIComponent(u.username)}`)
  await load()
}

onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <div>
      <h1 class="text-xl font-semibold">Users</h1>
      <p class="mt-1 text-sm text-muted">Dashboard logins. Roles limit what someone can change.</p>
    </div>
    <p v-if="error" class="rounded-lg border border-red-500/40 bg-red-500/10 px-3 py-2 text-sm text-red-600 dark:text-red-300">{{ error }}</p>
    <form class="card grid gap-3 md:grid-cols-4" @submit.prevent="save">
      <input v-model="form.username" class="input-field" placeholder="Username" required />
      <input v-model="form.password" class="input-field" type="password" placeholder="Password" required />
      <select v-model="form.role" class="input-field">
        <option value="viewer">Viewer</option>
        <option value="operator">Operator</option>
        <option value="admin">Admin</option>
      </select>
      <button class="btn-primary" type="submit">Add user</button>
    </form>
    <div class="card">
      <div class="table-scroll">
        <table class="data-table">
          <thead class="text-muted">
            <tr><th>User</th><th>Role</th><th></th></tr>
          </thead>
          <tbody>
            <tr v-for="u in items" :key="u.username" class="table-row-hover">
              <td>{{ u.username }}</td>
              <td>{{ u.role }}</td>
              <td class="text-right">
                <button class="btn-row btn-row-danger" type="button" @click="askRemove(u)">
                  <Trash2 class="h-3.5 w-3.5" />
                  Delete
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>
