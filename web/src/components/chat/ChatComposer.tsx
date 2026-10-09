import { useLayoutEffect, useRef, useState, type FormEvent, type ReactNode } from 'react'
import { ArrowUp, BookOpen, Cpu, FileText, Paperclip, Square, X } from 'lucide-react'
import { limitCode, errorText, chatAttachmentProblem, chatAttachmentTypes, maxChatAttachments, type ChatAttachment, type Course, type Model } from '@/lib/api'
import { modelOptions } from '@/lib/models'
import { Button } from '@/components/ui/button'
import { Select } from '@/components/ui/select'

const maxRows = 8

const attachAccept = Object.keys(chatAttachmentTypes)
  .map((ext) => `.${ext}`)
  .join(',')

/** Text a starter action puts in the box. A new key replaces the text even when it repeats. */
export type Prefill = { text: string; key: number }

type Pending = { key: number; name: string; status: 'uploading' | 'ready' | 'failed'; error?: string; limit?: 'quota' | 'paused' | null; attachment?: ChatAttachment }

let nextKey = 0

/** Course and model are chosen for a new chat only. After the first message or file they are locked. */
export type ComposerSetup =
  | {
      locked: false
      courses: Course[]
      courseId: string
      onCourseChange: (id: string) => void
      models: Model[]
      model: string
      onModelChange: (id: string) => void
    }
  | { locked: true; courseTitle: string; modelLabel: string }

