import { describe, expect, it } from 'vitest'
import { relativeFrom } from './time'

describe('relativeFrom', () => {
  const now = Date.parse('2026-10-01T00:00:00Z')
  it.each([
    ['2026-10-03T00:00:00Z', 'in 2 days'],
    ['2026-10-02T23:00:00Z', 'in 47 hours'],
    ['2026-10-01T01:30:00Z', 'in 1 hour'],
    ['2026-10-01T00:10:00Z', 'in 10 minutes'],
    ['2026-10-01T00:00:20Z', 'in 1 minute'],
    ['2026-09-30T00:00:00Z', 'soon'],
    ['not a date', 'soon'],
  ])('%s reads as %s', (iso, want) => {
    expect(relativeFrom(iso, now)).toBe(want)
  })
})
