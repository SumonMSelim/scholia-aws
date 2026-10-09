import { useState } from 'react'
import { Link2, Play } from 'lucide-react'
import type { Citation, WebSource } from '@/lib/api'
import { firstTime, place, youTubeEmbed } from '@/lib/cite'
import { safeUrl } from '@/lib/safeUrl'

/**
 * Sources lists what an answer drew on, numbered as the answer cites it: course
 * passages as [1], [2] and web results as [W1]. A passage opens to show its text.
 * A lecture moment from a recorded lecture opens the recording at that moment.
 */
export function SourceList({
  citations,
  web,
  names,
  videos = {},
}: {
  citations: Citation[]
  web: WebSource[]
  names: Record<string, string>
  /** videos maps a source id to the YouTube id of its lecture recording. */
  videos?: Record<string, string>
}) {
  const [open, setOpen] = useState<number | null>(null)
  const [playing, setPlaying] = useState<number | null>(null)
  if (citations.length === 0 && web.length === 0) return null
  return (
    <section aria-label="Sources" className="max-w-[90ch] border border-border bg-card px-4 py-3">
      <h2 className="flex items-center gap-2 text-xs font-semibold tracking-wide text-muted-foreground uppercase">
        <Link2 className="size-3.5" aria-hidden />
        Sources
      </h2>
      <ol className="mt-2 space-y-1">
        {citations.map((citation, index) => {
          const expanded = open === index
          const name = names[citation.source_id] ?? 'Course file'
          const where = place(citation.locators)
          const section = lastSection(citation.section)
          const moment = firstTime(citation.locators)
          const player = moment ? youTubeEmbed(videos[citation.source_id] ?? '', moment.start_ms) : ''
          const watching = player !== '' && playing === index
          return (
            <li key={`${citation.chunk_id}-${index}`} className="text-sm leading-6">
              <div className="flex min-w-0 items-baseline gap-3">
                <button
                  type="button"
                  aria-expanded={expanded}
                  onClick={() => setOpen(expanded ? null : index)}
                  className="flex min-w-0 flex-1 gap-2 text-left hover:text-primary"
                >
                  <span className="shrink-0 text-muted-foreground tabular-nums">{index + 1}.</span>
                  <span className="min-w-0 truncate" title={citation.section}>
                    {name}
                    {section ? <span className="text-muted-foreground"> · {section}</span> : null}
                    {where ? <span className="text-muted-foreground"> · {where}</span> : null}
                  </span>
                </button>
                {player !== '' ? (
                  <button
                    type="button"
                    aria-pressed={watching}
                    onClick={() => setPlaying(watching ? null : index)}
                    className="inline-flex shrink-0 items-center gap-1 font-medium text-primary underline-offset-4 hover:underline"
                  >
                    <Play className="size-3.5" aria-hidden />
                    {watching ? 'Close' : `Watch ${where}`}
                  </button>
                ) : null}
              </div>
              {watching ? (
                <iframe
                  src={player}
                  title={`${name} at ${where}`}
                  allow="autoplay; encrypted-media; picture-in-picture"
                  allowFullScreen
                  referrerPolicy="strict-origin-when-cross-origin"
                  className="mt-2 mb-2 ml-6 aspect-video w-[calc(100%-1.5rem)] max-w-2xl border border-border"
                />
              ) : null}
              {expanded ? (
                <blockquote
                  role="region"
                  aria-label={`Source ${index + 1}`}
                  className="mt-1 mb-2 ml-6 border-l-2 border-border pl-3 whitespace-pre-wrap text-muted-foreground"
                >
                  {citation.excerpt || 'This answer was saved before passages were kept with it.'}
                </blockquote>
              ) : null}
            </li>
          )
        })}
        {web.map((source, index) => {
          const href = safeUrl(source.url)
          return (
            <li key={`${source.url}-${index}`} className="flex min-w-0 gap-2 text-sm leading-6">
              <span className="shrink-0 text-muted-foreground tabular-nums">W{index + 1}.</span>
              {href === '' ? (
                <span className="truncate text-muted-foreground">{source.title || source.url}</span>
              ) : (
                <a
                  href={href}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex min-w-0 items-baseline gap-2 underline-offset-4 hover:underline"
                >
                  <span className="truncate text-primary">{source.title || hostname(href)}</span>
                  <span className="shrink-0 text-xs text-muted-foreground">{hostname(href)}</span>
                </a>
              )}
            </li>
          )
        })}
      </ol>
    </section>
  )
}

function lastSection(section: string | undefined): string {
  if (!section) return ''
  const parts = section.split(' › ')
  return parts[parts.length - 1] ?? ''
}

function hostname(url: string): string {
  try {
    return new URL(url).hostname.replace(/^www\./, '')
  } catch {
    return url
  }
}
