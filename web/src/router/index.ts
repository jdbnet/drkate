import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import LoginView from '@/views/LoginView.vue'
import DashboardView from '@/views/DashboardView.vue'
import NamespaceView from '@/views/NamespaceView.vue'
import ResourceEditorView from '@/views/ResourceEditorView.vue'
import UsersView from '@/views/UsersView.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', component: LoginView, meta: { public: true } },
    { path: '/', component: DashboardView },
    { path: '/namespace/:ns', component: NamespaceView },
    { path: '/resource/:ns/:kind/:name', component: ResourceEditorView },
    { path: '/users', component: UsersView },
  ],
})

router.beforeEach(async (to) => {
  if (to.meta.public) return true
  const auth = useAuthStore()
  await auth.check()
  if (!auth.authenticated) {
    return { path: '/login', query: to.path !== '/' ? { redirect: to.fullPath } : undefined }
  }
  return true
})

export default router
