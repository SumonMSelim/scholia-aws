import { useEffect, useState, type FormEvent } from 'react'
import { ArrowLeft, CheckCircle2, CircleAlert, Globe, Loader2, MessageSquarePlus } from 'lucide-react'
import { ApiError, errorText, listSources, uploadSource, type Course, type Source } from '@/lib/api'
import { fieldClass, textareaClass } from '@/lib/field'
import { Link } from '@/components/Link'
import { useAppData } from '@/components/app/appDataContext'
import { SourceList } from '@/components/courses/SourceList'
import { UploadDropzone } from '@/components/courses/UploadDropzone'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'

const pollMs = 2000

type Upload = { key: number; name: string; state: 'uploading' | 'done' | 'error'; error?: string }

function message(err: unknown, fallback: string): string {
  if (err instanceof ApiError || err instanceof Error) return err.message
  return fallback
}

let uploadKey = 0

export function CoursePage({ courseId }: { courseId: string }) {
  const { courses, chats } = useAppData()
  if (courses === null) return <p className="px-6 py-10 text-sm text-muted-foreground">Loading course…</p>
  const course = courses.find((row) => row.id === courseId)
  if (!course) {
    return (
      <div className="mx-auto max-w-md px-6 py-24 text-center">
        <h1 className="text-xl font-semibold">Course not found</h1>
        <Link to="/knowledge" className="mt-3 inline-block text-sm text-primary underline-offset-4 hover:underline">
          Back to Knowledge
        </Link>
      </div>
    )
  }
  return <CourseDetail key={course.id} course={course} chats={(chats ?? []).filter((chat) => chat.course_id === course.id)} />
}

