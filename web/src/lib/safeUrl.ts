const safeProtocols = new Set(['http:', 'https:', 'mailto:'])

/** Keeps http(s) and mailto links. Anything else, including javascript: and relative paths, is dropped. */
export function safeUrl(url: string): string {
  try {
    return safeProtocols.has(new URL(url).protocol) ? url : ''
  } catch {
    return ''
  }
}
