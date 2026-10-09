import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { chat, courses, goTo, renderWithData, settings, catalog } from '@/test/harness'
import { KnowledgePage } from './KnowledgePage'
import { CoursePage } from './CoursePage'

vi.mock('@/lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/api')>()),
  listCourses: vi.fn(),
  listChats: vi.fn(),
  listModels: vi.fn(),
  getSettings: vi.fn(),
  createCourse: vi.fn(),
  listSources: vi.fn(),
  uploadSource: vi.fn(),
}))

import { ApiError, createCourse, getSettings, listChats, listCourses, listModels, listSources, uploadSource } from '@/lib/api'

beforeEach(() => {
  goTo('/knowledge')
  vi.mocked(listCourses).mockResolvedValue([...courses, { id: 'p1', title: 'Open Physics', mine: false, public: true }])
  vi.mocked(listChats).mockResolvedValue([chat('h1', 'c1'), chat('h2', 'c1', 'Eigen')])
  vi.mocked(listModels).mockResolvedValue(catalog)
  vi.mocked(getSettings).mockResolvedValue(settings)
  vi.mocked(listSources).mockResolvedValue([])
})
afterEach(() => goTo('/'))

describe('KnowledgePage', () => {
  it('lists owned and public courses with chat counts', async () => {
    renderWithData(<KnowledgePage />)
    const algebra = await screen.findByRole('link', { name: /Algebra/ })
    expect(algebra).toHaveAttribute('href', '/knowledge/c1')
    await waitFor(() => expect(algebra).toHaveTextContent('2 chats'))
    expect(screen.getByRole('link', { name: /Biology/ })).toHaveTextContent('No chats yet')
    const shared = screen.getByRole('region', { name: 'Public courses' })
    expect(within(shared).getByText('Open Physics')).toBeInTheDocument()
  })

  it('creates a course and opens it', async () => {
    vi.mocked(createCourse).mockRejectedValueOnce(new Error('title is taken')).mockResolvedValueOnce({ id: 'c9', title: 'Networks', mine: true, public: true })
    renderWithData(<KnowledgePage />)
    await userEvent.click(await screen.findByRole('button', { name: 'New course' }))
    await userEvent.type(screen.getByLabelText('Course title'), 'Networks')
    await userEvent.click(screen.getByLabelText('Public course'))
    await userEvent.click(screen.getByRole('button', { name: 'Create course' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('title is taken')
    await userEvent.click(screen.getByRole('button', { name: 'Create course' }))
    expect(createCourse).toHaveBeenLastCalledWith('Networks', { public: true })
    await waitFor(() => expect(window.location.pathname).toBe('/knowledge/c9'))
    expect(await screen.findByRole('link', { name: /Networks/ })).toBeInTheDocument()
  })

  it('lists public courses in the order they arrive, each with a Public badge', async () => {
    vi.mocked(listCourses).mockResolvedValue([
      { id: 'p1', title: 'Open Physics', mine: false, public: true },
      { id: 'p2', title: 'Open Networks', mine: false, public: true },
    ])
    renderWithData(<KnowledgePage />)
    const shared = await screen.findByRole('region', { name: 'Public courses' })
    const links = within(shared).getAllByRole('link')
    expect(links.map((link) => link.getAttribute('href'))).toEqual(['/knowledge/p1', '/knowledge/p2'])
    for (const link of links) expect(within(link).getByText('Public')).toBeInTheDocument()
  })

  it('shows a quota error on course create', async () => {
    vi.mocked(createCourse).mockRejectedValueOnce(new ApiError(429, 'Guests can create 2 courses.', 'quota'))
    renderWithData(<KnowledgePage />)
    await userEvent.click(await screen.findByRole('button', { name: 'New course' }))
    await userEvent.type(screen.getByLabelText('Course title'), 'Networks')
    await userEvent.click(screen.getByRole('button', { name: 'Create course' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Guests can create 2 courses. Resets at 00:00 UTC.')
  })

  it('shows an empty state and a list error', async () => {
    vi.mocked(listCourses).mockRejectedValue(new Error('could not list courses'))
    renderWithData(<KnowledgePage />)
    expect(await screen.findByText('could not list courses')).toBeInTheDocument()
    expect(screen.getByText(/No courses yet/)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'New course' }))
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByLabelText('Course title')).not.toBeInTheDocument()
  })
})

describe('CoursePage', () => {
  it('uploads files with per-file status and lists the course chats', async () => {
    vi.mocked(uploadSource).mockResolvedValueOnce().mockRejectedValueOnce(new Error('content type "application/zip" is not allowed'))
    vi.mocked(listSources).mockResolvedValueOnce([]).mockResolvedValue([{ id: 's1', name: 'lecture.pdf', content_type: 'application/pdf', status: 'processing' }])
    renderWithData(<CoursePage courseId="c1" />)
    expect(await screen.findByRole('heading', { name: 'Algebra' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'New chat in this course' })).toHaveAttribute('href', '/chat?course=c1')
    expect(await screen.findByRole('link', { name: 'Eigen' })).toHaveAttribute('href', '/chat/h2')

    const pdf = new File(['%PDF-1.7'], 'lecture.pdf', { type: 'application/pdf' })
    const tex = new File(['\\section{A}'], 'main.tex', { type: 'application/x-tex' })
    await userEvent.upload(screen.getByLabelText('Upload files'), [pdf, tex])
    const uploads = screen.getByRole('list', { name: 'Uploads' })
    expect(await within(uploads).findByText('Uploaded')).toBeInTheDocument()
    expect(await within(uploads).findByText(/is not allowed/)).toBeInTheDocument()
    expect(uploadSource).toHaveBeenCalledWith('c1', pdf)
    expect(await screen.findByText('Processing')).toBeInTheDocument()
  })

  it('adds markdown notes', async () => {
    vi.mocked(uploadSource).mockResolvedValue()
    renderWithData(<CoursePage courseId="c1" />)
    await userEvent.type(await screen.findByLabelText('Markdown title'), 'Week 1')
    await userEvent.type(screen.getByLabelText('Markdown content'), '# Vectors')
    await userEvent.click(screen.getByRole('button', { name: 'Add notes' }))
    const file = vi.mocked(uploadSource).mock.calls[0]?.[1]
    expect(file?.name).toBe('Week 1.md')
  })

  it('shows a file list error', async () => {
    vi.mocked(listSources).mockRejectedValue(new Error('could not list files'))
    renderWithData(<CoursePage courseId="c1" />)
    expect(await screen.findByText('could not list files')).toBeInTheDocument()
  })

  it('is read only for a public course and handles a missing one', async () => {
    const view = renderWithData(<CoursePage courseId="p1" />)
    expect(await screen.findByText(/public course/)).toBeInTheDocument()
    expect(screen.queryByLabelText('Upload files')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Provider')).not.toBeInTheDocument()
    view.unmount()
    renderWithData(<CoursePage courseId="nope" />)
    expect(await screen.findByRole('heading', { name: 'Course not found' })).toBeInTheDocument()
  })
})
