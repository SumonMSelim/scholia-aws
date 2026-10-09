export type Health = {
  status: string
  version: string
  commit: string
  env: string
  time: string
}

export class ApiError extends Error {
  readonly status: number
  /** The error body's code, such as quota or paused. Empty when the body had none. */
  readonly code: string

  constructor(status: number, message: string, code = '') {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

/**
 * The text to show for a failed call. A quota error gets the reset time, and a paused
 * service says so plainly. Other errors keep their own message.
 */
export function errorText(err: unknown, fallback: string): string {
  const code = limitCode(err)
  if (code === 'quota') return `${(err as ApiError).message} Resets at 00:00 UTC.`
  if (code === 'paused') return 'Scholia is paused right now. Please try again later.'
  if (err instanceof Error && err.message !== '') return err.message
  return fallback
}

/** A daily quota ran out (429) or the operator paused the service (503). */
export function limitCode(err: unknown): 'quota' | 'paused' | null {
  if (!(err instanceof ApiError)) return null
  if (err.code === 'quota') return 'quota'
  if (err.code === 'paused') return 'paused'
  return null
}

export async function fetchHealth(signal?: AbortSignal): Promise<Health> {
  const res = await apiFetch('/api/health', { signal, headers: { Accept: 'application/json' } })
  if (!res.ok) {
    throw new ApiError(res.status, `health check failed with status ${res.status}`)
  }
  const body: unknown = await res.json()
  if (!isHealth(body)) {
    throw new ApiError(res.status, 'unexpected health response')
  }
  return body
}

export type Course = {
  id: string
  title: string
  public?: boolean
  mine?: boolean
}

const sessionKey = 'scholia.session'
const emailKey = 'scholia.email'
const guestKey = 'scholia.guest_expires'

export function sessionToken(): string {
  return sessionStorage.getItem(sessionKey) ?? ''
}

// Clearing the token also forgets the email so a signed-out tab shows no account.
export function setSessionToken(token: string): void {
  if (token === '') {
    sessionStorage.removeItem(sessionKey)
    sessionStorage.removeItem(emailKey)
    sessionStorage.removeItem(guestKey)
  } else sessionStorage.setItem(sessionKey, token)
}

export function sessionEmail(): string {
  return sessionStorage.getItem(emailKey) ?? ''
}

/** When the guest session's data is deleted. Empty for an email account. */
export function guestExpiresAt(): string {
  return sessionStorage.getItem(guestKey) ?? ''
}

export function isGuest(): boolean {
  return guestExpiresAt() !== ''
}

export type Usage = {
  guest: boolean
  limits: { messages: number; uploads: number; web: number }
  used: { messages: number; uploads: number; web: number }
  resets_at: string
}

export async function startGuest(): Promise<void> {
  const res = await apiFetch('/api/session/guest', { method: 'POST', headers: { Accept: 'application/json' } })
  if (!res.ok) throw await readError(res, 'could not start the demo')
  const body: unknown = await res.json()
  if (!isGuestSession(body)) throw new ApiError(res.status, 'unexpected guest session')
  setSessionToken(body.token)
  sessionStorage.setItem(guestKey, body.expires_at)
}

export async function getUsage(signal?: AbortSignal): Promise<Usage> {
  const res = await apiFetch('/api/account/usage', { signal, headers: { Accept: 'application/json' } })
  if (!res.ok) throw await readError(res, 'could not read usage')
  const body: unknown = await res.json()
  if (!isUsage(body)) throw new ApiError(res.status, 'unexpected usage')
  return body
}

function isGuestSession(v: unknown): v is { token: string; guest: true; expires_at: string } {
  if (typeof v !== 'object' || v === null) return false
  const row = v as Record<string, unknown>
  return typeof row.token === 'string' && row.token !== '' && row.guest === true && typeof row.expires_at === 'string' && row.expires_at !== ''
}

function isCounts(v: unknown): v is Usage['limits'] {
  if (typeof v !== 'object' || v === null) return false
  const row = v as Record<string, unknown>
  return ['messages', 'uploads', 'web'].every((k) => typeof row[k] === 'number')
}

function isUsage(v: unknown): v is Usage {
  if (typeof v !== 'object' || v === null) return false
  const row = v as Record<string, unknown>
  return typeof row.guest === 'boolean' && isCounts(row.limits) && isCounts(row.used) && typeof row.resets_at === 'string'
}

/**
 * Extra headers the presigned PUT was signed with, such as x-amz-tagging for guest
 * uploads. Only x-amz-* names are forwarded; anything else makes the ticket invalid.
 * Undefined means the ticket had none.
 */
function uploadHeaders(v: unknown): Record<string, string> | null {
  if (v === undefined) return {}
  if (typeof v !== 'object' || v === null || Array.isArray(v)) return null
  const out: Record<string, string> = {}
  for (const [name, value] of Object.entries(v)) {
    if (!/^x-amz-[a-z0-9-]+$/i.test(name) || typeof value !== 'string') return null
    out[name] = value
  }
  return out
}

/**
 * Fetches a same-origin API path. CloudFront reaches the API through origin access
 * control, which signs every origin request with SigV4: that overwrites Authorization,
 * so the session travels in X-Scholia-Token, and a Lambda origin needs the viewer to
 * send the SHA-256 of any request body in x-amz-content-sha256.
 */
async function apiFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const headers: Record<string, string> = { ...(init.headers as Record<string, string> | undefined) }
  const token = sessionToken()
  if (token !== '') headers['X-Scholia-Token'] = token
  const method = (init.method ?? 'GET').toUpperCase()
  if (method !== 'GET' && method !== 'HEAD') {
    headers['x-amz-content-sha256'] = await sha256Hex(typeof init.body === 'string' ? init.body : '')
  }
  return fetch(path, { ...init, headers })
}

export type SourceStatus = 'queued' | 'processing' | 'ready' | 'failed'

export type Source = {
  id: string
  name: string
  content_type: string
  status: SourceStatus
  failure_reason?: string
  /** youtube_id is the lecture recording a transcript came from. */
  youtube_id?: string
}

export type TimeLocator = { kind: 'time'; start_ms: number; end_ms: number }
export type SlideLocator = { kind: 'slide'; slide: number }
export type PageLocator = { kind: 'page'; page: number; bbox?: { x0: number; y0: number; x1: number; y1: number } }
export type TextLocator = { kind: 'text'; start: number; end: number }
export type Locator = TimeLocator | SlideLocator | PageLocator | TextLocator

export type Citation = {
  chunk_id: string
  source_id: string
  locators: Locator[]
  section?: string
  excerpt?: string
}


export type WebSource = { title: string; url: string }

export type RefusalReason = 'no_material' | 'off_topic' | 'unsafe'

/** reason and web are absent on the older per-course answer routes. */
export type AnswerEvent =
  | { type: 'delta'; text: string }
  | { type: 'citation'; citations: Citation[]; web?: WebSource[] }
  | { type: 'refusal'; text: string; reason?: RefusalReason }
  | { type: 'error'; message: string }

export type UploadTicket = {
  source_id: string
  status: string
  method: string
  upload_url: string
  key: string
  headers?: Record<string, string>
}

async function readError(res: Response, fallback: string): Promise<ApiError> {
  try {
    const body: unknown = await res.json()
    if (typeof body === 'object' && body !== null && 'error' in body) {
      const err = (body as { error?: { message?: unknown; code?: unknown } }).error
      if (err && typeof err.message === 'string' && err.message !== '') {
        return new ApiError(res.status, err.message, typeof err.code === 'string' ? err.code : '')
      }
    }
  } catch {
    // The body was not JSON. The status is still the useful part.
  }
  return new ApiError(res.status, fallback)
}

export async function listCourses(signal?: AbortSignal): Promise<Course[]> {
  const res = await apiFetch('/api/courses', { signal, headers: { Accept: 'application/json' } })
  if (!res.ok) throw await readError(res, 'could not list courses')
  const body: unknown = await res.json()
  if (!isCourseList(body)) throw new ApiError(res.status, 'unexpected course list')
  return body.courses
}

export async function createCourse(title: string, opts?: { public?: boolean }): Promise<Course> {
  const payload: { title: string; public?: boolean } = { title }
  if (opts?.public) payload.public = true
  const res = await apiFetch('/api/courses', {
    method: 'POST',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
  if (!res.ok) throw await readError(res, 'could not create course')
  const body: unknown = await res.json()
  if (!isCourse(body)) throw new ApiError(res.status, 'unexpected course')
  return body
}

export async function listSources(courseID: string, signal?: AbortSignal): Promise<Source[]> {
  const res = await apiFetch(`/api/courses/${encodeURIComponent(courseID)}/sources`, {
    signal,
    headers: { Accept: 'application/json' },
  })
  if (!res.ok) throw await readError(res, 'could not list files')
  const body: unknown = await res.json()
  if (!isSourceList(body)) throw new ApiError(res.status, 'unexpected source list')
  return body.sources
}

export async function uploadSource(courseID: string, file: File): Promise<void> {
  const contentType = file.type || contentTypeFor(file.name)
  const res = await apiFetch(`/api/courses/${encodeURIComponent(courseID)}/sources`, {
    method: 'POST',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    body: JSON.stringify({ name: file.name, content_type: contentType, byte_size: file.size }),
  })
  if (!res.ok) throw await readError(res, 'could not start upload')
  const body: unknown = await res.json()
  if (!isUploadTicket(body)) throw new ApiError(res.status, 'unexpected upload response')
  const put = await fetch(body.upload_url, {
    method: 'PUT',
    headers: { ...body.headers, 'Content-Type': contentType },
    body: file,
  })
  if (!put.ok) throw new ApiError(put.status, 'upload failed')
}

const extensionTypes: Record<string, string> = {
  pdf: 'application/pdf',
  tex: 'application/x-tex',
  bib: 'application/x-bibtex',
  pptx: 'application/vnd.openxmlformats-officedocument.presentationml.presentation',
  json: 'application/json',
  vtt: 'text/vtt',
  srt: 'application/x-subrip',
  txt: 'text/plain',
  md: 'text/markdown',
  png: 'image/png',
  jpg: 'image/jpeg',
  jpeg: 'image/jpeg',
  gif: 'image/gif',
  webp: 'image/webp',
}

export async function sha256Hex(body: string): Promise<string> {
  const bytes = new TextEncoder().encode(body)
  // subtle is missing on an http origin that is not localhost. Compose uses
  // http://web, so the same digest is computed here when the browser has no Web Crypto.
  const digest = crypto.subtle ? new Uint8Array(await crypto.subtle.digest('SHA-256', bytes)) : sha256(bytes)
  return toHex(digest)
}

function toHex(bytes: Uint8Array): string {
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('')
}

const sha256K = new Uint32Array([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5, 0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3,
  0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174, 0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967, 0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
  0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85, 0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3, 0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
  0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
])

function rotr(x: number, n: number): number {
  return ((x >>> n) | (x << (32 - n))) >>> 0
}

export function sha256(bytes: Uint8Array): Uint8Array {
  const bitLen = bytes.length * 8
  const padded = new Uint8Array(((bytes.length + 9 + 63) >> 6) << 6)
  padded.set(bytes)
  padded[bytes.length] = 0x80
  const view = new DataView(padded.buffer)
  view.setUint32(padded.length - 4, bitLen, false)

  let h0 = 0x6a09e667
  let h1 = 0xbb67ae85
  let h2 = 0x3c6ef372
  let h3 = 0xa54ff53a
  let h4 = 0x510e527f
  let h5 = 0x9b05688c
  let h6 = 0x1f83d9ab
  let h7 = 0x5be0cd19
  const w = new Uint32Array(64)
  for (let offset = 0; offset < padded.length; offset += 64) {
    for (let t = 0; t < 16; t++) w[t] = view.getUint32(offset + t * 4, false)
    for (let t = 16; t < 64; t++) {
      const s0 = rotr(w[t - 15]!, 7) ^ rotr(w[t - 15]!, 18) ^ (w[t - 15]! >>> 3)
      const s1 = rotr(w[t - 2]!, 17) ^ rotr(w[t - 2]!, 19) ^ (w[t - 2]! >>> 10)
      w[t] = (w[t - 16]! + s0 + w[t - 7]! + s1) >>> 0
    }
    let a = h0
    let b = h1
    let c = h2
    let d = h3
    let e = h4
    let f = h5
    let g = h6
    let h = h7
    for (let t = 0; t < 64; t++) {
      const s1 = rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25)
      const ch = (e & f) ^ (~e & g)
      const temp1 = (h + s1 + ch + sha256K[t]! + w[t]!) >>> 0
      const s0 = rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22)
      const maj = (a & b) ^ (a & c) ^ (b & c)
      const temp2 = (s0 + maj) >>> 0
      h = g
      g = f
      f = e
      e = (d + temp1) >>> 0
      d = c
      c = b
      b = a
      a = (temp1 + temp2) >>> 0
    }
    h0 = (h0 + a) >>> 0
    h1 = (h1 + b) >>> 0
    h2 = (h2 + c) >>> 0
    h3 = (h3 + d) >>> 0
    h4 = (h4 + e) >>> 0
    h5 = (h5 + f) >>> 0
    h6 = (h6 + g) >>> 0
    h7 = (h7 + h) >>> 0
  }
  const out = new Uint8Array(32)
  const outView = new DataView(out.buffer)
  ;[h0, h1, h2, h3, h4, h5, h6, h7].forEach((word, i) => outView.setUint32(i * 4, word, false))
  return out
}

export async function startSession(email: string): Promise<string> {
  const res = await apiFetch('/api/session', {
    method: 'POST',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    body: JSON.stringify({ email }),
  })
  if (!res.ok) throw await readError(res, 'could not send a code')
  const body: unknown = await res.json()
  if (!isSessionStart(body)) throw new ApiError(res.status, 'unexpected session response')
  return body.session
}

export async function confirmSession(email: string, session: string, code: string): Promise<void> {
  const res = await apiFetch('/api/session/confirm', {
    method: 'POST',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, session, code }),
  })
  if (!res.ok) throw await readError(res, 'code was not accepted')
  const body: unknown = await res.json()
  if (!isSessionToken(body)) throw new ApiError(res.status, 'unexpected session response')
  setSessionToken(body.token)
  sessionStorage.setItem(emailKey, email)
}

