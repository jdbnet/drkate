import axios from 'axios'

const api = axios.create({
  baseURL: '/api',
  withCredentials: true,
})

api.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401 && !window.location.pathname.startsWith('/login')) {
      const redirect = encodeURIComponent(window.location.pathname + window.location.search)
      window.location.href = `/login?redirect=${redirect}`
    }
    return Promise.reject(error)
  },
)

export default api

export interface User {
  username: string
  role: string
}

export interface NamespaceStatus {
  namespace: string
  synced: number
  drifted: number
  missing: number
  total: number
}

export interface OverviewResponse {
  namespaces: NamespaceStatus[]
  comparing: boolean
  lastComparedAt: string | null
}

export interface NamespaceDetailResponse {
  resources: ResourceStatusEntry[]
  comparing: boolean
  lastComparedAt: string | null
}

export interface ResourceStatusEntry {
  namespace: string
  kind: string
  name: string
  status: 'synced' | 'drifted' | 'missing'
  scrapedAt?: string
  updatedAt?: string
}

export interface ResourceMeta {
  namespace: string
  kind: string
  name: string
  apiVersion: string
  scrapedAt: string
  updatedAt: string
}

export interface ScrapeStatus {
  scraping: boolean
  lastScrapeAt: string | null
  namespaceCount: number
  resourceCount: number
  namespaces: string[]
  lastResult?: {
    namespaces: string[]
    resourceCount: number
    errors?: string[]
  }
}

export interface ScrapeResult {
  namespaces: string[]
  resourceCount: number
  errors?: string[]
}

export interface DeployResult {
  success: string[]
  failed: string[]
}
