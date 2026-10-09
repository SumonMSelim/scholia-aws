import { describe, expect, it, vi } from 'vitest'
import {
  ApiError,
  chatAttachmentProblem,
  confirmSession,
  contentTypeFor,
  createChat,
  createCourse,
  deleteChat,
  deleteChatAttachment,
  deleteProviderKey,
  errorText,
  fetchHealth,
  getChat,
  getSettings,
  getUsage,
  guestExpiresAt,
  isGuest,
  limitCode,
  listChats,
  listCourses,
  listModels,
  listSources,
  putProviderKey,
  putSettings,
  readSSE,
  renameChat,
  sendChatMessage,
  sessionEmail,
  sessionToken,
  setSessionToken,
  sha256,
  sha256Hex,
  startGuest,
  startSession,
  uploadChatAttachment,
  uploadSource,
} from './api'

const health = { status: 'ok', version: '1.0.0', commit: 'abc', env: 'local', time: '2026-09-27T06:00:00Z' }

function mockFetch(response: Response) {
  return vi.spyOn(globalThis, 'fetch').mockResolvedValue(response)
}

describe('fetchHealth', () => {
  it('returns the parsed health response', async () => {
    const spy = mockFetch(Response.json(health))
    await expect(fetchHealth()).resolves.toEqual(health)
    expect(spy).toHaveBeenCalledWith('/api/health', expect.objectContaining({ headers: { Accept: 'application/json' } }))
  })

  it('throws ApiError with the status on HTTP errors', async () => {
    mockFetch(new Response('down', { status: 503 }))
    await expect(fetchHealth()).rejects.toMatchObject({ name: 'ApiError', status: 503 })
  })

  it.each([null, 'ok', { status: 'ok' }, { ...health, version: 1 }])('rejects malformed body %j', async (body) => {
    mockFetch(Response.json(body))
    await expect(fetchHealth()).rejects.toBeInstanceOf(ApiError)
  })
})

describe('courses api', () => {
  it('lists and creates courses', async () => {
    const spy = mockFetch(Response.json({ courses: [{ id: 'c1', title: 'Networks' }] }))
    await expect(listCourses()).resolves.toEqual([{ id: 'c1', title: 'Networks' }])
    spy.mockResolvedValueOnce(Response.json({ sources: [{ id: 's1', name: 'notes.txt', content_type: 'text/plain', status: 'ready' }] }))
    await expect(listSources('c1')).resolves.toEqual([{ id: 's1', name: 'notes.txt', content_type: 'text/plain', status: 'ready' }])
    spy.mockResolvedValueOnce(Response.json({ id: 'c2', title: 'Signals' }))
    await expect(createCourse('Signals')).resolves.toEqual({ id: 'c2', title: 'Signals' })
  })

  it('maps a text file extension', () => {
    expect(contentTypeFor('notes.txt')).toBe('text/plain')
    expect(contentTypeFor('main.tex')).toBe('application/x-tex')
    expect(contentTypeFor('refs.bib')).toBe('application/x-bibtex')
    expect(contentTypeFor('NOTES.TXT')).toBe('text/plain')
    expect(contentTypeFor('blob')).toBe('application/octet-stream')
  })

  it('surfaces the server message', async () => {
    mockFetch(Response.json({ error: { code: 'invalid', message: 'title is required' } }, { status: 400 }))
    await expect(createCourse('')).rejects.toMatchObject({ status: 400, message: 'title is required' })
  })

  it('uploads with a presigned PUT and no payload hash header', async () => {
    const file = new File(['hello notes\n'], 'notes.txt', { type: 'text/plain' })
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      if (url.includes('/sources') && init?.method === 'POST') {
        return Response.json({ source_id: 's1', status: 'queued', method: 'PUT', upload_url: 'http://uploads/put', key: 'k' })
      }
      return new Response(null, { status: 200 })
    })
    await uploadSource('c1', file)
    const put = fetchSpy.mock.calls[1]
    expect(String(put?.[0])).toBe('http://uploads/put')
    const headers = new Headers((put?.[1] as RequestInit | undefined)?.headers)
    expect(headers.get('x-amz-content-sha256')).toBeNull()
    expect(headers.get('Content-Type')).toBe('text/plain')
  })

  it('rejects a malformed course list', async () => {
    mockFetch(Response.json({ courses: [{ id: 1 }] }))
    await expect(listCourses()).rejects.toBeInstanceOf(ApiError)
  })

  it('uses the fallback when the error body is not JSON', async () => {
    mockFetch(new Response('nope', { status: 502 }))
    await expect(listCourses()).rejects.toMatchObject({ status: 502, message: 'could not list courses' })
  })

  it('guesses the content type when the file has none', async () => {
    const file = new File(['hello notes\n'], 'notes.txt')
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      if (url.includes('/sources') && init?.method === 'POST') {
        return Response.json({ source_id: 's1', status: 'queued', method: 'PUT', upload_url: 'http://uploads/put', key: 'k' })
      }
      return new Response(null, { status: 200 })
    })
    await uploadSource('c1', file)
  })
})

