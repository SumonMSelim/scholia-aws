import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { chat, courses, goTo, pick, renderWithData, settings, catalog } from '@/test/harness'
import { matchRoute, useLocation } from '@/lib/router'
import { ChatPage } from './ChatPage'

vi.mock('@/lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/api')>()),
  listCourses: vi.fn(),
  listChats: vi.fn(),
  listModels: vi.fn(),
  getSettings: vi.fn(),
  createChat: vi.fn(),
  getChat: vi.fn(),
  listSources: vi.fn(),
  sendChatMessage: vi.fn(),
  uploadChatAttachment: vi.fn(),
  deleteChatAttachment: vi.fn(),
}))

import { ApiError, createChat, getChat, getSettings, listChats, listCourses, listSources, deleteChatAttachment, listModels, sendChatMessage, uploadChatAttachment } from '@/lib/api'

function Routed() {
  const { path } = useLocation()
  const route = matchRoute(path)
  return <ChatPage chatId={route.name === 'chat' ? route.chatId : null} />
}

describe('ChatPage', () => {
  beforeEach(() => {
    goTo('/chat')
    vi.mocked(listCourses).mockResolvedValue(courses)
    vi.mocked(listChats).mockResolvedValue([])
    vi.mocked(listModels).mockResolvedValue(catalog)
    vi.mocked(getSettings).mockResolvedValue(settings)
    vi.mocked(listSources).mockResolvedValue([{ id: 's', name: 'cells.pdf', content_type: 'application/pdf', status: 'ready' }])
  })
  afterEach(() => goTo('/'))

  it('starts a chat with the chosen course and model, streams, and refreshes the list', async () => {
    const created = chat('h1', 'c2', '')
    vi.mocked(createChat).mockResolvedValue(created)
    vi.mocked(sendChatMessage).mockImplementation(async (_id, _q, _mode, onEvent) => {
      onEvent({ type: 'delta', text: 'Cells divide ' })
      onEvent({ type: 'delta', text: 'by mitosis.' })
      onEvent({
        type: 'citation',
        citations: [{ chunk_id: 'k', source_id: 's', locators: [{ kind: 'slide', slide: 3 }], section: 'Cells › Mitosis', excerpt: 'A cell splits in two.' }],
      })
    })
    renderWithData(<Routed />)
    expect(await screen.findByRole('heading', { name: 'Study with your course' })).toBeInTheDocument()
    await waitFor(() => expect(screen.getByLabelText('Course')).toHaveTextContent('Algebra'))
    expect(screen.getByLabelText('Model')).toHaveTextContent('GPT-4.1 mini')
    expect(screen.queryByRole('radiogroup', { name: 'Response style' })).not.toBeInTheDocument()

    await pick('Course', 'Biology')
    expect(screen.getByLabelText('Model')).toHaveTextContent('GPT-4.1 mini')
    await pick('Model', 'Nova Lite')
    await userEvent.type(screen.getByLabelText('Message'), 'How do cells divide?{Enter}')

    expect(await screen.findByText('Cells divide by mitosis.')).toBeInTheDocument()
    expect(screen.getByText('How do cells divide?')).toBeInTheDocument()
    expect(createChat).toHaveBeenCalledWith('c2', 'nova-lite')
    expect(sendChatMessage).toHaveBeenCalledWith('h1', 'How do cells divide?', 'answer', expect.any(Function), expect.any(AbortSignal), [])
    expect(window.location.pathname).toBe('/chat/h1')
    expect(getChat).not.toHaveBeenCalled()
    await waitFor(() => expect(listChats).toHaveBeenCalledTimes(3))
    expect(screen.getByLabelText('Course: Biology')).toBeInTheDocument()
    expect(screen.getByLabelText('Model: Nova Lite')).toBeInTheDocument()

    const source = within(screen.getByRole('region', { name: 'Sources' })).getByRole('button', { name: /cells\.pdf.*Mitosis.*slide 3/ })
    expect(source).toHaveAttribute('aria-expanded', 'false')
    await userEvent.click(source)
    expect(screen.getByRole('region', { name: 'Source 1' })).toHaveTextContent('A cell splits in two.')
    await userEvent.click(source)
    expect(screen.queryByRole('region', { name: 'Source 1' })).not.toBeInTheDocument()
  })

  it('uses the default model and a course from the link', async () => {
    goTo('/chat?course=c2')
    vi.mocked(getSettings).mockResolvedValue({ default_model: 'nova-lite', providers: [{ provider: 'openai', connected: true }] })
    renderWithData(<Routed />)
    await waitFor(() => expect(screen.getByLabelText('Course')).toHaveTextContent('Biology'))
    // Only the connected provider's models are offered, so the default does not apply.
    expect(screen.getByLabelText('Model')).toHaveTextContent('GPT-4.1 mini')
    expect(screen.queryByRole('option', { name: 'Nova Lite' })).not.toBeInTheDocument()
  })

  it('opens an existing chat with its course and model locked', async () => {
    goTo('/chat/h7')
    vi.mocked(getChat).mockResolvedValue({
      chat: chat('h7', 'c1', 'Matrices'),
      messages: [
        { id: 'm1', role: 'user', text: 'What is a matrix?', mode: 'answer', created_at: 't' },
        { id: 'm2', role: 'assistant', text: 'A grid of numbers.', mode: 'answer', refusal: 'Not in the slides.', created_at: 't' },
      ],
    })
    renderWithData(<Routed />)
    expect(await screen.findByText('A grid of numbers.')).toBeInTheDocument()
    expect(screen.getByText('Not in the slides.')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Matrices' })).toBeInTheDocument()
    expect(await screen.findByLabelText('Course: Algebra')).toBeInTheDocument()
    expect(screen.queryByLabelText('Model')).not.toBeInTheDocument()
  })

  it('shows a load error', async () => {
    goTo('/chat/missing')
    vi.mocked(getChat).mockRejectedValue(new Error('chat not found'))
    renderWithData(<Routed />)
    expect(await screen.findByRole('alert')).toHaveTextContent('chat not found')
  })

  it('stops a stream without an error and shows a stream error', async () => {
    vi.mocked(createChat).mockResolvedValue(chat('h2', 'c1'))
    vi.mocked(sendChatMessage).mockImplementationOnce(
      (_id, _q, _mode, onEvent, signal) =>
        new Promise((_resolve, reject) => {
          onEvent({ type: 'delta', text: 'Partial' })
          signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')))
        }),
    )
    renderWithData(<Routed />)
    await waitFor(() => expect(screen.getByLabelText('Course')).toHaveTextContent('Algebra'))
    await userEvent.type(screen.getByLabelText('Message'), 'Long question{Enter}')
    await userEvent.click(await screen.findByRole('button', { name: 'Stop' }))
    expect(await screen.findByRole('button', { name: 'Send' })).toBeInTheDocument()
    expect(screen.getByText('Partial')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()

    vi.mocked(sendChatMessage).mockImplementationOnce(async (_id, _q, _mode, onEvent) => onEvent({ type: 'error', message: 'model failed' }))
    await userEvent.type(screen.getByLabelText('Message'), 'Again{Enter}')
    expect(await screen.findByRole('alert')).toHaveTextContent('model failed')
    expect(createChat).toHaveBeenCalledTimes(1)
  })

  it('points to Knowledge when there are no courses', async () => {
    vi.mocked(listCourses).mockResolvedValue([])
    renderWithData(<Routed />)
    expect(await screen.findByRole('link', { name: 'Create a course in Knowledge' })).toHaveAttribute('href', '/knowledge')
    expect(screen.getByLabelText('Message')).toBeDisabled()
  })
  it('fills the box from a starter', async () => {
    renderWithData(<Routed />)
    await waitFor(() => expect(screen.getByLabelText('Course')).toHaveTextContent('Algebra'))
    await userEvent.click(screen.getByRole('button', { name: /Take a mock exam/ }))
    expect(screen.getByLabelText('Message')).toHaveValue('Give me a mock exam on this course: 5 questions, one at a time, then grade my answers.')
    expect(screen.getByLabelText('Message')).toHaveFocus()
  })

  it('creates the chat on first attach, uploads to it, and sends the ready files', async () => {
    vi.mocked(createChat).mockResolvedValue(chat('h3', 'c1', ''))
    vi.mocked(uploadChatAttachment)
      .mockResolvedValueOnce({ id: 'a1', name: 'draft.pdf', content_type: 'application/pdf' })
      .mockResolvedValueOnce({ id: 'a2', name: 'notes.md', content_type: 'text/markdown' })
    vi.mocked(deleteChatAttachment).mockResolvedValue()
    vi.mocked(sendChatMessage).mockImplementation(async (_id, _q, _mode, onEvent) => onEvent({ type: 'delta', text: 'Looks good.' }))
    renderWithData(<Routed />)
    await waitFor(() => expect(screen.getByLabelText('Course')).toHaveTextContent('Algebra'))
    const draft = new File(['%PDF'], 'draft.pdf', { type: 'application/pdf' })
    const notes = new File(['# n'], 'notes.md', { type: 'text/markdown' })
    const essay = new File(['x'], 'essay.docx')
    await userEvent.upload(screen.getByLabelText('Attach files'), [draft, notes, essay], { applyAccept: false })

    const list = await screen.findByRole('list', { name: 'Attached files' })
    await waitFor(() => expect(list).toHaveTextContent('notes.mdReady'))
    expect(list).toHaveTextContent('draft.pdfReady')
    expect(list).toHaveTextContent('essay.docxFailed')
    expect(screen.getByRole('alert')).toHaveTextContent('only PDF, Markdown, text, LaTeX, BibTeX, PNG, JPEG, and WebP')
    expect(createChat).toHaveBeenCalledTimes(1)
    expect(createChat).toHaveBeenCalledWith('c1', 'gpt-4.1-mini')
    expect(uploadChatAttachment).toHaveBeenCalledWith('h3', draft)
    expect(uploadChatAttachment).toHaveBeenCalledWith('h3', notes)
    expect(window.location.pathname).toBe('/chat/h3')
    // The course and model lock once the chat exists.
    expect(screen.getByLabelText('Course: Algebra')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Remove notes.md' }))
    expect(deleteChatAttachment).toHaveBeenCalledWith('h3', 'a2')
    await userEvent.click(screen.getByRole('button', { name: 'Remove essay.docx' }))

    await userEvent.type(screen.getByLabelText('Message'), 'Review my draft{Enter}')
    expect(await screen.findByText('Looks good.')).toBeInTheDocument()
    expect(sendChatMessage).toHaveBeenCalledWith('h3', 'Review my draft', 'answer', expect.any(Function), expect.any(AbortSignal), ['a1'])
    expect(screen.queryByRole('list', { name: 'Attached files' })).not.toBeInTheDocument()
    expect(screen.getByRole('list', { name: 'Files' })).toHaveTextContent('draft.pdf')
  })

  it('holds Send while a file uploads and lets a public course attach', async () => {
    vi.mocked(listCourses).mockResolvedValue([{ id: 'p1', title: 'Open Physics', public: true, mine: false }])
    vi.mocked(createChat).mockResolvedValue(chat('h4', 'p1', ''))
    let finish: (value: { id: string; name: string; content_type: string }) => void = () => {}
    vi.mocked(uploadChatAttachment).mockImplementation(() => new Promise((resolve) => (finish = resolve)))
    renderWithData(<Routed />)
    await waitFor(() => expect(screen.getByLabelText('Course')).toHaveTextContent('Open Physics'))
    expect(screen.getByRole('button', { name: 'Attach files to this chat' })).toBeEnabled()
    await userEvent.upload(screen.getByLabelText('Attach files'), new File(['a'], 'a.txt', { type: 'text/plain' }))
    await userEvent.type(screen.getByLabelText('Message'), 'Question')
    expect(await screen.findByText('Uploading…')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Send' })).toBeDisabled()
    finish({ id: 'a9', name: 'a.txt', content_type: 'text/plain' })
    await waitFor(() => expect(screen.getByRole('button', { name: 'Send' })).toBeEnabled())
  })

  it('drops streamed text when a guardrail refusal arrives', async () => {
    vi.mocked(createChat).mockResolvedValue(chat('h1', 'c1', ''))
    vi.mocked(sendChatMessage).mockImplementation(async (_id, _q, _mode, onEvent) => {
      onEvent({ type: 'delta', text: 'Here is how to bui' })
      onEvent({ type: 'refusal', text: "I can't share that response.", reason: 'unsafe' })
    })
    renderWithData(<Routed />)
    await waitFor(() => expect(screen.getByLabelText('Course')).toHaveTextContent('Algebra'))
    await userEvent.type(screen.getByLabelText('Message'), 'something{Enter}')
    expect(await screen.findByText("I can't share that response.")).toBeInTheDocument()
    expect(screen.queryByText('Here is how to bui')).not.toBeInTheDocument()
  })

  it('renders refusals by reason and web sources as links', async () => {
    goTo('/chat/h5')
    vi.mocked(getChat).mockResolvedValue({
      chat: chat('h5', 'c1'),
      messages: [
        { id: 'u1', role: 'user', text: 'Write me a poem', mode: 'answer', attachments: [], web: [], created_at: 't' },
        { id: 'r1', role: 'assistant', text: '', mode: 'answer', refusal: 'I only help with Algebra.', refusal_reason: 'off_topic', created_at: 't' },
        { id: 'r2', role: 'assistant', text: '', mode: 'answer', refusal: "I won't help with that.", refusal_reason: 'unsafe', created_at: 't' },
        { id: 'r3', role: 'assistant', text: '', mode: 'answer', refusal: 'Nothing in this course covers that.', refusal_reason: 'no_material', created_at: 't' },
        {
          id: 'w1',
          role: 'assistant',
          text: 'Eigenvalues scale eigenvectors.',
          mode: 'answer',
          web: [
            { title: 'Eigenvalues and eigenvectors', url: 'https://www.example.org/eigen' },
            { title: 'Crafted', url: 'javascript:alert(1)' },
          ],
          created_at: 't',
        },
      ],
    })
    renderWithData(<Routed />)
    const notes = await screen.findAllByRole('note', { name: 'Refusal' })
    expect(notes.map((node) => node.dataset.reason)).toEqual(['off_topic', 'unsafe', 'no_material'])
    expect(notes[0]).toHaveTextContent('This chat only covers its course.')
    expect(notes[1]).toHaveTextContent("I won't help with that.")
    expect(notes[1]).not.toHaveTextContent('Knowledge')
    expect(within(notes[2]!).getByRole('link', { name: 'Add files in Knowledge' })).toHaveAttribute('href', '/knowledge/c1')

    const [link] = within(screen.getByRole('region', { name: 'Sources' })).getAllByRole('link')
    expect(link).toHaveAttribute('href', 'https://www.example.org/eigen')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
    expect(link).toHaveTextContent('Eigenvalues and eigenvectorsexample.org')
    // A source URL that is not http(s) renders as plain text, never as a link.
    const sources = screen.getByRole('region', { name: 'Sources' })
    expect(within(sources).getAllByRole('link')).toHaveLength(1)
    expect(within(sources).getByText('Crafted').closest('a')).toBeNull()
  })

  it('shows web sources and the refusal reason from the stream', async () => {
    vi.mocked(createChat).mockResolvedValue(chat('h6', 'c1'))
    vi.mocked(sendChatMessage)
      .mockImplementationOnce(async (_id, _q, _mode, onEvent) => {
        onEvent({ type: 'delta', text: 'Answer.' })
        onEvent({ type: 'citation', citations: [], web: [{ title: 'Source', url: 'https://example.com/a' }] })
      })
      .mockImplementationOnce(async (_id, _q, _mode, onEvent) => onEvent({ type: 'refusal', text: 'Only Algebra here.', reason: 'off_topic' }))
    renderWithData(<Routed />)
    await waitFor(() => expect(screen.getByLabelText('Course')).toHaveTextContent('Algebra'))
    await userEvent.type(screen.getByLabelText('Message'), 'Q1{Enter}')
    expect(await screen.findByRole('link', { name: /Source/ })).toHaveAttribute('href', 'https://example.com/a')
    await userEvent.type(screen.getByLabelText('Message'), 'Q2{Enter}')
    expect(await screen.findByRole('note', { name: 'Refusal' })).toHaveAttribute('data-reason', 'off_topic')
  })

  it('shows a quota notice when the daily limit is reached', async () => {
    vi.mocked(createChat).mockResolvedValue(chat('h1', 'c1', ''))
    vi.mocked(sendChatMessage).mockRejectedValueOnce(new ApiError(429, 'You used all 40 messages today.', 'quota'))
    renderWithData(<Routed />)
    await waitFor(() => expect(screen.getByLabelText('Course')).toHaveTextContent('Algebra'))
    await userEvent.type(screen.getByLabelText('Message'), 'What is a group?{Enter}')
    const notice = await screen.findByRole('alert', { name: 'Daily limit reached' })
    expect(notice).toHaveTextContent('You used all 40 messages today. Resets at 00:00 UTC.')
  })

  it('shows a paused notice when the service is off', async () => {
    vi.mocked(createChat).mockRejectedValueOnce(new ApiError(503, 'paused', 'paused'))
    renderWithData(<Routed />)
    await waitFor(() => expect(screen.getByLabelText('Course')).toHaveTextContent('Algebra'))
    await userEvent.type(screen.getByLabelText('Message'), 'What is a group?{Enter}')
    expect(await screen.findByRole('alert', { name: 'Service paused' })).toHaveTextContent('Scholia is paused right now.')
  })
})