function isSessionStart(v: unknown): v is { session: string } {
  return typeof v === 'object' && v !== null && typeof (v as { session?: unknown }).session === 'string'
}

function isSessionToken(v: unknown): v is { token: string } {
  return typeof v === 'object' && v !== null && typeof (v as { token?: unknown }).token === 'string' && (v as { token: string }).token !== ''
}

async function streamQuestion(
  path: string,
  payload: Record<string, string | string[]>,
  onEvent: (event: AnswerEvent) => void,
  signal?: AbortSignal,
): Promise<void> {
  const body = JSON.stringify(payload)
  const res = await apiFetch(path, {
    method: 'POST',
    signal,
    headers: {
      Accept: 'text/event-stream',
      'Content-Type': 'application/json',
    },
    body,
  })
  if (!res.ok) throw await readError(res, 'could not ask')
  if (!res.body) throw new ApiError(res.status, 'answer had no stream')
  await readSSE(res.body, onEvent)
}

export type Provider = 'openai' | 'bedrock'

export const providers: Provider[] = ['openai', 'bedrock']

export type Model = { id: string; provider: Provider; label: string; kind?: 'chat' | 'embedding' }

/** Project-wide fallbacks the server uses when a user sets no key or model. */
export type ModelDefaults = { chat_model: string; embed_model: string; web_search: boolean; server_models: boolean }