function streamOf(chunks: string[]): ReadableStream<Uint8Array> {
  return new ReadableStream({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(new TextEncoder().encode(chunk))
      controller.close()
    },
  })
}

describe('answer stream', () => {
  it('hashes the empty string with SHA-256', async () => {
    await expect(sha256Hex('')).resolves.toBe('e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855')
    const abc = sha256(new TextEncoder().encode('abc'))
    expect(Array.from(abc, (byte) => byte.toString(16).padStart(2, '0')).join('')).toBe(
      'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad',
    )
    const subtle = crypto.subtle
    Object.defineProperty(crypto, 'subtle', { value: undefined, configurable: true })
    try {
      await expect(sha256Hex('abc')).resolves.toBe('ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad')
    } finally {
      Object.defineProperty(crypto, 'subtle', { value: subtle, configurable: true })
    }
  })

  it('sends the body hash and emits deltas before citations', async () => {
    const events: string[] = []
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(async (_input, init) => {
      const body = String(init?.body)
      const hash = await sha256Hex(body)
      const headers = new Headers(init?.headers)
      expect(headers.get('x-amz-content-sha256')).toBe(hash)
      expect(hash).toHaveLength(64)
      return new Response(
        streamOf([
          'event: delta\ndata: {"text":"Hel',
          'lo"}\n\n',
          'event: citation\ndata: {"citations":[{"chunk_id":"p1","source_id":"s1","locators":[{"kind":"time","start_ms":1500,"end_ms":3000},{"kind":"slide","slide":4},{"kind":"page","page":2},{"kind":"text","start":0,"end":12}]},{"chunk_id":1},{"chunk_id":"p2","source_id":"s2","locators":[{"kind":"nope"}]},{"chunk_id":"p3","source_id":"s3"}]}\n\n',
          'event: note\ndata: {"text":"skip"}\n\n',
          'event: delta\ndata: not-json\n\n',
          'event: error\ndata: {}\n\n',
        ]),
        { status: 200, headers: { 'Content-Type': 'text/event-stream' } },
      )
    })
    await sendChatMessage('h 1', 'hop', 'answer', (event) => events.push(event.type === 'citation' ? `cite:${event.citations.length}` : `${event.type}:${'text' in event ? event.text : event.message}`))
    expect(String(fetchSpy.mock.calls[0]?.[0])).toBe('/api/chats/h%201/messages')
    expect(events).toEqual(['delta:Hello', 'cite:1', 'error:answer failed'])
  })

  it('reads a refusal and a named error', async () => {
    const events: string[] = []
    await readSSE(
      streamOf([
        'event: refusal\ndata: {"text":"nothing"}\n\n',
        'event: error\ndata: {"message":"answer failed"}\n\n',
        ': comment\n\n',
        'event: delta\n\n',
        'event: delta\ndata: {"text":1}\n\n',
        'event: refusal\ndata: {"text":1}\n\n',
      ]),
      (event) => events.push(event.type),
    )
    expect(events).toEqual(['refusal', 'error'])
  })

  it('uses the server message when the ask fails', async () => {
    mockFetch(Response.json({ error: { message: 'question is required' } }, { status: 400 }))
    await expect(sendChatMessage('h1', '', 'answer', () => {})).rejects.toMatchObject({ status: 400, message: 'question is required' })
  })

  it('stores a session and sends it on a public create', async () => {
    sessionStorage.clear()
    const spy = mockFetch(Response.json({ session: 'sess' }))
    await expect(startSession('ada@example.com')).resolves.toBe('sess')
    spy.mockResolvedValueOnce(Response.json({ token: 'tok' }))
    await confirmSession('ada@example.com', 'sess', '123456')
    expect(sessionToken()).toBe('tok')
    expect(sessionEmail()).toBe('ada@example.com')
    spy.mockResolvedValueOnce(Response.json({ id: 'c2', title: 'Signals', public: true }))
    await createCourse('Signals', { public: true })
    const headers = new Headers(spy.mock.calls.at(-1)?.[1]?.headers)
    expect(headers.get('X-Scholia-Token')).toBe('tok')
    expect(headers.get('Authorization')).toBeNull()
    const body = JSON.parse(String(spy.mock.calls.at(-1)?.[1]?.body))
    expect(body).toEqual({ title: 'Signals', public: true })
    setSessionToken('')
    expect(sessionToken()).toBe('')
    expect(sessionEmail()).toBe('')
  })

  it('rejects a session response without a token', async () => {
    mockFetch(Response.json({ session: 1 }))
    await expect(startSession('ada@example.com')).rejects.toBeInstanceOf(ApiError)
    mockFetch(Response.json({ token: '' }))
    await expect(confirmSession('ada@example.com', 'sess', '123456')).rejects.toBeInstanceOf(ApiError)
  })

  it('rejects a response that has no stream', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true, status: 200, body: null } as Response)
    await expect(sendChatMessage('h1', 'hop', 'answer', () => {})).rejects.toMatchObject({ message: 'answer had no stream' })
  })
})

