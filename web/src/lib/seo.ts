/**
 * Builds what index.html carries before any JavaScript runs: meta tags,
 * structured data and a plain HTML copy of the landing page. Crawlers and AI
 * agents that do not run scripts read this; React replaces the body copy on load.
 * vite.config.ts calls these at build time, so the text comes from content.ts only.
 */
// Explicit extensions: vite.config.ts imports this file under nodenext resolution.
import { description, faq, features, formats, hero, repoUrl, siteUrl, steps } from './content.ts'
import { siteName, siteTitle } from './site.ts'

const ogImage = `${siteUrl}/og.png`

export function escapeHtml(text: string): string {
  return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
}

/** JSON inside a script element must not be able to close it. */
function jsonScript(data: unknown): string {
  return `<script type="application/ld+json">${JSON.stringify(data).replace(/</g, '\\u003c')}</script>`
}

export function structuredData(): unknown[] {
  return [
    {
      '@context': 'https://schema.org',
      '@type': 'WebApplication',
      name: siteName,
      url: siteUrl,
      description,
      applicationCategory: 'EducationalApplication',
      operatingSystem: 'Any (web browser)',
      isAccessibleForFree: true,
      offers: { '@type': 'Offer', price: '0', priceCurrency: 'USD' },
      featureList: features.map((f) => f.title),
      image: ogImage,
      sameAs: [repoUrl],
    },
    {
      '@context': 'https://schema.org',
      '@type': 'FAQPage',
      mainEntity: faq.map(({ q, a }) => ({
        '@type': 'Question',
        name: q,
        acceptedAnswer: { '@type': 'Answer', text: a },
      })),
    },
  ]
}

export function headTags(): string {
  const meta = (attr: 'name' | 'property', key: string, value: string) =>
    `<meta ${attr}="${key}" content="${escapeHtml(value)}" />`
  return [
    `<title>${escapeHtml(siteTitle)}</title>`,
    meta('name', 'description', description),
    `<link rel="canonical" href="${siteUrl}/" />`,
    meta('name', 'theme-color', '#ffffff'),
    meta('property', 'og:type', 'website'),
    meta('property', 'og:site_name', siteName),
    meta('property', 'og:title', siteTitle),
    meta('property', 'og:description', description),
    meta('property', 'og:url', `${siteUrl}/`),
    meta('property', 'og:image', ogImage),
    meta('property', 'og:image:width', '1200'),
    meta('property', 'og:image:height', '630'),
    meta('property', 'og:image:alt', siteTitle),
    meta('name', 'twitter:card', 'summary_large_image'),
    meta('name', 'twitter:title', siteTitle),
    meta('name', 'twitter:description', description),
    meta('name', 'twitter:image', ogImage),
    ...structuredData().map(jsonScript),
  ].join('\n    ')
}

/** llms.txt: a plain Markdown summary for AI agents, per llmstxt.org. */
export function llmsTxt(): string {
  return [
    `# ${siteName}`,
    '',
    `> ${description}`,
    '',
    hero.body,
    '',
    '## What it does',
    '',
    ...features.map((f) => `- ${f.title}: ${f.body}`),
    '',
    '## Files you can upload',
    '',
    ...formats.map((f) => `- ${f.kind}: ${f.files}`),
    '',
    '## How it works',
    '',
    ...steps.map((s, i) => `${i + 1}. ${s.title}. ${s.body}`),
    '',
    '## Questions',
    '',
    ...faq.flatMap(({ q, a }) => [`### ${q}`, '', a, '']),
    '## Links',
    '',
    `- [Try the demo, no sign-up](${siteUrl}/)`,
    `- [Source code on GitHub](${repoUrl})`,
    '',
  ].join('\n')
}

/** The landing page as plain HTML, for readers that do not run JavaScript. */
export function staticBody(): string {
  const e = escapeHtml
  const list = (items: string[]) => items.map((item) => `<li>${item}</li>`).join('')
  return [
    `<main class="seo-static mx-auto max-w-3xl space-y-6 px-6 py-10">`,
    `<h1 class="text-4xl font-semibold tracking-tight">${e(hero.title)}</h1>`,
    `<p>${e(hero.body)}</p>`,
    `<p><a href="${siteUrl}/">Try the demo, no sign-up</a></p>`,
    `<h2 class="text-2xl font-semibold">What Scholia does</h2>`,
    `<ul>${list(features.map((f) => `<strong>${e(f.title)}.</strong> ${e(f.body)}`))}</ul>`,
    `<h2 class="text-2xl font-semibold">Files you can upload</h2>`,
    `<ul>${list(formats.map((f) => `${e(f.kind)}: ${e(f.files)}`))}</ul>`,
    `<h2 class="text-2xl font-semibold">How it works</h2>`,
    `<ol>${list(steps.map((s) => `<strong>${e(s.title)}.</strong> ${e(s.body)}`))}</ol>`,
    `<h2 class="text-2xl font-semibold">Questions</h2>`,
    faq.map(({ q, a }) => `<h3 class="font-medium">${e(q)}</h3><p>${e(a)}</p>`).join(''),
    `</main>`,
  ].join('')
}