export type ModelCatalog = { models: Model[]; defaults: ModelDefaults | null }

export type ChatMode = 'answer' | 'explain'

export type Chat = {
  id: string
  course_id: string
  title: string
  model: string
  created_at: string
  updated_at: string
}

export type ChatAttachment = { id: string; name: string; content_type: string }

export type ChatMessage = {
  id: string
  role: 'user' | 'assistant'
  text: string
  mode: ChatMode
  citations?: Citation[]
  refusal?: string
  refusal_reason?: RefusalReason
  attachments?: ChatAttachment[]
  web?: WebSource[]
  created_at: string
}

export type ProviderStatus = { provider: Provider; connected: boolean }

export type Settings = { default_model: string; providers: ProviderStatus[] }

function jsonHeaders(): Record<string, string> {
  return { Accept: 'application/json', 'Content-Type': 'application/json' }
}

export async function listModels(signal?: AbortSignal): Promise<ModelCatalog> {
  const res = await apiFetch('/api/models', { signal, headers: { Accept: 'application/json' } })
  if (!res.ok) throw await readError(res, 'could not list models')
  const body: unknown = await res.json()
  const row = body as { models?: unknown; defaults?: unknown } | null
  const models = row?.models
  if (!Array.isArray(models) || !models.every(isModel)) throw new ApiError(res.status, 'unexpected model list')
  // An older server sends no defaults. A malformed block is a contract break, not a missing feature.
  if (row?.defaults !== undefined && !isModelDefaults(row.defaults)) throw new ApiError(res.status, 'unexpected model list')
  return { models, defaults: row?.defaults ?? null }
}

