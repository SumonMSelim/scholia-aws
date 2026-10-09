import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { flushSync } from 'react-dom'
import { FileText } from 'lucide-react'
import {
  ApiError,
  createChat,
  deleteChatAttachment,
  errorText,
  limitCode,
  getChat,
  listSources,
  sendChatMessage,
  uploadChatAttachment,
  type AnswerEvent,
  type Chat,
  type ChatAttachment,
  type ChatMessage,
  type RefusalReason,
} from '@/lib/api'
import { availableModels, modelLabel, pickModel } from '@/lib/models'
import { navigate, useLocation } from '@/lib/router'
import { Link } from '@/components/Link'
import { useAppData } from '@/components/app/appDataContext'
import { SourceList } from '@/components/chat/SourceList'
import { MessageText } from '@/components/chat/MessageText'
import { ChatComposer, type ComposerSetup, type Prefill } from '@/components/chat/ChatComposer'

/** Starter actions fill the box. The student finishes the sentence or pastes the work. */
const starters = [
  { title: 'Ask the course', detail: 'Get an answer from your lectures and readings, with sources.', text: 'Explain ' },
  { title: 'Solve an assignment', detail: 'Work through a problem step by step.', text: 'Help me solve this assignment step by step:\n\n' },
  { title: 'Review my work', detail: 'Get feedback on a draft or an answer.', text: 'Review my answer and point out mistakes and gaps:\n\n' },
  { title: 'Take a mock exam', detail: 'Practise with exam-style questions, then get graded.', text: 'Give me a mock exam on this course: 5 questions, one at a time, then grade my answers.' },
] as const

type Row = ChatMessage & { error?: string; limit?: 'quota' | 'paused' | null; pending?: boolean }

function message(err: unknown, fallback: string): string {
  if (err instanceof ApiError || err instanceof Error) return err.message
  return fallback
}

let nextId = 0
function localId(): string {
  nextId += 1
  return `local-${nextId}`
}

