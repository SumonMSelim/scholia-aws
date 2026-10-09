import { describe, expect, it } from 'vitest'
import { safeUrl } from './safeUrl'

describe('safeUrl', () => {
  it.each([
    'https://example.com/a?b=1#c',
    'http://example.com',
    'HTTPS://EXAMPLE.COM/x',
    'mailto:ta@example.edu',
  ])('keeps %s', (url) => {
    expect(safeUrl(url)).toBe(url)
  })

  it.each([
    'javascript:alert(1)',
    'JAVASCRIPT:alert(1)',
    'JaVaScRiPt:alert(1)',
    ' javascript:alert(1)',
    '\tjavascript:alert(1)',
    '\u0001javascript:alert(1)',
    'java\tscript:alert(1)',
    'data:text/html,<script>alert(1)</script>',
    'vbscript:msgbox(1)',
    'file:///etc/passwd',
    'blob:https://example.com/id',
    '//evil.example/x',
    '/relative/path',
    'relative',
    '#fragment',
    '',
  ])('drops %j', (url) => {
    expect(safeUrl(url)).toBe('')
  })
})