function isModelDefaults(v: unknown): v is ModelDefaults {
  if (typeof v !== 'object' || v === null) return false
  const row = v as Record<string, unknown>
  return (
    typeof row.chat_model === 'string' &&
    typeof row.embed_model === 'string' &&
    typeof row.web_search === 'boolean' &&
    typeof row.server_models === 'boolean'
  )
}

export async function listChats(signal?: AbortSignal): Promise<Chat[]> {
  const res = await apiFetch('/api/chats', { signal, headers: { Accept: 'application/json' } })
  if (!res.ok) throw await readError(res, 'could not list chats')
  const body: unknown = await res.json()
  const chats = (body as { chats?: unknown } | null)?.chats
  if (!Array.isArray(chats) || !chats.every(isChat)) throw new ApiError(res.status, 'unexpected chat list')
  return chats
}

export async function createChat(courseID: string, model: string): Promise<Chat> {
  const res = await apiFetch('/api/chats', {
    method: 'POST',
    headers: jsonHeaders(),
    body: JSON.stringify({ course_id: courseID, model }),
  })
  if (!res.ok) throw await readError(res, 'could not start a chat')
  const body: unknown = await res.json()
  if (!isChat(body)) throw new ApiError(res.status, 'unexpected chat')
  return body
}

export async function getChat(chatID: string, signal?: AbortSignal): Promise<{ chat: Chat; messages: ChatMessage[] }> {
  const res = await apiFetch(`/api/chats/${encodeURIComponent(chatID)}`, { signal, headers: { Accept: 'application/json' } })
  if (!res.ok) throw await readError(res, 'could not open the chat')
  const body: unknown = await res.json()
  const row = body as { chat?: unknown; messages?: unknown } | null
  if (!row || !isChat(row.chat) || !Array.isArray(row.messages) || !row.messages.every(isChatMessage)) {
    throw new ApiError(res.status, 'unexpected chat')
  }
  return { chat: row.chat, messages: row.messages }
}

