import type { Locator, TimeLocator } from '@/lib/api'

export function formatTime(ms: number): string {
  const total = Math.max(0, Math.floor(ms))
  const minutes = Math.floor(total / 60000)
  const seconds = Math.floor((total % 60000) / 1000)
  const milli = total % 1000
  return `${minutes}:${String(seconds).padStart(2, '0')}.${String(milli).padStart(3, '0')}`
}

/** place names the slide, page or lecture moment. A text offset means nothing to a reader, so it is left out. */
export function place(locators: Locator[]): string {
  for (const locator of locators) {
    if (locator.kind === 'page') return `p. ${locator.page}`
    if (locator.kind === 'slide') return `slide ${locator.slide}`
    if (locator.kind === 'time') return formatTime(locator.start_ms).replace(/\.\d+$/, '')
  }
  return ''
}

const youTubeId = /^[A-Za-z0-9_-]{11}$/

// A caption cue starts mid-thought, so playback begins a moment earlier.
const leadInSeconds = 2

/** youTubeEmbed is the privacy-enhanced player URL at a lecture moment, or '' for an id that is not YouTube's. */
export function youTubeEmbed(id: string, startMs: number): string {
  if (!youTubeId.test(id)) return ''
  const start = Math.max(0, Math.floor(startMs / 1000) - leadInSeconds)
  return `https://www.youtube-nocookie.com/embed/${id}?start=${start}&autoplay=1&rel=0`
}

/** firstTime is the first lecture moment among a citation's locators. */
export function firstTime(locators: Locator[]): TimeLocator | undefined {
  return locators.find((locator): locator is TimeLocator => locator.kind === 'time')
}
