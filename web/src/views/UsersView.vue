<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Trash2 } from '@lucide/vue'
import api, { type User } from '@/api/client'
import { confirm } from '@/lib/confirm'
import { useToastStore } from '@/stores/toast'

const toast = useToastStore()
const items = ref<User[]>([])
const loading = ref(true)
const form = ref({ username: '', password: '', role: 'viewer' })

async function load() {
  const { data } = await api.get<User[]>('/users')
  items.value = data || []
  loading.value = false
}

async function save() {
  try {
    await api.post('/users', form.value)
    form.value = { username: '', password: '', role: 'viewer' }
    toast.success('User created')
    await load()
  } catch (e: unknown) {
    const err = e as { response?: { data?: string }; message?: string }
    toast.error(err.response?.data || err.message || 'Failed to add user')
  }
}

async function askRemove(u: User) {
  if (!await confirm({
    title: 'Delete user?',
    message: `Remove "${u.username}"? They will no longer be able to sign in.`,
  })) return
  await api.delete(`/users/${encodeURIComponent(u.username)}`)
  toast.success(`Deleted ${u.username}`)
  await load()
}

onMounted(load)
</script>

<template>
  <div>
    <div class="mb-6">
      <h1 class="text-2xl font-semibold tracking-tight">Users</h1>
      <p class="mt-1 text-sm text-muted">Dashboard logins. Roles limit what someone can change.</p>
    </div>
    <form class="card mb-4 grid gap-3 p-5 md:grid-cols-4" @submit.prevent="save">
      <input v-model="form.username" class="input-field" placeholder="Username" required autocomplete="off" />
      <input v-model="form.password" class="input-field" type="password" placeholder="Password" required autocomplete="new-password" />
      <select v-model="form.role" class="input-field">
        <option value="viewer">Viewer</option>
        <option value="operator">Operator</option>
        <option value="admin">Admin</option>
      </select>
      <button class="btn-primary" type="submit">Add user</button>
    </form>
    <div class="card p-5">
      <div v-if="loading" class="space-y-3" aria-busy="true">
        <div v-for="i in 4" :key="i" class="skeleton h-9" />
      </div>
      <div v-else-if="items.length === 0" class="empty-state">No users yet.</div>
      <div v-else class="table-scroll">
        <table class="data-table">
          <thead class="text-muted">
            <tr><th>User</th><th>Role</th><th></th></tr>
          </thead>
          <tbody>
            <tr v-for="u in items" :key="u.username" class="table-row-hover">
              <td class="font-medium">{{ u.username }}</td>
              <td><span class="role-chip">{{ u.role }}</span></td>
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
