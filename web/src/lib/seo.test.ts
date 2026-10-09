import { describe, expect, it } from 'vitest'
import indexHtml from '../../index.html?raw'
import { description, faq, features, formats, hero } from './content'
import { escapeHtml, headTags, llmsTxt, staticBody, structuredData } from './seo'
import { siteTitle } from './site'

function built(): Document {
  const html = indexHtml.replace('<!--seo-head-->', headTags()).replace('<!--seo-body-->', staticBody())
  return new DOMParser().parseFromString(html, 'text/html')
}

describe('seo', () => {
  it('fills index.html with the site title and description', () => {
    expect(indexHtml).toContain('<!--seo-head-->')
    expect(indexHtml).toContain('<!--seo-body-->')
    const doc = built()
    expect(doc.title).toBe(siteTitle)
    const meta = (sel: string) => doc.querySelector(sel)?.getAttribute('content')
    expect(meta('meta[name="description"]')).toBe(description)
    expect(meta('meta[property="og:title"]')).toBe(siteTitle)
    expect(meta('meta[property="og:image"]')).toBe('https://scholia-aws.mol.la/og.png')
    expect(doc.querySelector('link[rel="canonical"]')?.getAttribute('href')).toBe('https://scholia-aws.mol.la/')
  })

  it('keeps the description short enough for search results', () => {
    expect(siteTitle.length).toBeLessThanOrEqual(60)
    expect(description.length).toBeLessThanOrEqual(160)
  })

  it('gives readers without JavaScript the whole landing page', () => {
    const root = built().getElementById('root')!
    expect(root.querySelector('h1')?.textContent).toBe(hero.title)
    const text = root.textContent ?? ''
    for (const item of [hero.body, ...features.map((f) => f.body), ...formats.map((f) => f.kind), ...faq.map((f) => f.a)]) {
      expect(text).toContain(item)
    }
  })

  it('describes the app and its questions as structured data', () => {
    const scripts = [...built().querySelectorAll('script[type="application/ld+json"]')].map((s) => JSON.parse(s.textContent ?? ''))
    expect(scripts).toEqual(structuredData())
    const page = scripts.find((s) => s['@type'] === 'FAQPage')
    expect(page.mainEntity.map((q: { name: string }) => q.name)).toEqual(faq.map((f) => f.q))
  })

  it('summarises the site for agents in llms.txt', () => {
    const txt = llmsTxt()
    expect(txt.startsWith('# Scholia\n\n> ' + description)).toBe(true)
    for (const item of [...features.map((f) => f.title), ...formats.map((f) => f.files), ...faq.map((f) => f.q)]) {
      expect(txt).toContain(item)
    }
    expect(txt).toContain('https://github.com/SumonMSelim/scholia-aws')
  })

  it('escapes markup in copy', () => {
    expect(escapeHtml('<a href="x">&</a>')).toBe('&lt;a href=&quot;x&quot;&gt;&amp;&lt;/a&gt;')
  })

  it('never uses an em dash in public copy', () => {
    const all = JSON.stringify({ description, faq, features, formats, hero, siteTitle })
    expect(all).not.toContain('—')
  })
})