export function ChatPage({ chatId }: { chatId: string | null }) {
  const { courses, models, defaults, settings, chats, refreshChats, refreshUsage } = useAppData()
  const { query } = useLocation()
  const [shown, setShown] = useState(chatId)
  const [chat, setChat] = useState<Chat | null>(null)
  const [rows, setRows] = useState<Row[]>([])
  const [loadError, setLoadError] = useState<{ id: string; text: string } | null>(null)
  const [streaming, setStreaming] = useState(false)
  const [coursePick, setCoursePick] = useState<{ query: string; id: string } | null>(null)
  const [modelPick, setModelPick] = useState('')
  const [prefill, setPrefill] = useState<Prefill | null>(null)
  const created = useRef<string | null>(null)
  const streamChat = useRef<string | null>(null)
  const abort = useRef<AbortController | null>(null)
  const bottom = useRef<HTMLLIElement>(null)
  // Attaching and sending both need a chat. The ref sees a chat created a moment ago, and the
  // promise stops two quick attachments from creating two chats.
  const chatRef = useRef<Chat | null>(null)
  const creating = useRef<Promise<Chat> | null>(null)
  useLayoutEffect(() => {
    chatRef.current = chat
  }, [chat])

  // Switching chats resets the thread. A chat created on this page is already in state.
  if (chatId !== shown) {
    setShown(chatId)
    if (chat?.id !== chatId) {
      setChat(null)
      setRows([])
    }
  }

  useEffect(() => {
    if (streamChat.current !== chatId) abort.current?.abort()
  }, [chatId])

  useEffect(() => {
    if (chatId === null) return
    if (chatId === created.current) {
      created.current = null
      return
    }
    const ctrl = new AbortController()
    getChat(chatId, ctrl.signal)
      .then((res) => {
        if (ctrl.signal.aborted) return
        setChat(res.chat)
        setRows(res.messages)
      })
      .catch((err: unknown) => {
        if (!ctrl.signal.aborted) setLoadError({ id: chatId, text: message(err, 'could not open the chat') })
      })
    return () => ctrl.abort()
  }, [chatId])

  useEffect(() => () => abort.current?.abort(), [])

  useEffect(() => {
    bottom.current?.scrollIntoView?.({ block: 'end' })
  }, [rows])

  const courseRows = courses ?? []
  const choices = availableModels(models, settings?.providers, defaults?.server_models)
  const queryCourse = query.get('course') ?? ''
  const valid = (id: string | undefined) => (id && courseRows.some((course) => course.id === id) ? id : '')
  // A pick made under a different ?course= no longer applies, so the link wins.
  const courseId = valid(coursePick?.query === queryCourse ? coursePick.id : undefined) || valid(queryCourse) || (courseRows[0]?.id ?? '')

  // Citations carry a source id; the course's file list names it and links its lecture recording.
  const sourceCourse = chat?.course_id ?? courseId
  const [named, setNamed] = useState<{ course: string; names: Record<string, string>; videos: Record<string, string> }>({
    course: '',
    names: {},
    videos: {},
  })
  useEffect(() => {
    if (sourceCourse === '') return
    const ctrl = new AbortController()
    listSources(sourceCourse, ctrl.signal)
      .then((list) =>
        setNamed({
          course: sourceCourse,
          names: Object.fromEntries(list.map((source) => [source.id, source.name])),
          videos: Object.fromEntries(list.flatMap((source) => (source.youtube_id ? [[source.id, source.youtube_id]] : []))),
        }),
      )
      .catch(() => {})
    return () => ctrl.abort()
  }, [sourceCourse])
  const sourceNames = named.course === sourceCourse ? named.names : {}
  const sourceVideos = named.course === sourceCourse ? named.videos : {}
  const modelChoice = choices.some((row) => row.id === modelPick) ? modelPick : pickModel(choices, settings?.default_model, defaults?.chat_model)
  const error = loadError?.id === chatId ? loadError.text : null
  const loading = chatId !== null && chat?.id !== chatId && error === null

  async function ensureChat(): Promise<Chat> {
    if (chatRef.current) return chatRef.current
    if (!creating.current) {
      creating.current = createChat(courseId, modelChoice)
        .then((fresh) => {
          created.current = fresh.id
          chatRef.current = fresh
          // A send in flight belongs to this chat, so the route change must not abort it.
          streamChat.current = fresh.id
          // The chat must be in state before the route changes, or the switch would reset the thread.
          flushSync(() => setChat(fresh))
          navigate(`/chat/${encodeURIComponent(fresh.id)}`, { replace: true })
          void refreshChats()
          return fresh
        })
        .finally(() => {
          creating.current = null
        })
    }
    return creating.current
  }

  async function onAttach(file: File): Promise<ChatAttachment> {
    try {
      const target = await ensureChat()
      return await uploadChatAttachment(target.id, file)
    } finally {
      void refreshUsage()
    }
  }

  async function onRemoveAttachment(attachment: ChatAttachment): Promise<void> {
    if (chatRef.current) await deleteChatAttachment(chatRef.current.id, attachment.id)
  }

  async function onSend(question: string, attachments: ChatAttachment[]) {
    const ctrl = new AbortController()
    abort.current = ctrl
    const assistantId = localId()
    const now = new Date().toISOString()
    setRows((prev) => [
      ...prev,
      { id: localId(), role: 'user', text: question, mode: 'answer', attachments, created_at: now },
      { id: assistantId, role: 'assistant', text: '', mode: 'answer', created_at: now, pending: true },
    ])
    setStreaming(true)

    function patch(update: (row: Row) => Row) {
      setRows((prev) => prev.map((row) => (row.id === assistantId ? update(row) : row)))
    }

    function onEvent(event: AnswerEvent) {
      patch((row) => {
        if (event.type === 'delta') return { ...row, text: row.text + event.text }
        if (event.type === 'citation') return { ...row, citations: event.citations, web: event.web ?? [] }
        // A guardrail can block after some text streamed. The refusal replaces it, so no blocked text stays on screen.
        if (event.type === 'refusal') return { ...row, text: '', refusal: event.text, refusal_reason: event.reason }
        return { ...row, error: event.message }
      })
    }

    try {
      const target = await ensureChat()
      streamChat.current = target.id
      await sendChatMessage(
        target.id,
        question,
        'answer',
        onEvent,
        ctrl.signal,
        attachments.map((row) => row.id),
      )
    } catch (err) {
      if (!ctrl.signal.aborted) patch((row) => ({ ...row, error: errorText(err, 'could not answer'), limit: limitCode(err) }))
    } finally {
      patch((row) => ({ ...row, pending: false }))
      if (abort.current === ctrl) {
        abort.current = null
        streamChat.current = null
      }
      setStreaming(false)
      void refreshChats()
      void refreshUsage()
    }
  }

  const listed = chat ? chats?.find((row) => row.id === chat.id) : undefined
  const lockedCourse = chat ? courseRows.find((course) => course.id === chat.course_id) : undefined
  const setup: ComposerSetup = chat
    ? { locked: true, courseTitle: lockedCourse?.title ?? 'Course', modelLabel: modelLabel(models, chat.model) }
    : {
        locked: false,
        courses: courseRows,
        courseId,
        onCourseChange: (id) => setCoursePick({ query: queryCourse, id }),
        models: choices,
        model: modelChoice,
        onModelChange: setModelPick,
      }

  const empty = rows.length === 0
  const noCourses = courses !== null && courseRows.length === 0 && !chat

  return (
    <div className="flex h-full flex-col">
      {chat ? (
        <div className="border-b border-border px-4 py-2.5 sm:px-6 lg:px-8">
          <h1 className="truncate text-sm font-medium">{listed?.title || chat.title || 'New chat'}</h1>
        </div>
      ) : null}
      <div className="min-h-0 flex-1 overflow-y-auto">
        {error ? (
          <p role="alert" className="px-4 py-10 text-sm text-destructive sm:px-6 lg:px-8">
            {error}
          </p>
        ) : loading ? (
          <p className="px-4 py-10 text-sm text-muted-foreground sm:px-6 lg:px-8">Loading chat…</p>
        ) : empty ? (
          <div className="flex flex-col items-center px-4 pt-[min(20vh,11rem)] pb-8 text-center sm:px-6 lg:px-8">
            <h1 className="text-2xl font-semibold tracking-tight sm:text-3xl">Study with your course</h1>
            {noCourses ? (
              <p className="mt-3 text-sm text-muted-foreground">
                You have no courses yet.{' '}
                <Link to="/knowledge" className="font-medium text-primary underline-offset-4 hover:underline">
                  Create a course in Knowledge
                </Link>{' '}
                and add your lectures to start.
              </p>
            ) : (
              <>
                <p className="mt-3 max-w-lg text-sm text-muted-foreground">
                  Choose a course and a model below. This chat stays on that course: answers come from its lectures, slides, and
                  assignments, with the web filling gaps and sources shown for each.
                </p>
                <ul className="mt-8 grid w-full max-w-5xl gap-2 text-left sm:grid-cols-2 xl:grid-cols-4" aria-label="Starters">
                  {starters.map((row) => (
                    <li key={row.title}>
                      <button
                        type="button"
                        className="h-full w-full border border-border bg-card px-4 py-3 text-left transition-colors hover:border-primary focus-visible:border-primary focus-visible:outline-none disabled:opacity-50"
                        disabled={courseId === ''}
                        onClick={() => setPrefill({ text: row.text, key: Date.now() })}
                      >
                        <span className="block text-sm font-medium">{row.title}</span>
                        <span className="mt-0.5 block text-xs text-muted-foreground">{row.detail}</span>
                      </button>
                    </li>
                  ))}
                </ul>
              </>
            )}
          </div>
        ) : (
          <ul className="flex w-full flex-col gap-6 px-4 py-6 sm:px-6 lg:px-8" aria-label="Messages">
            {rows.map((row) =>
              row.role === 'user' ? (
                <li key={row.id} className="flex flex-col items-end gap-1.5">
                  {row.attachments && row.attachments.length > 0 ? <FileChips files={row.attachments} /> : null}
                  <div className="max-w-[min(85%,90ch)] border border-border bg-secondary px-4 py-2.5 whitespace-pre-wrap">{row.text}</div>
                </li>
              ) : (
                <li key={row.id} className="min-w-0 max-w-[120ch] space-y-3 leading-7">
                  {row.text !== '' ? <MessageText text={row.text} /> : null}
                  {row.pending && row.text === '' ? <Typing /> : null}
                  {row.refusal ? <RefusalNotice text={row.refusal} reason={row.refusal_reason} courseId={chat?.course_id ?? courseId} /> : null}
                  {row.error && row.limit ? (
                    <LimitNotice text={row.error} code={row.limit} />
                  ) : row.error ? (
                    <p role="alert" className="text-sm text-destructive">
                      {row.error}
                    </p>
                  ) : null}
                  <SourceList citations={row.citations ?? []} web={row.web ?? []} names={sourceNames} videos={sourceVideos} />
                </li>
              ),
            )}
            <li ref={bottom} aria-hidden className="h-px" />
          </ul>
        )}
      </div>
      <ChatComposer
        setup={setup}
        streaming={streaming}
        prefill={prefill}
        onAttach={onAttach}
        onRemoveAttachment={onRemoveAttachment}
        disabled={loading || error !== null || (!chat && courseId === '')}
        onSend={(question, attachments) => void onSend(question, attachments)}
        onStop={() => abort.current?.abort()}
      />
    </div>
  )
}

