const units: [Intl.RelativeTimeFormatUnit, number][] = [
  ['day', 86_400_000],
  ['hour', 3_600_000],
  ['minute', 60_000],
]

/** "in 47 hours" from now to iso. Past or unparsable times read as "soon". */
export function relativeFrom(iso: string, now = Date.now()): string {
  const ms = Date.parse(iso) - now
  if (!Number.isFinite(ms) || ms <= 0) return 'soon'
  const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'always' })
  // Days only once there are at least two, so 47 hours does not round to "in 2 days".
  for (const [unit, size] of units) {
    if (unit === 'day' ? ms >= 2 * size : ms >= size) return rtf.format(Math.floor(ms / size), unit)
  }
  return rtf.format(1, 'minute')
}
