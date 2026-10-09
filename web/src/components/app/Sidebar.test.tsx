import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { chat, courses, goTo, renderWithData, settings, catalog } from '@/test/harness'
import { Sidebar } from './Sidebar'

vi.mock('@/lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/api')>()),
  listCourses: vi.fn(),
  listChats: vi.fn(),
  listModels: vi.fn(),
  getSettings: vi.fn(),
  renameChat: vi.fn(),
  deleteChat: vi.fn(),
}))

import { deleteChat, getSettings, listChats, listCourses, listModels, renameChat } from '@/lib/api'

const health = { kind: 'loading' } as const

describe('Sidebar', () => {
  beforeEach(() => {
    goTo('/chat/h2')
    vi.mocked(listCourses).mockResolvedValue([...courses, { id: 'c3', title: 'Chemistry', mine: true }])
    vi.mocked(listChats).mockResolvedValue([chat('h3', 'c2', 'Cells'), chat('h2', 'c1', 'Vectors'), chat('h1', 'c2', 'Genes')])
    vi.mocked(listModels).mockResolvedValue(catalog)
    vi.mocked(getSettings).mockResolvedValue(settings)
  })
  afterEach(() => goTo('/'))

  it('groups chats under their course, newest course first', async () => {
    renderWithData(<Sidebar route={{ name: 'chat', chatId: 'h2' }} health={health} onNavigate={() => {}} />)
    const biology = await screen.findByRole('listitem', { name: 'Biology' })
    expect(within(biology).getAllByRole('link').map((a) => a.textContent)).toEqual(['', 'Cells', 'Genes'])
    const groups = screen.getAllByRole('button', { expanded: true }).map((b) => b.textContent)
    expect(groups).toEqual(['Biology', 'Algebra'])
    expect(screen.getByRole('link', { name: 'Vectors' })).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('link', { name: 'New chat in Biology' })).toHaveAttribute('href', '/chat?course=c2')

    const chemistry = screen.getByRole('button', { name: 'Chemistry' })
    expect(chemistry).toHaveAttribute('aria-expanded', 'false')
    await userEvent.click(chemistry)
    expect(screen.getByText('No chats yet')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Biology' }))
    expect(screen.queryByRole('link', { name: 'Cells' })).not.toBeInTheDocument()
  })

  it('renames and deletes a chat', async () => {
    vi.mocked(renameChat).mockResolvedValue(chat('h3', 'c2', 'Mitosis'))
    vi.mocked(deleteChat).mockResolvedValue()
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    renderWithData(<Sidebar route={{ name: 'chat', chatId: 'h2' }} health={health} onNavigate={() => {}} />)

    await userEvent.click(await screen.findByRole('button', { name: 'Rename Cells' }))
    const input = screen.getByLabelText('Chat title')
    await userEvent.clear(input)
    await userEvent.type(input, 'Mitosis{Enter}')
    expect(renameChat).toHaveBeenCalledWith('h3', 'Mitosis')
    expect(await screen.findByRole('link', { name: 'Mitosis' })).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Rename Genes' }))
    await userEvent.keyboard('{Escape}')
    expect(screen.queryByLabelText('Chat title')).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Delete Vectors' }))
    expect(deleteChat).toHaveBeenCalledWith('h2')
    await waitFor(() => expect(screen.queryByRole('link', { name: 'Vectors' })).not.toBeInTheDocument())
    expect(window.location.pathname).toBe('/chat')
  })

  it('keeps the rename open when it fails and skips a declined delete', async () => {
    vi.mocked(renameChat).mockRejectedValue(new Error('nope'))
    vi.spyOn(window, 'confirm').mockReturnValue(false)
    renderWithData(<Sidebar route={{ name: 'knowledge', courseId: null }} health={health} onNavigate={() => {}} />)
    await userEvent.click(await screen.findByRole('button', { name: 'Rename Cells' }))
    await userEvent.type(screen.getByLabelText('Chat title'), '!{Enter}')
    expect(screen.getByLabelText('Chat title')).toHaveAttribute('aria-invalid', 'true')
    await userEvent.click(screen.getByRole('button', { name: 'Cancel rename' }))
    await userEvent.click(screen.getByRole('button', { name: 'Delete Genes' }))
    expect(deleteChat).not.toHaveBeenCalled()
  })

  it('shows the account and signs out', async () => {
    sessionStorage.setItem('scholia.email', 'ada@example.com')
    const { onSignOut } = renderWithData(<Sidebar route={{ name: 'settings' }} health={health} onNavigate={() => {}} />)
    expect(screen.getByText('ada@example.com')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Settings' })).toHaveAttribute('aria-current', 'page')
    await userEvent.click(screen.getByRole('button', { name: 'Sign out' }))
    expect(onSignOut).toHaveBeenCalled()
    sessionStorage.clear()
  })

  it('shows the deployed version under the account', () => {
    const ok = { kind: 'ok', health: { status: 'ok', version: 'v1.0.2', commit: '51be261', env: 'prod', time: 't' } } as const
    renderWithData(<Sidebar route={{ name: 'settings' }} health={ok} onNavigate={() => {}} />)
    expect(screen.getByText(/v1\.0\.2/)).toHaveTextContent('v1.0.2 · 51be261')
  })

  it('links to Knowledge when there are no courses', async () => {
    vi.mocked(listCourses).mockResolvedValue([])
    vi.mocked(listChats).mockResolvedValue([])
    renderWithData(<Sidebar route={{ name: 'chat', chatId: null }} health={health} onNavigate={() => {}} />)
    expect(await screen.findByRole('link', { name: 'Add a course in Knowledge' })).toBeInTheDocument()
    expect(screen.getByText('Signed in')).toBeInTheDocument()
  })
})