const chat = { id: 'h1', course_id: 'c1', title: 'Routing', model: 'gpt-4.1-mini', created_at: '2026-09-30T10:00:00Z', updated_at: '2026-09-30T10:05:00Z' }
const settings = { default_model: 'gpt-4.1-mini', providers: [{ provider: 'openai', connected: true }, { provider: 'bedrock', connected: false }] }

describe('models api', () => {
  it('lists models and rejects a bad shape', async () => {
    const spy = mockFetch(Response.json({ models: [{ id: 'gpt-4.1-mini', provider: 'openai', label: 'GPT-4.1 mini' }] }))
    await expect(listModels()).resolves.toEqual({ models: [{ id: 'gpt-4.1-mini', provider: 'openai', label: 'GPT-4.1 mini' }], defaults: null })
    expect(String(spy.mock.calls[0]?.[0])).toBe('/api/models')
    spy.mockResolvedValueOnce(Response.json({ models: [{ id: 'x', provider: 'gemini', label: 'X' }] }))
    await expect(listModels()).rejects.toMatchObject({ message: 'unexpected model list' })
    spy.mockResolvedValueOnce(Response.json(null))
    await expect(listModels()).rejects.toBeInstanceOf(ApiError)
    spy.mockResolvedValueOnce(new Response('', { status: 500 }))
    await expect(listModels()).rejects.toMatchObject({ status: 500 })
  })

  it('reads the server defaults and model kinds', async () => {
    const defaults = { chat_model: 'nova', embed_model: 'titan', web_search: false, server_models: true }
    const models = [
      { id: 'nova', provider: 'bedrock', label: 'Nova', kind: 'chat' },
      { id: 'titan', provider: 'bedrock', label: 'Titan', kind: 'embedding' },
    ]
    const spy = mockFetch(Response.json({ models, defaults }))
    await expect(listModels()).resolves.toEqual({ models, defaults })
    spy.mockResolvedValueOnce(Response.json({ models, defaults: { chat_model: 'nova' } }))
    await expect(listModels()).rejects.toMatchObject({ message: 'unexpected model list' })
    spy.mockResolvedValueOnce(Response.json({ models: [{ id: 'x', provider: 'bedrock', label: 'X', kind: 'rerank' }] }))
    await expect(listModels()).rejects.toMatchObject({ message: 'unexpected model list' })
  })
})

