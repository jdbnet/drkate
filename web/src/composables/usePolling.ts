import { onMounted, onUnmounted } from 'vue'

export function usePolling(
  fn: () => void | Promise<void>,
  intervalMs: number,
  options: { pauseWhenHidden?: boolean; immediate?: boolean } = {},
) {
  const { pauseWhenHidden = true, immediate = true } = options
  let timer: ReturnType<typeof setInterval> | null = null

  async function run() {
    if (pauseWhenHidden && document.visibilityState === 'hidden') return
    await fn()
  }

  onMounted(() => {
    if (immediate) run()
    timer = setInterval(run, intervalMs)
  })

  onUnmounted(() => {
    if (timer) clearInterval(timer)
  })

  return { refresh: run }
}

export function useIntervalWhile(
  fn: () => void | Promise<void>,
  intervalMs: number,
  active: () => boolean,
) {
  let timer: ReturnType<typeof setInterval> | null = null

  onMounted(() => {
    timer = setInterval(() => {
      if (active()) fn()
    }, intervalMs)
  })

  onUnmounted(() => {
    if (timer) clearInterval(timer)
  })
}