function Typing() {
  return (
    <span role="status" aria-label="Answering" className="inline-flex gap-1 py-2">
      {[0, 150, 300].map((delay) => (
        <span key={delay} className="size-1.5 animate-bounce rounded-full bg-muted-foreground" style={{ animationDelay: `${delay}ms` }} />
      ))}
    </span>
  )
}

function FileChips({ files }: { files: ChatAttachment[] }) {
  return (
    <ul className="flex max-w-[85%] flex-wrap justify-end gap-1.5" aria-label="Files">
      {files.map((file) => (
        <li key={file.id} className="inline-flex h-7 max-w-[16rem] items-center gap-1.5 border border-border bg-card px-2 text-xs">
          <FileText className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
          <span className="truncate">{file.name}</span>
        </li>
      ))}
    </ul>
  )
}

/** A quota or pause stops the answer. It reuses the refusal notice so it reads as a status, not a crash. */
function LimitNotice({ text, code }: { text: string; code: 'quota' | 'paused' }) {
  return (
    <div role="alert" aria-label={code === 'quota' ? 'Daily limit reached' : 'Service paused'} data-limit={code} className="border border-border border-l-4 border-l-primary bg-muted/50 px-4 py-3 text-sm">
      <p className="font-medium">{code === 'quota' ? 'Daily limit reached' : 'Scholia is paused'}</p>
      <p className="mt-1 whitespace-pre-wrap text-muted-foreground">{text}</p>
    </div>
  )
}

function RefusalNotice({ text, reason, courseId }: { text: string; reason?: RefusalReason; courseId: string }) {
  return (
    <div role="note" aria-label="Refusal" data-reason={reason ?? 'no_material'} className="border border-border border-l-4 border-l-muted-foreground bg-muted/50 px-4 py-3 text-sm">
      <p className="whitespace-pre-wrap">{text}</p>
      {reason === 'off_topic' ? (
        <p className="mt-1.5 text-muted-foreground">This chat only covers its course. Ask about the lectures, readings, or assignments.</p>
      ) : reason === 'unsafe' ? null : (
        <p className="mt-1.5 text-muted-foreground">
          The course material doesn't cover this yet.{' '}
          {courseId ? (
            <Link to={`/knowledge/${encodeURIComponent(courseId)}`} className="font-medium text-primary underline-offset-4 hover:underline">
              Add files in Knowledge
            </Link>
          ) : (
            'Add files in Knowledge'
          )}{' '}
          to answer questions like this.
        </p>
      )}
    </div>
  )
}