describe('chats api', () => {
  it('lists, creates, opens, renames, and deletes chats with the session', async () => {
    sessionStorage.setItem('scholia.session', 'tok')
    const spy = mockFetch(Response.json({ chats: [chat] }))
    await expect(listChats()).resolves.toEqual([chat])
    expect(new Headers(spy.mock.calls[0]?.[1]?.headers).get('X-Scholia-Token')).toBe('tok')

    spy.mockResolvedValueOnce(Response.json(chat, { status: 201 }))
    await expect(createChat('c1', 'gpt-4.1-mini')).resolves.toEqual(chat)
    expect(JSON.parse(String(spy.mock.calls[1]?.[1]?.body))).toEqual({ course_id: 'c1', model: 'gpt-4.1-mini' })

    const messages = [
      { id: 'm1', role: 'user', text: 'hop?', mode: 'answer', created_at: 't' },
      { id: 'm2', role: 'assistant', text: 'next hop', mode: 'answer', refusal: 'no', citations: [{ chunk_id: 'k', source_id: 's', locators: [{ kind: 'page', page: 2 }] }], created_at: 't' },
    ]
    spy.mockResolvedValueOnce(Response.json({ chat, messages }))
    await expect(getChat('h 1')).resolves.toEqual({ chat, messages })
    expect(String(spy.mock.calls[2]?.[0])).toBe('/api/chats/h%201')

    spy.mockResolvedValueOnce(Response.json({ ...chat, title: 'Hops' }))
    await expect(renameChat('h1', 'Hops')).resolves.toMatchObject({ title: 'Hops' })
    expect(spy.mock.calls[3]?.[1]?.method).toBe('PATCH')

    spy.mockResolvedValueOnce(new Response(null, { status: 204 }))
    await deleteChat('h1')
    expect(spy.mock.calls[4]?.[1]?.method).toBe('DELETE')
    setSessionToken('')
  })

  it.each([
    ['list', () => listChats(), { chats: [{ id: 'h1' }] }],
    ['create', () => createChat('c1', ''), { id: 'h1' }],
    ['rename', () => renameChat('h1', 't'), { title: 't' }],
    ['open without messages', () => getChat('h1'), { chat }],
    ['open with a bad message', () => getChat('h1'), { chat, messages: [{ id: 'm', role: 'system', text: '', mode: 'answer', created_at: 't' }] }],
    ['open with a bad mode', () => getChat('h1'), { chat, messages: [{ id: 'm', role: 'user', text: '', mode: 'x', created_at: 't' }] }],
    ['open with a bad refusal', () => getChat('h1'), { chat, messages: [{ id: 'm', role: 'user', text: '', mode: 'answer', refusal: 1, created_at: 't' }] }],
    ['open with bad citations', () => getChat('h1'), { chat, messages: [{ id: 'm', role: 'user', text: '', mode: 'answer', citations: {}, created_at: 't' }] }],
    ['open with a missing id', () => getChat('h1'), { chat, messages: [{ role: 'user', text: '', mode: 'answer', created_at: 't' }] }],
    ['open with bad attachments', () => getChat('h1'), { chat, messages: [{ id: 'm', role: 'user', text: '', mode: 'answer', attachments: [{ id: 1 }], created_at: 't' }] }],
    ['open with a non-http web link', () => getChat('h1'), { chat, messages: [{ id: 'm', role: 'assistant', text: '', mode: 'answer', web: [{ title: 'x', url: 'javascript:alert(1)' }], created_at: 't' }] }],
    ['open with a bad refusal reason', () => getChat('h1'), { chat, messages: [{ id: 'm', role: 'assistant', text: '', mode: 'answer', refusal: 'no', refusal_reason: 'rude', created_at: 't' }] }],
    ['open null', () => getChat('h1'), null],
  ])('rejects a malformed %s response', async (_name, call, body) => {
    mockFetch(Response.json(body))
    await expect(call()).rejects.toBeInstanceOf(ApiError)
  })

  it.each([
    ['list', () => listChats(), 'could not list chats'],
    ['create', () => createChat('c1', 'm'), 'could not start a chat'],
    ['open', () => getChat('h1'), 'could not open the chat'],
    ['rename', () => renameChat('h1', 't'), 'could not rename the chat'],
    ['delete', () => deleteChat('h1'), 'could not delete the chat'],
    ['attach', () => uploadChatAttachment('h1', new File(['a'], 'a.md')), 'could not attach the file'],
    ['remove attachment', () => deleteChatAttachment('h1', 'a1'), 'could not remove the file'],
    ['settings', () => getSettings(), 'could not read settings'],
    ['save settings', () => putSettings('m'), 'could not save settings'],
    ['store key', () => putProviderKey('openai', 'k'), 'could not store the key'],
    ['remove key', () => deleteProviderKey('openai'), 'could not remove the key'],
  ])('uses a fallback when %s fails', async (_name, call, text) => {
    mockFetch(new Response('', { status: 500 }))
    await expect(call()).rejects.toMatchObject({ status: 500, message: text })
  })

  it('streams a chat message with the mode and a body hash', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockImplementation(async (_input, init) => {
      expect(new Headers(init?.headers).get('x-amz-content-sha256')).toBe(await sha256Hex(String(init?.body)))
      return new Response(streamOf(['event: delta\ndata: {"text":"hi"}\n\n']), { status: 200 })
    })
    const events: string[] = []
    await sendChatMessage('h1', 'hop?', 'explain', (event) => events.push(event.type))
    expect(String(spy.mock.calls[0]?.[0])).toBe('/api/chats/h1/messages')
    expect(JSON.parse(String(spy.mock.calls[0]?.[1]?.body))).toEqual({ question: 'hop?', mode: 'explain' })
    expect(events).toEqual(['delta'])
    await sendChatMessage('h1', 'with files', 'answer', () => {}, undefined, ['a1', 'a2'])
    expect(JSON.parse(String(spy.mock.calls[1]?.[1]?.body))).toEqual({ question: 'with files', mode: 'answer', attachment_ids: ['a1', 'a2'] })
  })

  it('accepts messages that carry attachments, web sources, and a refusal reason', async () => {
    const messages = [
      { id: 'u', role: 'user', text: 'Review', mode: 'answer', attachments: [{ id: 'a1', name: 'd.pdf', content_type: 'application/pdf' }], web: [], created_at: 't' },
      { id: 'a', role: 'assistant', text: '', mode: 'answer', refusal: 'Only Algebra.', refusal_reason: 'off_topic', web: [{ title: 'W', url: 'https://w.org' }], created_at: 't' },
    ]
    mockFetch(Response.json({ chat, messages }))
    await expect(getChat('h1')).resolves.toEqual({ chat, messages })
  })

  it('reads the refusal reason and web sources from the stream, tolerating older events', async () => {
    const events: unknown[] = []
    await readSSE(
      streamOf([
        'event: refusal\ndata: {"text":"off","reason":"off_topic"}\n\n',
        'event: refusal\ndata: {"text":"old"}\n\n',
        'event: refusal\ndata: {"text":"odd","reason":"rude"}\n\n',
        'event: citation\ndata: {"citations":[],"web":[{"title":"A","url":"https://a.org"},{"title":"B","url":"ftp://b"},{"url":"https://c.org"}]}\n\n',
        'event: citation\ndata: {"citations":[]}\n\n',
      ]),
      (event) => events.push(event),
    )
    expect(events).toEqual([
      { type: 'refusal', text: 'off', reason: 'off_topic' },
      { type: 'refusal', text: 'old' },
      { type: 'refusal', text: 'odd' },
      { type: 'citation', citations: [], web: [{ title: 'A', url: 'https://a.org' }] },
      { type: 'citation', citations: [], web: [] },
    ])
  })
})