export async function renameChat(chatID: string, title: string): Promise<Chat> {
  const res = await apiFetch(`/api/chats/${encodeURIComponent(chatID)}`, {
    method: 'PATCH',
    headers: jsonHeaders(),
    body: JSON.stringify({ title }),
  })
  if (!res.ok) throw await readError(res, 'could not rename the chat')
  const body: unknown = await res.json()
  if (!isChat(body)) throw new ApiError(res.status, 'unexpected chat')
  return body
}

export async function deleteChat(chatID: string): Promise<void> {
  const res = await apiFetch(`/api/chats/${encodeURIComponent(chatID)}`, { method: 'DELETE', headers: { Accept: 'application/json' } })
  if (!res.ok) throw await readError(res, 'could not delete the chat')
}

export function sendChatMessage(
  chatID: string,
  question: string,
  mode: ChatMode,
  onEvent: (event: AnswerEvent) => void,
  signal?: AbortSignal,
  attachmentIDs: string[] = [],
): Promise<void> {
  const payload: Record<string, string | string[]> = { question, mode }
  if (attachmentIDs.length > 0) payload.attachment_ids = attachmentIDs
  return streamQuestion(`/api/chats/${encodeURIComponent(chatID)}/messages`, payload, onEvent, signal)
}

/** Types and size a chat attachment may have. The server checks the same list. */
export const chatAttachmentTypes: Record<string, string> = {
  pdf: 'application/pdf',
  md: 'text/markdown',
  txt: 'text/plain',
  tex: 'application/x-tex',
  bib: 'application/x-bibtex',
  png: 'image/png',
  jpg: 'image/jpeg',
  jpeg: 'image/jpeg',
  webp: 'image/webp',
}