function CourseDetail({ course, chats }: { course: Course; chats: { id: string; title: string }[] }) {
  const { refreshUsage } = useAppData()
  const canEdit = course.mine !== false
  const [sources, setSources] = useState<Source[] | null>(null)
  const [sourceError, setSourceError] = useState<string | null>(null)
  const [uploads, setUploads] = useState<Upload[]>([])

  useEffect(() => {
    const ctrl = new AbortController()
    const tick = () => {
      listSources(course.id, ctrl.signal)
        .then((rows) => {
          if (ctrl.signal.aborted) return
          setSources(rows)
          setSourceError(null)
        })
        .catch((err: unknown) => {
          if (!ctrl.signal.aborted) setSourceError(message(err, 'could not list files'))
        })
    }
    tick()
    const timer = window.setInterval(tick, pollMs)
    return () => {
      ctrl.abort()
      window.clearInterval(timer)
    }
  }, [course.id])

  async function onFile(file: File) {
    uploadKey += 1
    const key = uploadKey
    setUploads((prev) => [{ key, name: file.name, state: 'uploading' }, ...prev])
    const set = (patch: Partial<Upload>) => setUploads((prev) => prev.map((row) => (row.key === key ? { ...row, ...patch } : row)))
    try {
      await uploadSource(course.id, file)
      set({ state: 'done' })
      setSources(await listSources(course.id))
    } catch (err) {
      set({ state: 'error', error: errorText(err, 'could not upload') })
    } finally {
      void refreshUsage()
    }
  }

  return (
    <div className="space-y-6 px-4 py-6 sm:px-6 sm:py-8 lg:px-8">
      <div className="space-y-3">
        <Link to="/knowledge" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="size-4" aria-hidden />
          Knowledge
        </Link>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex min-w-0 items-center gap-2">
            <h1 className="truncate text-2xl font-semibold tracking-tight">{course.title}</h1>
            {course.public ? (
              <Badge variant="secondary">
                <Globe aria-hidden />
                Public
              </Badge>
            ) : null}
          </div>
          <Link to={`/chat?course=${encodeURIComponent(course.id)}`} className={buttonVariants()}>
            <MessageSquarePlus aria-hidden />
            New chat in this course
          </Link>
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_22rem] 2xl:grid-cols-[minmax(0,1fr)_26rem]">
        <div className="min-w-0 space-y-6">
          {canEdit ? (
            <section aria-labelledby="add-files" className="space-y-3">
              <h2 id="add-files" className="text-sm font-medium">
                Add files
              </h2>
              <UploadDropzone onFile={(file) => void onFile(file)} />
              {uploads.length > 0 ? (
                <ul className="space-y-1 text-sm" aria-label="Uploads">
                  {uploads.map((row) => (
                    <li key={row.key} className="flex items-center gap-2">
                      {row.state === 'uploading' ? <Loader2 className="size-4 animate-spin text-muted-foreground" aria-hidden /> : null}
                      {row.state === 'done' ? <CheckCircle2 className="size-4 text-primary" aria-hidden /> : null}
                      {row.state === 'error' ? <CircleAlert className="size-4 text-destructive" aria-hidden /> : null}
                      <span className="truncate">{row.name}</span>
                      <span className={`ml-auto shrink-0 text-xs ${row.state === 'error' ? 'text-destructive' : 'text-muted-foreground'}`}>
                        {row.state === 'uploading' ? 'Uploading…' : row.state === 'done' ? 'Uploaded' : row.error}
                      </span>
                    </li>
                  ))}
                </ul>
              ) : null}
            </section>
          ) : (
            <p className="text-sm text-muted-foreground">This is a public course. You can read and ask it, but not change it.</p>
          )}

          <section aria-labelledby="files" className="space-y-3">
            <h2 id="files" className="text-sm font-medium">
              Files
            </h2>
            {sourceError ? (
              <Alert variant="destructive">
                <AlertDescription>{sourceError}</AlertDescription>
              </Alert>
            ) : sources === null ? (
              <p className="text-sm text-muted-foreground">Loading files…</p>
            ) : (
              <SourceList sources={sources} />
            )}
          </section>

          {canEdit ? <MarkdownNotes onFile={onFile} /> : null}
        </div>

        <div className="space-y-6">
          <Card size="sm">
            <CardHeader>
              <CardTitle>Chats</CardTitle>
            </CardHeader>
            <CardContent>
              {chats.length === 0 ? (
                <p className="text-sm text-muted-foreground">No chats in this course yet.</p>
              ) : (
                <ul className="-mx-2 space-y-0.5">
                  {chats.map((chat) => (
                    <li key={chat.id}>
                      <Link to={`/chat/${encodeURIComponent(chat.id)}`} className="block truncate rounded-md px-2 py-1.5 text-sm hover:bg-muted">
                        {chat.title || 'New chat'}
                      </Link>
                    </li>
                  ))}
                </ul>
              )}
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  )
}

function MarkdownNotes({ onFile }: { onFile: (file: File) => Promise<void> }) {
  const [title, setTitle] = useState('')
  const [body, setBody] = useState('')

  async function onSubmit(event: FormEvent) {
    event.preventDefault()
    const text = body.trim()
    if (text === '') return
    const file = new File([text], `${title.trim() || 'notes'}.md`, { type: 'text/markdown' })
    setTitle('')
    setBody('')
    await onFile(file)
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Paste markdown notes</CardTitle>
        <CardDescription>Adds the notes to this course as a markdown file.</CardDescription>
      </CardHeader>
      <CardContent>
        <form className="space-y-3" onSubmit={(event) => void onSubmit(event)}>
          <label className="block space-y-1.5 text-sm font-medium">
            Title
            <input aria-label="Markdown title" className={fieldClass} value={title} onChange={(event) => setTitle(event.target.value)} placeholder="Lecture notes" />
          </label>
          <label className="block space-y-1.5 text-sm font-medium">
            Content
            <textarea aria-label="Markdown content" className={textareaClass} value={body} onChange={(event) => setBody(event.target.value)} placeholder="Paste or write markdown." />
          </label>
          <Button type="submit" variant="outline" disabled={body.trim() === ''}>
            Add notes
          </Button>
        </form>
      </CardContent>
    </Card>
  )
}