describe('chat attachments api', () => {
  it('validates the type, size, and emptiness of a file', () => {
    expect(chatAttachmentProblem(new File(['x'], 'notes.TEX'))).toBeNull()
    expect(chatAttachmentProblem(new File(['x'], 'essay.docx'))).toMatch(/only PDF, Markdown/)
    expect(chatAttachmentProblem(new File(['x'], 'noext'))).toMatch(/only PDF/)
    expect(chatAttachmentProblem(new File([], 'empty.md'))).toBe('empty.md is empty.')
    const big = new File(['x'], 'big.pdf')
    Object.defineProperty(big, 'size', { value: 10 * 1024 * 1024 + 1 })
    expect(chatAttachmentProblem(big)).toBe('big.pdf is larger than 10 MB.')
  })

  it('creates an attachment, PUTs the file, and deletes it', async () => {
    const attachment = { id: 'a1', name: 'draft.pdf', content_type: 'application/pdf', byte_size: 4 }
    const spy = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(Response.json({ attachment, upload_url: 'https://bucket/put' }, { status: 201 }))
      .mockResolvedValueOnce(new Response(null, { status: 200 }))
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
    const file = new File(['%PDF'], 'draft.pdf')
    await expect(uploadChatAttachment('h 1', file)).resolves.toEqual(attachment)
    expect(String(spy.mock.calls[0]?.[0])).toBe('/api/chats/h%201/attachments')
    expect(JSON.parse(String(spy.mock.calls[0]?.[1]?.body))).toEqual({ name: 'draft.pdf', content_type: 'application/pdf', byte_size: 4 })
    expect(String(spy.mock.calls[1]?.[0])).toBe('https://bucket/put')
    expect(spy.mock.calls[1]?.[1]?.method).toBe('PUT')
    expect(new Headers(spy.mock.calls[1]?.[1]?.headers).get('Content-Type')).toBe('application/pdf')
    await deleteChatAttachment('h 1', 'a/1')
    expect(String(spy.mock.calls[2]?.[0])).toBe('/api/chats/h%201/attachments/a%2F1')
    expect(spy.mock.calls[2]?.[1]?.method).toBe('DELETE')
  })

  it('rejects a bad file before any request, a malformed ticket, and a failed PUT', async () => {
    const spy = mockFetch(Response.json({ attachment: { id: 'a1' }, upload_url: 'u' }))
    await expect(uploadChatAttachment('h1', new File(['x'], 'essay.docx'))).rejects.toThrow(/only PDF/)
    expect(spy).not.toHaveBeenCalled()
    await expect(uploadChatAttachment('h1', new File(['x'], 'a.md'))).rejects.toMatchObject({ message: 'unexpected attachment' })
    spy.mockReset()
    spy
      .mockResolvedValueOnce(Response.json({ attachment: { id: 'a1', name: 'a.md', content_type: 'text/markdown' }, upload_url: 'u' }))
      .mockResolvedValueOnce(new Response('', { status: 403 }))
    await expect(uploadChatAttachment('h1', new File(['x'], 'a.md'))).rejects.toMatchObject({ status: 403, message: 'upload failed' })
  })
})

