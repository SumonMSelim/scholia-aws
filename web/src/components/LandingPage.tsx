import {
  BookOpenCheck,
  FolderPlus,
  Globe,
  GraduationCap,
  MessagesSquare,
  MonitorPlay,
  Presentation,
  Quote,
  Upload,
  type LucideIcon,
} from 'lucide-react'
import { useState } from 'react'
import { ApiError, limitCode, startGuest } from '@/lib/api'
import { faq, features, formats, hero, steps, type FeatureKey, type StepKey } from '@/lib/content'
import { HeroGraphic } from '@/components/HeroGraphic'
import { SignIn } from '@/components/courses/SignIn'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'

const featureIcons: Record<FeatureKey, LucideIcon> = {
  sources: Quote,
  recordings: MonitorPlay,
  slides: Presentation,
  exams: BookOpenCheck,
  help: GraduationCap,
  web: Globe,
}

const stepIcons: Record<StepKey, LucideIcon> = {
  course: FolderPlus,
  files: Upload,
  ask: MessagesSquare,
  test: BookOpenCheck,
}

function guestError(err: unknown): string {
  const code = limitCode(err)
  if (code === 'paused') return 'The demo is paused right now. Please try again later.'
  if (code === 'quota') return 'Too many people are trying the demo right now. Please try again in a few minutes.'
  if (err instanceof ApiError || err instanceof Error) return err.message
  return 'The demo did not start. Please try again.'
}

export function LandingPage({ onSignedIn }: { onSignedIn: (path?: string) => void }) {
  const [starting, setStarting] = useState(false)
  const [demoError, setDemoError] = useState<string | null>(null)

  async function onDemo() {
    setStarting(true)
    setDemoError(null)
    try {
      await startGuest()
      // A guest has no courses of their own, so the chat opens on a public course.
      onSignedIn('/chat')
    } catch (err) {
      setDemoError(guestError(err))
      setStarting(false)
    }
  }

  return (
    <main>
      <section className="mx-auto grid max-w-6xl items-center gap-12 px-6 pt-10 pb-20 lg:grid-cols-2 lg:pt-20">
        <div className="space-y-6">
          <h1 className="text-4xl font-semibold tracking-tight text-balance sm:text-5xl">{hero.title}</h1>
          <p className="max-w-prose text-lg text-muted-foreground">{hero.body}</p>
          <div className="max-w-md space-y-1.5">
            <Button type="button" size="lg" className="w-full" disabled={starting} onClick={() => void onDemo()}>
              {starting ? 'Starting the demo…' : 'Try the demo, no sign-up'}
            </Button>
            <p className="text-xs text-muted-foreground">{hero.demoNote}</p>
            {demoError ? (
              <p role="alert" className="text-sm text-destructive">
                {demoError}
              </p>
            ) : null}
          </div>
          <Card className="max-w-md">
            <CardHeader className="pb-3">
              <CardTitle className="text-base">Sign in to use your own files</CardTitle>
              <CardDescription>We email you a code. No password needed.</CardDescription>
            </CardHeader>
            <CardContent>
              <SignIn onChange={(next) => next && onSignedIn()} />
            </CardContent>
          </Card>
        </div>
        <HeroGraphic />
      </section>

      <Separator />

      <section className="mx-auto max-w-6xl px-6 py-20" aria-labelledby="features-heading">
        <h2 id="features-heading" className="text-2xl font-semibold tracking-tight">
          What Scholia does
        </h2>
        <div className="mt-8 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {features.map(({ key, title, body }) => {
            const Icon = featureIcons[key]
            return (
              <Card key={key}>
                <CardHeader>
                  <Icon className="size-5 text-muted-foreground" aria-hidden />
                  <h3 className="text-base leading-snug font-medium">{title}</h3>
                  <CardDescription>{body}</CardDescription>
                </CardHeader>
              </Card>
            )
          })}
        </div>
      </section>

      <section className="bg-muted/40 py-20" aria-labelledby="how-heading">
        <div className="mx-auto max-w-6xl px-6">
          <h2 id="how-heading" className="text-2xl font-semibold tracking-tight">
            How it works
          </h2>
          <ol className="mt-8 grid gap-6 sm:grid-cols-2 lg:grid-cols-4">
            {steps.map(({ key, title, body }, i) => {
              const Icon = stepIcons[key]
              return (
                <li key={key} className="space-y-2">
                  <div className="flex items-center gap-2 text-sm text-muted-foreground">
                    <span className="font-mono">{String(i + 1).padStart(2, '0')}</span>
                    <Icon className="size-4" aria-hidden />
                  </div>
                  <h3 className="font-medium">{title}</h3>
                  <p className="text-sm text-muted-foreground">{body}</p>
                </li>
              )
            })}
          </ol>
        </div>
      </section>

      <section className="mx-auto max-w-6xl px-6 py-20" aria-labelledby="formats-heading">
        <h2 id="formats-heading" className="text-2xl font-semibold tracking-tight">
          Files you can upload
        </h2>
        <dl className="mt-8 grid gap-x-8 gap-y-4 sm:grid-cols-2">
          {formats.map(({ kind, files }) => (
            <div key={kind} className="border-l-2 border-border pl-4">
              <dt className="font-medium">{kind}</dt>
              <dd className="text-sm text-muted-foreground">{files}</dd>
            </div>
          ))}
        </dl>
      </section>

      <section className="bg-muted/40 py-20" aria-labelledby="faq-heading">
        <div className="mx-auto max-w-3xl px-6">
          <h2 id="faq-heading" className="text-2xl font-semibold tracking-tight">
            Questions
          </h2>
          <div className="mt-8 divide-y divide-border border-y border-border">
            {faq.map(({ q, a }) => (
              <details key={q} className="group py-4">
                <summary className="cursor-pointer list-none font-medium marker:hidden">
                  <span className="flex items-center justify-between gap-4">
                    {q}
                    <span className="text-muted-foreground transition-transform group-open:rotate-45" aria-hidden>
                      +
                    </span>
                  </span>
                </summary>
                <p className="mt-2 text-muted-foreground">{a}</p>
              </details>
            ))}
          </div>
        </div>
      </section>
    </main>
  )
}
