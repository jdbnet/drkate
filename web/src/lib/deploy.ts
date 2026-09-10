import type { DeployResult } from '@/api/client'

export function formatDeployFeedback(result: DeployResult): { title: string; detail?: string; ok: boolean } {
  const success = result.success ?? []
  const failed = result.failed ?? []

  if (failed.length === 0) {
    const title =
      success.length === 1
        ? `Deployed ${success[0]}`
        : `Deploy complete: ${success.length} succeeded`
    return { title, ok: true }
  }

  const title = `${success.length} succeeded, ${failed.length} failed`
  return { title, detail: failed.join('\n'), ok: false }
}