describe('settings api', () => {
  it('reads and saves settings and provider keys', async () => {
    const spy = mockFetch(Response.json(settings))
    await expect(getSettings()).resolves.toEqual(settings)
    spy.mockResolvedValueOnce(Response.json({ ...settings, default_model: 'nova' }))
    await expect(putSettings('nova')).resolves.toMatchObject({ default_model: 'nova' })
    expect(JSON.parse(String(spy.mock.calls[1]?.[1]?.body))).toEqual({ default_model: 'nova' })

    const secret = 'sk-live-secret'
    spy.mockResolvedValueOnce(new Response(null, { status: 204 }))
    await putProviderKey('openai', secret)
    expect(String(spy.mock.calls[2]?.[0])).toBe('/api/account/keys/openai')
    expect(spy.mock.calls[2]?.[1]?.method).toBe('PUT')
    expect(JSON.parse(String(spy.mock.calls[2]?.[1]?.body))).toEqual({ api_key: secret })

    spy.mockResolvedValueOnce(new Response(null, { status: 204 }))
    await deleteProviderKey('bedrock')
    expect(String(spy.mock.calls[3]?.[0])).toBe('/api/account/keys/bedrock')
    expect(spy.mock.calls[3]?.[1]?.method).toBe('DELETE')
  })

  it.each([
    { providers: [] },
    { default_model: 1, providers: [] },
    { default_model: '', providers: [null] },
    { default_model: '', providers: [{ provider: 'gemini', connected: true }] },
    { default_model: '', providers: [{ provider: 'openai', connected: 'yes' }] },
    null,
  ])('rejects settings %j', async (body) => {
    const spy = mockFetch(Response.json(body))
    await expect(getSettings()).rejects.toBeInstanceOf(ApiError)
    spy.mockResolvedValueOnce(Response.json(body))
    await expect(putSettings('')).rejects.toBeInstanceOf(ApiError)
  })
})