export const maxChatAttachmentBytes = 10 * 1024 * 1024

export const maxChatAttachments = 5

/** Returns why a file cannot be attached, or null when it can. */
export function chatAttachmentProblem(file: File): string | null {
  const ext = file.name.includes('.') ? file.name.slice(file.name.lastIndexOf('.') + 1).toLowerCase() : ''
  if (!(ext in chatAttachmentTypes)) return `${file.name}: only PDF, Markdown, text, LaTeX, BibTeX, PNG, JPEG, and WebP files can be attached.`
  if (file.size < 1) return `${file.name} is empty.`
  if (file.size > maxChatAttachmentBytes) return `${file.name} is larger than 10 MB.`
  return null
}

export async function uploadChatAttachment(chatID: string, file: File): Promise<ChatAttachment> {
  const problem = chatAttachmentProblem(file)
  if (problem) throw new Error(problem)
  const ext = file.name.slice(file.name.lastIndexOf('.') + 1).toLowerCase()
  const contentType = chatAttachmentTypes[ext]!
  const res = await apiFetch(`/api/chats/${encodeURIComponent(chatID)}/attachments`, {
    method: 'POST',
    headers: jsonHeaders(),
    body: JSON.stringify({ name: file.name, content_type: contentType, byte_size: file.size }),
  })
  if (!res.ok) throw await readError(res, 'could not attach the file')
  const body: unknown = await res.json()
  const row = body as { attachment?: unknown; upload_url?: unknown; headers?: unknown } | null
  const extra = uploadHeaders(row?.headers)
  if (!row || !isChatAttachment(row.attachment) || typeof row.upload_url !== 'string' || !extra) throw new ApiError(res.status, 'unexpected attachment')
  const put = await fetch(row.upload_url, { method: 'PUT', headers: { ...extra, 'Content-Type': contentType }, body: file })
  if (!put.ok) throw new ApiError(put.status, 'upload failed')
  return row.attachment
}

export async function deleteChatAttachment(chatID: string, attachmentID: string): Promise<void> {
  const res = await apiFetch(`/api/chats/${encodeURIComponent(chatID)}/attachments/${encodeURIComponent(attachmentID)}`, {
    method: 'DELETE',
    headers: { Accept: 'application/json' },
  })
  if (!res.ok) throw await readError(res, 'could not remove the file')
}

function isChatAttachment(v: unknown): v is ChatAttachment {
  if (typeof v !== 'object' || v === null) return false
  const row = v as Record<string, unknown>
  return typeof row.id === 'string' && typeof row.name === 'string' && typeof row.content_type === 'string'
}

function isWebSource(v: unknown): v is WebSource {
  if (typeof v !== 'object' || v === null) return false
  const row = v as Record<string, unknown>
  return typeof row.title === 'string' && typeof row.url === 'string' && /^https?:\/\//i.test(row.url)
}

function isRefusalReason(v: unknown): v is RefusalReason {
  return v === 'no_material' || v === 'off_topic' || v === 'unsafe'
}

export async function getSettings(signal?: AbortSignal): Promise<Settings> {
  const res = await apiFetch('/api/account/settings', { signal, headers: { Accept: 'application/json' } })
  if (!res.ok) throw await readError(res, 'could not read settings')
  const body: unknown = await res.json()
  if (!isSettings(body)) throw new ApiError(res.status, 'unexpected settings')
  return body
}

