import { lazy, Suspense } from 'react'

// The markdown, highlighting and KaTeX code loads only once a reply is shown,
// so the landing page and the first paint of the app stay small.
const Markdown = lazy(() => import('@/components/chat/Markdown'))

/** Assistant text as markdown. Until the renderer loads, the same text shows plain so nothing jumps. */
export function MessageText({ text }: { text: string }) {
  return (
    <Suspense fallback={<p className="max-w-[90ch] whitespace-pre-wrap">{text}</p>}>
      <Markdown text={text} />
    </Suspense>
  )
}