describe('guest demo api', () => {
  it('starts a guest session and forgets it on sign-out', async () => {
    const spy = mockFetch(Response.json({ token: 'g-tok', guest: true, expires_at: '2026-10-03T00:00:00Z' }, { status: 201 }))
    await startGuest()
    expect(String(spy.mock.calls[0]?.[0])).toBe('/api/session/guest')
    expect(spy.mock.calls[0]?.[1]?.method).toBe('POST')
    // SHA-256 of the empty body, required by CloudFront OAC on a POST.
    expect(new Headers(spy.mock.calls[0]?.[1]?.headers).get('x-amz-content-sha256')).toBe('e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855')
    expect(sessionToken()).toBe('g-tok')
    expect(guestExpiresAt()).toBe('2026-10-03T00:00:00Z')
    expect(isGuest()).toBe(true)
    setSessionToken('')
    expect(isGuest()).toBe(false)
  })

  it('rejects a malformed guest session and keeps the error code', async () => {
    mockFetch(Response.json({ token: 'g', guest: false, expires_at: 'x' }, { status: 201 }))
    await expect(startGuest()).rejects.toMatchObject({ message: 'unexpected guest session' })
    expect(sessionToken()).toBe('')
    vi.mocked(globalThis.fetch).mockResolvedValue(Response.json({ error: { code: 'paused', message: 'demo paused' } }, { status: 503 }))
    const err = await startGuest().catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err).toMatchObject({ status: 503, code: 'paused', message: 'demo paused' })
  })

  it('reads usage and rejects a malformed body', async () => {
    const usage = { guest: true, limits: { messages: 40, uploads: 10, web: 5 }, used: { messages: 3, uploads: 0, web: 1 }, resets_at: '2026-10-02T00:00:00Z' }
    setSessionToken('tok')
    const spy = mockFetch(Response.json(usage))
    await expect(getUsage()).resolves.toEqual(usage)
    expect(String(spy.mock.calls[0]?.[0])).toBe('/api/account/usage')
    expect(new Headers(spy.mock.calls[0]?.[1]?.headers).get('X-Scholia-Token')).toBe('tok')
    spy.mockResolvedValue(Response.json({ ...usage, used: { messages: '3' } }))
    await expect(getUsage()).rejects.toMatchObject({ message: 'unexpected usage' })
    spy.mockResolvedValue(Response.json({ error: { code: 'quota', message: 'x' } }, { status: 429 }))
    await expect(getUsage()).rejects.toMatchObject({ code: 'quota' })
    setSessionToken('')
  })

  it('names quota and paused errors and leaves others alone', () => {
    const quota = new ApiError(429, 'You used all 40 messages today.', 'quota')
    expect(limitCode(quota)).toBe('quota')
    expect(errorText(quota, 'x')).toBe('You used all 40 messages today. Resets at 00:00 UTC.')
    const paused = new ApiError(503, 'paused', 'paused')
    expect(limitCode(paused)).toBe('paused')
    expect(errorText(paused, 'x')).toBe('Scholia is paused right now. Please try again later.')
    expect(limitCode(new ApiError(400, 'bad', 'invalid'))).toBeNull()
    expect(limitCode(new Error('plain'))).toBeNull()
    expect(errorText(new Error('plain'), 'x')).toBe('plain')
    expect(errorText('nope', 'fallback')).toBe('fallback')
    expect(new ApiError(500, 'm').code).toBe('')
  })

  it('forwards the x-amz upload headers a ticket was signed with', async () => {
    const attachment = { id: 'a1', name: 'a.md', content_type: 'text/markdown', byte_size: 1 }
    const spy = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(Response.json({ attachment, upload_url: 'https://bucket/put', headers: { 'x-amz-tagging': 'guest=true' } }, { status: 201 }))
      .mockResolvedValueOnce(new Response(null, { status: 200 }))
      .mockResolvedValueOnce(Response.json({ source_id: 's1', status: 'queued', method: 'PUT', upload_url: 'https://bucket/src', key: 'k', headers: { 'x-amz-tagging': 'guest=true' } }))
      .mockResolvedValueOnce(new Response(null, { status: 200 }))
    await uploadChatAttachment('h1', new File(['x'], 'a.md'))
    let headers = new Headers(spy.mock.calls[1]?.[1]?.headers)
    expect(headers.get('x-amz-tagging')).toBe('guest=true')
    expect(headers.get('Content-Type')).toBe('text/markdown')
    await uploadSource('c1', new File(['x'], 'notes.md', { type: 'text/markdown' }))
    headers = new Headers(spy.mock.calls[3]?.[1]?.headers)
    expect(headers.get('x-amz-tagging')).toBe('guest=true')
    expect(headers.get('Content-Type')).toBe('text/markdown')
  })

  it('rejects upload headers that are not x-amz strings', async () => {
    const attachment = { id: 'a1', name: 'a.md', content_type: 'text/markdown', byte_size: 1 }
    const spy = mockFetch(Response.json({ attachment, upload_url: 'u', headers: { Authorization: 'Bearer x' } }, { status: 201 }))
    await expect(uploadChatAttachment('h1', new File(['x'], 'a.md'))).rejects.toMatchObject({ message: 'unexpected attachment' })
    spy.mockResolvedValue(Response.json({ attachment, upload_url: 'u', headers: { 'x-amz-tagging': 1 } }, { status: 201 }))
    await expect(uploadChatAttachment('h1', new File(['x'], 'a.md'))).rejects.toMatchObject({ message: 'unexpected attachment' })
    spy.mockResolvedValue(Response.json({ attachment, upload_url: 'u', headers: ['x'] }, { status: 201 }))
    await expect(uploadChatAttachment('h1', new File(['x'], 'a.md'))).rejects.toMatchObject({ message: 'unexpected attachment' })
    spy.mockResolvedValue(Response.json({ source_id: 's', status: 'q', method: 'PUT', upload_url: 'u', key: 'k', headers: { Host: 'evil' } }))
    await expect(uploadSource('c1', new File(['x'], 'n.md'))).rejects.toMatchObject({ message: 'unexpected upload response' })
  })
})