export async function putSettings(defaultModel: string): Promise<Settings> {
  const res = await apiFetch('/api/account/settings', {
    method: 'PUT',
    headers: jsonHeaders(),
    body: JSON.stringify({ default_model: defaultModel }),
  })
  if (!res.ok) throw await readError(res, 'could not save settings')
  const body: unknown = await res.json()
  if (!isSettings(body)) throw new ApiError(res.status, 'unexpected settings')
  return body
}

export async function putProviderKey(provider: Provider, apiKey: string): Promise<void> {
  const res = await apiFetch(`/api/account/keys/${encodeURIComponent(provider)}`, {
    method: 'PUT',
    headers: jsonHeaders(),
    body: JSON.stringify({ api_key: apiKey }),
  })
  if (!res.ok) throw await readError(res, 'could not store the key')
}

export async function deleteProviderKey(provider: Provider): Promise<void> {
  const res = await apiFetch(`/api/account/keys/${encodeURIComponent(provider)}`, {
    method: 'DELETE',
    headers: { Accept: 'application/json' },
  })
  if (!res.ok) throw await readError(res, 'could not remove the key')
}

function isProvider(v: unknown): v is Provider {
  return v === 'openai' || v === 'bedrock'
}

function isModel(v: unknown): v is Model {
  if (typeof v !== 'object' || v === null) return false
  const row = v as Record<string, unknown>
  if (row.kind !== undefined && row.kind !== 'chat' && row.kind !== 'embedding') return false
  return typeof row.id === 'string' && isProvider(row.provider) && typeof row.label === 'string'
}

function isChat(v: unknown): v is Chat {
  if (typeof v !== 'object' || v === null) return false
  const row = v as Record<string, unknown>
  return ['id', 'course_id', 'title', 'model', 'created_at', 'updated_at'].every((k) => typeof row[k] === 'string')
}

function isChatMessage(v: unknown): v is ChatMessage {
  if (typeof v !== 'object' || v === null) return false
  const row = v as Record<string, unknown>
  if (typeof row.id !== 'string' || typeof row.text !== 'string' || typeof row.created_at !== 'string') return false
  if (row.role !== 'user' && row.role !== 'assistant') return false
  if (row.mode !== 'answer' && row.mode !== 'explain') return false
  if (row.refusal !== undefined && typeof row.refusal !== 'string') return false
  if (row.refusal_reason !== undefined && !isRefusalReason(row.refusal_reason)) return false
  if (row.attachments !== undefined && !(Array.isArray(row.attachments) && row.attachments.every(isChatAttachment))) return false
  if (row.web !== undefined && !(Array.isArray(row.web) && row.web.every(isWebSource))) return false
  return row.citations === undefined || (Array.isArray(row.citations) && row.citations.every(isCitation))
}

function isSettings(v: unknown): v is Settings {
  if (typeof v !== 'object' || v === null) return false
  const row = v as { default_model?: unknown; providers?: unknown }
  if (typeof row.default_model !== 'string' || !Array.isArray(row.providers)) return false
  return row.providers.every((p) => {
    if (typeof p !== 'object' || p === null) return false
    const item = p as { provider?: unknown; connected?: unknown }
    return isProvider(item.provider) && typeof item.connected === 'boolean'
  })
}

export async function readSSE(stream: ReadableStream<Uint8Array>, onEvent: (event: AnswerEvent) => void): Promise<void> {
  const reader = stream.getReader()
  const decoder = new TextDecoder()
  let buf = ''
  for (;;) {
    const { done, value } = await reader.read()
    if (done) return
    buf += decoder.decode(value, { stream: true })
    let split = buf.indexOf('\n\n')
    while (split >= 0) {
      const event = parseEvent(buf.slice(0, split))
      if (event) onEvent(event)
      buf = buf.slice(split + 2)
      split = buf.indexOf('\n\n')
    }
  }
}

