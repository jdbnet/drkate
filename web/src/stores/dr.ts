import { defineStore } from 'pinia'
import api, {
  type NamespaceStatus,
  type ResourceStatusEntry,
  type ScrapeStatus,
  type OverviewResponse,
  type NamespaceDetailResponse,
} from '@/api/client'

const DR_POLL_MS = 30000
const SCRAPE_POLL_MS = 15000
const SCRAPE_WAIT_MS = 15 * 60 * 1000

function sleep(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

export const useDrStore = defineStore('dr', {
  state: () => ({
    overview: [] as NamespaceStatus[],
    namespaceDetail: {} as Record<string, ResourceStatusEntry[]>,
    scrapeStatus: null as ScrapeStatus | null,
    comparing: false,
    lastComparedAt: null as string | null,
    overviewFetchedAt: null as number | null,
    namespaceFetchedAt: {} as Record<string, number>,
    overviewLoading: false,
    namespaceLoading: {} as Record<string, boolean>,
    scrapeWaiting: false,
    serverScraping: false,
    scrapeMessage: '' as string,
    scrapeError: '' as string,
  }),
  getters: {
    totals(state) {
      let synced = 0, drifted = 0, missing = 0, total = 0
      for (const ns of state.overview) {
        synced += ns.synced
        drifted += ns.drifted
        missing += ns.missing
        total += ns.total
      }
      return { synced, drifted, missing, total }
    },
    scrapeBusy(state) {
      return state.scrapeWaiting || state.serverScraping
    },
    drPollInterval: () => DR_POLL_MS,
    scrapePollInterval: () => SCRAPE_POLL_MS,
  },
  actions: {
    applyOverviewResponse(data: OverviewResponse) {
      this.overview = data.namespaces
      this.comparing = data.comparing
      this.lastComparedAt = data.lastComparedAt ?? null
      this.overviewFetchedAt = Date.now()
    },
    async refreshOverview(opts: { background?: boolean } = {}) {
      const background = opts.background ?? false
      if (!background && this.overview.length === 0) {
        this.overviewLoading = true
      }
      try {
        const { data } = await api.get<OverviewResponse>('/dr/status')
        this.applyOverviewResponse(data)
      } finally {
        this.overviewLoading = false
      }
    },
    async refreshNamespace(ns: string, opts: { background?: boolean } = {}) {
      const background = opts.background ?? false
      if (!background && !this.namespaceDetail[ns]?.length) {
        this.namespaceLoading[ns] = true
      }
      try {
        const { data } = await api.get<NamespaceDetailResponse>(`/dr/status/${encodeURIComponent(ns)}`)
        this.namespaceDetail[ns] = data.resources
        this.comparing = data.comparing
        this.lastComparedAt = data.lastComparedAt ?? this.lastComparedAt
        this.namespaceFetchedAt[ns] = Date.now()
      } finally {
        this.namespaceLoading[ns] = false
      }
    },
    async ensureOverview() {
      if (this.overview.length === 0) {
        await this.refreshOverview()
      }
    },
    async ensureNamespace(ns: string) {
      if (!this.namespaceDetail[ns]?.length) {
        await this.refreshNamespace(ns)
      }
    },
    async refreshScrapeStatus() {
      const { data } = await api.get<ScrapeStatus>('/scrape/status')
      this.scrapeStatus = data
      this.serverScraping = data.scraping
      return data
    },
    triggerDrRefresh() {
      api.post('/dr/refresh').catch(() => {})
    },
    async waitForScrapeDone() {
      const start = Date.now()
      while (Date.now() - start < SCRAPE_WAIT_MS) {
        const status = await this.refreshScrapeStatus()
        if (!status.scraping) {
          return status
        }
        await sleep(1000)
      }
      throw new Error('Scrape timed out')
    },
    async runScrape() {
      this.scrapeError = ''
      this.scrapeMessage = ''
      this.scrapeWaiting = true
      try {
        await api.post('/scrape')
        const status = await this.waitForScrapeDone()
        const result = status.lastResult
        if (result) {
          const errCount = result.errors?.length ?? 0
          this.scrapeMessage = `Scraped ${result.resourceCount} resources in ${result.namespaces.length} namespaces${errCount ? ` (${errCount} warnings)` : ''}`
        } else {
          this.scrapeMessage = 'Scrape finished'
        }
        this.triggerDrRefresh()
      } catch (e: unknown) {
        const err = e as { response?: { data?: string }; message?: string }
        this.scrapeError = err.response?.data || err.message || 'Scrape failed'
      } finally {
        this.scrapeWaiting = false
        await this.refreshScrapeStatus()
      }
    },
    async deploy(body: { namespace: string; kind?: string; name?: string; filter?: string }) {
      const { data } = await api.post('/deploy', body)
      this.triggerDrRefresh()
      return data
    },
    startPolling() {
      this.ensureOverview()
      this.refreshScrapeStatus()
    },
  },
})
