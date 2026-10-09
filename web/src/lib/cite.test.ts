import { describe, expect, it } from 'vitest'
import { firstTime, formatTime, youTubeEmbed } from './cite'

describe('citations', () => {
  it('formats a lecture time', () => {
    expect(formatTime(0)).toBe('0:00.000')
    expect(formatTime(1500)).toBe('0:01.500')
    expect(formatTime(61000)).toBe('1:01.000')
    expect(formatTime(-5)).toBe('0:00.000')
  })

  it('plays a lecture on YouTube a moment before the cue', () => {
    expect(youTubeEmbed('Nu8YGneFCWE', 754_900)).toBe('https://www.youtube-nocookie.com/embed/Nu8YGneFCWE?start=752&autoplay=1&rel=0')
    expect(youTubeEmbed('Nu8YGneFCWE', 500)).toContain('start=0&')
    expect(youTubeEmbed('x" onload="alert(1)', 0)).toBe('')
    expect(youTubeEmbed('', 0)).toBe('')
  })

  it('finds the first lecture moment', () => {
    expect(firstTime([{ kind: 'page', page: 1 }, { kind: 'time', start_ms: 5, end_ms: 9 }])).toEqual({ kind: 'time', start_ms: 5, end_ms: 9 })
    expect(firstTime([{ kind: 'slide', slide: 2 }])).toBeUndefined()
  })
})