function parseEvent(block: string): AnswerEvent | null {
  let name = ''
  const data: string[] = []
  for (const line of block.split('\n')) {
    if (line.startsWith('event:')) name = line.slice(6).trim()
    else if (line.startsWith('data:')) data.push(line.slice(5).trim())
  }
  if (name === '' || data.length === 0) return null
  let parsed: unknown
  try {
    parsed = JSON.parse(data.join('\n'))
  } catch {
    return null
  }
  if (typeof parsed !== 'object' || parsed === null) return null
  const row = parsed as Record<string, unknown>
  if (name === 'delta' && typeof row.text === 'string') return { type: 'delta', text: row.text }
  if (name === 'refusal' && typeof row.text === 'string') {
    return isRefusalReason(row.reason) ? { type: 'refusal', text: row.text, reason: row.reason } : { type: 'refusal', text: row.text }
  }
  if (name === 'error') {
    const message = typeof row.message === 'string' ? row.message : 'answer failed'
    return { type: 'error', message }
  }
  if (name === 'citation' && Array.isArray(row.citations)) {
    const web = Array.isArray(row.web) ? row.web.filter(isWebSource) : []
    return { type: 'citation', citations: row.citations.filter(isCitation), web }
  }
  return null
}

function isCitation(v: unknown): v is Citation {
  if (typeof v !== 'object' || v === null) return false
  const row = v as Record<string, unknown>
  return (
    typeof row.chunk_id === 'string' &&
    typeof row.source_id === 'string' &&
    Array.isArray(row.locators) &&
    row.locators.every(isLocator) &&
    (row.section === undefined || typeof row.section === 'string') &&
    (row.excerpt === undefined || typeof row.excerpt === 'string')
  )
}

function isLocator(v: unknown): v is Locator {
  if (typeof v !== 'object' || v === null) return false
  const row = v as Record<string, unknown>
  if (row.kind === 'time') return typeof row.start_ms === 'number' && typeof row.end_ms === 'number'
  if (row.kind === 'slide') return typeof row.slide === 'number'
  if (row.kind === 'page') return typeof row.page === 'number'
  if (row.kind === 'text') return typeof row.start === 'number' && typeof row.end === 'number'
  return false
}

export function contentTypeFor(name: string): string {
  const ext = name.slice(name.lastIndexOf('.') + 1).toLowerCase()
  return extensionTypes[ext] ?? 'application/octet-stream'
}

function isCourse(v: unknown): v is Course {
  if (typeof v !== 'object' || v === null) return false
  const c = v as Record<string, unknown>
  return typeof c.id === 'string' && typeof c.title === 'string'
}

function isCourseList(v: unknown): v is { courses: Course[] } {
  if (typeof v !== 'object' || v === null) return false
  const courses = (v as { courses?: unknown }).courses
  return Array.isArray(courses) && courses.every(isCourse)
}

function isSource(v: unknown): v is Source {
  if (typeof v !== 'object' || v === null) return false
  const s = v as Record<string, unknown>
  return (
    typeof s.id === 'string' &&
    typeof s.name === 'string' &&
    typeof s.content_type === 'string' &&
    (s.status === 'queued' || s.status === 'processing' || s.status === 'ready' || s.status === 'failed') &&
    (s.youtube_id === undefined || typeof s.youtube_id === 'string')
  )
}

function isSourceList(v: unknown): v is { sources: Source[] } {
  if (typeof v !== 'object' || v === null) return false
  const sources = (v as { sources?: unknown }).sources
  return Array.isArray(sources) && sources.every(isSource)
}

function isUploadTicket(v: unknown): v is UploadTicket {
  if (typeof v !== 'object' || v === null) return false
  const t = v as Record<string, unknown>
  if (typeof t.source_id !== 'string' || typeof t.upload_url !== 'string' || typeof t.method !== 'string') return false
  const extra = uploadHeaders(t.headers)
  if (!extra) return false
  t.headers = extra
  return true
}

function isHealth(v: unknown): v is Health {
  if (typeof v !== 'object' || v === null) return false
  const h = v as Record<string, unknown>
  return ['status', 'version', 'commit', 'env', 'time'].every((k) => typeof h[k] === 'string')
}