describe('api transport through CloudFront OAC', () => {
  const init = (spy: ReturnType<typeof vi.spyOn>, i: number) => (spy.mock.calls[i]?.[1] ?? {}) as RequestInit

  it('hashes every body, sends the session in its own header, and never sends Authorization', async () => {
    setSessionToken('tok')
    const spy = mockFetch(Response.json({ id: 'c9', title: 'Networks' }))
    await createCourse('Networks')
    const post = init(spy, 0)
    const postHeaders = new Headers(post.headers)
    expect(postHeaders.get('x-amz-content-sha256')).toBe(await sha256Hex(String(post.body)))
    expect(postHeaders.get('X-Scholia-Token')).toBe('tok')
    expect(postHeaders.get('Authorization')).toBeNull()

    spy.mockResolvedValueOnce(Response.json({ default_model: 'm', providers: [] }))
    await putSettings('m')
    const put = init(spy, 1)
    expect(new Headers(put.headers).get('x-amz-content-sha256')).toBe(await sha256Hex(String(put.body)))

    spy.mockResolvedValueOnce(new Response(null, { status: 204 }))
    await deleteChat('h1')
    const del = new Headers(init(spy, 2).headers)
    expect(del.get('x-amz-content-sha256')).toBe('e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855')
    expect(del.get('X-Scholia-Token')).toBe('tok')

    spy.mockResolvedValueOnce(Response.json({ courses: [] }))
    await listCourses()
    const get = new Headers(init(spy, 3).headers)
    expect(get.get('x-amz-content-sha256')).toBeNull()
    expect(get.get('X-Scholia-Token')).toBe('tok')

    for (const call of spy.mock.calls) expect(new Headers((call[1] as RequestInit | undefined)?.headers).get('Authorization')).toBeNull()
    setSessionToken('')
  })

  it('sends no session header without a session', async () => {
    setSessionToken('')
    const spy = mockFetch(Response.json({ courses: [] }))
    await listCourses()
    expect(new Headers(init(spy, 0).headers).get('X-Scholia-Token')).toBeNull()
  })

  it('keeps the session off the presigned S3 PUT', async () => {
    setSessionToken('tok')
    const spy = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, req) => {
      if (String(input).includes('/sources') && req?.method === 'POST') {
        return Response.json({ source_id: 's1', status: 'queued', method: 'PUT', upload_url: 'https://uploads.example/put', key: 'k' })
      }
      return new Response(null, { status: 200 })
    })
    await uploadSource('c1', new File(['x'], 'n.md', { type: 'text/markdown' }))
    const s3 = new Headers((spy.mock.calls[1]?.[1] as RequestInit | undefined)?.headers)
    expect(String(spy.mock.calls[1]?.[0])).toBe('https://uploads.example/put')
    expect(s3.get('X-Scholia-Token')).toBeNull()
    expect(s3.get('Authorization')).toBeNull()
    setSessionToken('')
  })
})
