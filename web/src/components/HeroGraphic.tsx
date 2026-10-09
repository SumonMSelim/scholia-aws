import { AudioLines, FileText, Presentation } from 'lucide-react'

const sources = [
  { icon: AudioLines, label: 'Lecture', kind: 'audio' },
  { icon: Presentation, label: 'Slides', kind: 'slides' },
  { icon: FileText, label: 'Notes', kind: 'pages' },
] as const

const citations = [
  { icon: AudioLines, label: '12:41' },
  { icon: Presentation, label: 'Slide 8' },
  { icon: FileText, label: 'p. 3' },
] as const

// Purely decorative: the surrounding copy carries the meaning, so the whole
// graphic is hidden from assistive technology.
export function HeroGraphic() {
  return (
    <div
      className="hero-graphic relative isolate mx-auto w-full max-w-xl select-none"
      aria-hidden
      data-testid="hero-graphic"
    >
      <div className="hero-blob hero-blob-a" />
      <div className="hero-blob hero-blob-b" />

      <div className="grid grid-cols-[minmax(0,1fr)_2.5rem_minmax(0,1.4fr)] items-center gap-y-3 sm:grid-cols-[minmax(0,1fr)_4rem_minmax(0,1.4fr)]">
        <div className="flex flex-col gap-3">
          {sources.map(({ icon: Icon, label, kind }, i) => (
            <div
              key={label}
              className="hero-tile hero-float rounded-lg border bg-card/90 p-3 shadow-sm backdrop-blur"
              style={{ animationDelay: `${i * 0.6}s` }}
            >
              <div className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
                <Icon className="size-3.5" />
                {label}
              </div>
              <div className="mt-2">
                {kind === 'audio' && (
                  <div className="flex h-8 items-end gap-0.5">
                    {Array.from({ length: 18 }, (_, j) => (
                      <span
                        key={j}
                        className="hero-bar w-1 flex-1 rounded-sm bg-foreground/70"
                        style={{ animationDelay: `${(j % 6) * 0.12}s`, height: `${30 + ((j * 37) % 60)}%` }}
                      />
                    ))}
                  </div>
                )}
                {kind === 'slides' && (
                  <div className="aspect-video rounded-sm border bg-muted/60 p-1.5">
                    <div className="h-1.5 w-2/3 rounded-sm bg-foreground/60" />
                    <div className="mt-1.5 space-y-1">
                      <div className="h-1 w-full rounded-sm bg-foreground/25" />
                      <div className="h-1 w-5/6 rounded-sm bg-foreground/25" />
                      <div className="h-1 w-3/5 rounded-sm bg-foreground/25" />
                    </div>
                  </div>
                )}
                {kind === 'pages' && (
                  <div className="space-y-1">
                    <div className="h-1 w-full rounded-sm bg-foreground/25" />
                    <div className="hero-highlight h-1 w-4/5 rounded-sm bg-foreground/25" />
                    <div className="h-1 w-11/12 rounded-sm bg-foreground/25" />
                    <div className="h-1 w-2/3 rounded-sm bg-foreground/25" />
                  </div>
                )}
              </div>
            </div>
          ))}
        </div>

        <div className="flex h-full flex-col justify-around py-6">
          {sources.map(({ label }, i) => (
            <div key={label} className="relative h-px w-full bg-border">
              <span className="hero-dot absolute top-1/2 size-1.5 rounded-full bg-foreground" style={{ animationDelay: `${i * 0.9}s` }} />
            </div>
          ))}
        </div>

        <div className="hero-tile rounded-lg border bg-card/90 p-4 shadow-md backdrop-blur">
          <div className="text-xs font-medium text-muted-foreground">Answer</div>
          <div className="mt-3 space-y-2">
            <div className="h-1.5 w-full rounded-sm bg-foreground/30" />
            <div className="h-1.5 w-11/12 rounded-sm bg-foreground/30" />
            <div className="h-1.5 w-4/5 rounded-sm bg-foreground/30" />
            <div className="h-1.5 w-full rounded-sm bg-foreground/30" />
            <div className="h-1.5 w-2/3 rounded-sm bg-foreground/30" />
          </div>
          <div className="mt-4 flex flex-wrap gap-1.5">
            {citations.map(({ icon: Icon, label }, i) => (
              <span
                key={label}
                className="hero-chip inline-flex items-center gap-1 rounded-full border bg-background px-2 py-0.5 text-[11px] font-medium"
                style={{ animationDelay: `${0.5 + i * 0.9}s` }}
              >
                <Icon className="size-3" />
                {label}
              </span>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}
