import { defineStore } from 'pinia'
import api, {
  type DeployResult,
  type NamespaceStatus,
  type ResourceStatusEntry,
  type ScrapeStatus,
  type OverviewResponse,
  type NamespaceDetailResponse,
} from '@/api/client'
import { formatDeployFeedback } from '@/lib/deploy'
import { useToastStore } from '@/stores/toast'

const DR_POLL_MS = 30000
const SCRAPE_POLL_MS = 15000
const SCRAPE_WAIT_MS = 15 * 60 * 1000
const COMPARE_WAIT_MS = 15 * 60 * 1000

function sleep(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

let compareWait: Promise<void> | null = null

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
    toast() {
      return useToastStore()
    },
    applyOverviewResponse(data: OverviewResponse) {
      this.overview = data.namespaces ?? []
      this.comparing = data.comparing
      this.lastComparedAt = data.lastComparedAt ?? null
      this.overviewFetchedAt = Date.now()
      this.syncCompareToast()
      if (this.comparing) {
        void this.waitForCompareIdle()
      }
    },
    syncCompareToast() {
      const toast = this.toast()
      if (this.comparing) {
        toast.info('Rebuilding DR status...', {
          id: 'compare',
          persist: true,
          detail: 'Comparing stored manifests with the DR cluster. Counts update when this finishes.',
        })
        return
      }
      toast.dismiss('compare')
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
        this.syncCompareToast()
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
      await this.refreshNamespace(ns)
    },
    async refreshScrapeStatus() {
      const { data } = await api.get<ScrapeStatus>('/scrape/status')
      this.scrapeStatus = data
      this.serverScraping = data.scraping
      return data
    },
    triggerDrRefresh() {
      this.comparing = true
      this.syncCompareToast()
      api.post('/dr/refresh').catch(() => {})
      void this.waitForCompareIdle()
    },
    async waitForCompareIdle() {
      if (compareWait) return compareWait
      compareWait = this.pollUntilCompareIdle().finally(() => {
        compareWait = null
      })
      return compareWait
    },
    async pollUntilCompareIdle() {
      const start = Date.now()
      while (Date.now() - start < COMPARE_WAIT_MS) {
        await this.refreshOverview({ background: true })
        if (!this.comparing) {
          await sleep(400)
          await this.refreshOverview({ background: true })
          if (!this.comparing) return
        }
        await sleep(2000)
      }
    },
    async waitForScrapeDone() {
      const start = Date.now()
      const toast = this.toast()
      while (Date.now() - start < SCRAPE_WAIT_MS) {
        const status = await this.refreshScrapeStatus()
        if (!status.scraping) {
          return status
        }
        if (status.resourceCount > 0) {
          toast.info(`Scraping... ${status.resourceCount} resources in ${status.namespaceCount} namespaces`, {
            id: 'scrape',
            persist: true,
          })
        }
        await sleep(1000)
      }
      throw new Error('Scrape timed out')
    },
    async runScrape() {
      const toast = this.toast()
      this.scrapeWaiting = true
      toast.info('Starting scrape...', { id: 'scrape', persist: true })
      try {
        await api.post('/scrape')
        const status = await this.waitForScrapeDone()
        const result = status.lastResult
        if (result) {
          const errCount = result.errors?.length ?? 0
          toast.success(
            `Scraped ${result.resourceCount} resources in ${result.namespaces.length} namespaces`,
            {
              id: 'scrape',
              detail: errCount ? `${errCount} warnings` : undefined,
            },
          )
        } else {
          toast.success('Scrape finished', { id: 'scrape' })
        }
        this.comparing = true
        this.syncCompareToast()
        void this.waitForCompareIdle()
      } catch (e: unknown) {
        const err = e as { response?: { data?: string }; message?: string }
        toast.error(err.response?.data || err.message || 'Scrape failed', { id: 'scrape' })
      } finally {
        this.scrapeWaiting = false
        await this.refreshScrapeStatus()
      }
    },
    applyDeployFeedback(result: DeployResult) {
      const { title, detail, ok } = formatDeployFeedback(result)
      const toast = this.toast()
      if (ok) {
        toast.success(title)
      } else {
        toast.error(title, { detail })
      }
    },
    async deploy(body: { namespace: string; kind?: string; name?: string; filter?: string }) {
      const { data } = await api.post<DeployResult>('/deploy', body)
      this.applyDeployFeedback(data)
      this.triggerDrRefresh()
      await this.waitForCompareIdle()
      return data
    },
    async startPolling() {
      await this.ensureOverview()
      this.syncCompareToast()
      this.refreshScrapeStatus()
    },
  },
})