export function ChatComposer({
  setup,
  streaming,
  disabled,
  prefill,
  onAttach,
  onRemoveAttachment,
  onSend,
  onStop,
}: {
  setup: ComposerSetup
  streaming: boolean
  disabled?: boolean
  prefill?: Prefill | null
  /** Uploads a file to this chat only. It never enters the course's knowledge. */
  onAttach: (file: File) => Promise<ChatAttachment>
  onRemoveAttachment: (attachment: ChatAttachment) => Promise<void>
  onSend: (question: string, attachments: ChatAttachment[]) => void
  onStop: () => void
}) {
  const [question, setQuestion] = useState('')
  const [appliedPrefill, setAppliedPrefill] = useState<number | null>(null)
  const [pending, setPending] = useState<Pending[]>([])
  const textRef = useRef<HTMLTextAreaElement>(null)
  const fileRef = useRef<HTMLInputElement>(null)

  if (prefill && prefill.key !== appliedPrefill) {
    setAppliedPrefill(prefill.key)
    setQuestion(prefill.text)
  }

  useLayoutEffect(() => {
    if (prefill) textRef.current?.focus()
  }, [prefill])
  const courseTitle = setup.locked ? setup.courseTitle : setup.courses.find((course) => course.id === setup.courseId)?.title

  useLayoutEffect(() => {
    const node = textRef.current
    if (!node) return
    node.style.height = 'auto'
    const line = parseFloat(getComputedStyle(node).lineHeight) || 22
    node.style.height = `${Math.min(node.scrollHeight, line * maxRows + 16)}px`
  }, [question])

  const uploading = pending.some((row) => row.status === 'uploading')
  const live = pending.filter((row) => row.status !== 'failed').length

  function patch(key: number, next: Partial<Pending>) {
    setPending((rows) => rows.map((row) => (row.key === key ? { ...row, ...next } : row)))
  }

  async function onFiles(files: FileList | null) {
    if (!files) return
    let room = maxChatAttachments - live
    // Uploads run one at a time, so a new chat is created once by the first file.
    for (const file of Array.from(files)) {
      nextKey += 1
      const key = nextKey
      const problem = room <= 0 ? `${file.name}: a message can carry at most ${maxChatAttachments} files.` : chatAttachmentProblem(file)
      if (problem) {
        setPending((rows) => [...rows, { key, name: file.name, status: 'failed', error: problem }])
        continue
      }
      room -= 1
      setPending((rows) => [...rows, { key, name: file.name, status: 'uploading' }])
      try {
        const attachment = await onAttach(file)
        patch(key, { status: 'ready', attachment })
      } catch (err) {
        patch(key, { status: 'failed', error: errorText(err, 'could not upload'), limit: limitCode(err) })
      }
    }
  }

  function onRemove(row: Pending) {
    setPending((rows) => rows.filter((item) => item.key !== row.key))
    if (row.attachment) void onRemoveAttachment(row.attachment).catch(() => {})
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault()
    const trimmed = question.trim()
    if (trimmed === '' || disabled || streaming || uploading) return
    const ready = pending.flatMap((row) => (row.status === 'ready' && row.attachment ? [row.attachment] : []))
    onSend(trimmed, ready)
    setQuestion('')
    setPending([])
  }

  return (
    <form className="w-full px-4 pb-4 sm:px-6 lg:px-8" onSubmit={onSubmit}>
      <div className="border border-input bg-card transition-colors focus-within:border-primary">
        {pending.length > 0 ? (
          <ul className="flex flex-wrap gap-1.5 px-2.5 pt-2.5" aria-label="Attached files">
            {pending.map((row) => (
              <li
                key={row.key}
                className={`inline-flex h-8 max-w-full items-center gap-1.5 border px-2 text-xs sm:max-w-[20rem] ${row.status === 'failed' ? 'border-destructive text-destructive' : 'border-border bg-secondary'}`}
                title={row.error}
              >
                <FileText className="size-3.5 shrink-0" aria-hidden />
                <span className="truncate">{row.name}</span>
                <span className={`shrink-0 ${row.status === 'failed' ? '' : 'text-muted-foreground'}`}>
                  {row.status === 'uploading' ? 'Uploading…' : row.status === 'ready' ? 'Ready' : row.limit === 'quota' ? 'Limit reached' : row.limit === 'paused' ? 'Paused' : 'Failed'}
                </span>
                {row.status !== 'uploading' ? (
                  <button
                    type="button"
                    className="shrink-0 text-muted-foreground hover:text-foreground"
                    aria-label={`Remove ${row.name}`}
                    onClick={() => onRemove(row)}
                  >
                    <X className="size-3.5" aria-hidden />
                  </button>
                ) : null}
              </li>
            ))}
          </ul>
        ) : null}
        {pending.some((row) => row.status === 'failed' && row.error) ? (
          <p role="alert" className="px-3 pt-2 text-xs text-destructive">
            {pending.filter((row) => row.status === 'failed').at(-1)?.error}
          </p>
        ) : null}
        <textarea
          ref={textRef}
          id="chat-question"
          aria-label="Message"
          rows={1}
          className="block max-h-60 w-full resize-none bg-transparent px-4 pt-3.5 pb-2 text-[15px] leading-6 outline-none placeholder:text-muted-foreground"
          placeholder={courseTitle ? 'Ask anything about this course…' : 'Choose a course to start…'}
          value={question}
          disabled={disabled}
          onChange={(event) => setQuestion(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
              event.preventDefault()
              event.currentTarget.form?.requestSubmit()
            }
          }}
        />
        <div className="flex flex-wrap items-center gap-1.5 px-2.5 pb-2.5">
          {setup.locked ? (
            <>
              <Chip icon={<BookOpen className="size-3.5" aria-hidden />} label="Course" value={setup.courseTitle} />
              <Chip icon={<Cpu className="size-3.5" aria-hidden />} label="Model" value={setup.modelLabel} />
            </>
          ) : (
            <>
              <Select
                label="Course"
                size="sm"
                icon={<BookOpen aria-hidden />}
                className="max-w-[13rem]"
                value={setup.courseId}
                placeholder="No courses"
                disabled={setup.courses.length === 0}
                options={setup.courses.map((course) => ({ value: course.id, label: course.title }))}
                onValueChange={setup.onCourseChange}
              />
              <Select
                label="Model"
                size="sm"
                icon={<Cpu aria-hidden />}
                className="max-w-[13rem]"
                value={setup.model}
                placeholder="Default model"
                disabled={setup.models.length === 0}
                options={modelOptions(setup.models)}
                onValueChange={setup.onModelChange}
              />
            </>
          )}
          <div className="ml-auto flex items-center gap-1">
            <input
              ref={fileRef}
              type="file"
              className="sr-only"
              tabIndex={-1}
              accept={attachAccept}
              multiple
              aria-label="Attach files"
              onChange={(event) => {
                void onFiles(event.target.files)
                event.target.value = ''
              }}
            />
            <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-label="Attach files to this chat"
              title="Attach files to this chat"
              disabled={disabled || streaming}
              onClick={() => fileRef.current?.click()}
            >
              <Paperclip aria-hidden />
            </Button>
            {streaming ? (
              <Button type="button" size="icon" aria-label="Stop" onClick={onStop}>
                <Square className="size-3.5 fill-current" aria-hidden />
              </Button>
            ) : (
              <Button type="submit" size="icon" aria-label="Send" disabled={disabled || uploading || question.trim() === ''}>
                <ArrowUp aria-hidden />
              </Button>
            )}
          </div>
        </div>
      </div>
      <p className="mt-2 hidden text-center text-xs text-muted-foreground sm:block">Enter to send · Shift+Enter for a new line</p>
    </form>
  )
}

function Chip({ icon, label, value }: { icon: ReactNode; label: string; value: string }) {
  return (
    <span className="inline-flex h-8 max-w-[13rem] items-center gap-1.5 border border-border bg-secondary px-2.5 text-xs text-secondary-foreground" aria-label={`${label}: ${value}`}>
      {icon}
      <span className="truncate">{value}</span>
    </span>
  )
}
