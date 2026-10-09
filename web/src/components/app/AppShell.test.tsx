import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { chat, courses, goTo, renderWithData, settings, catalog } from '@/test/harness'
import { AppShell } from './AppShell'

vi.mock('@/lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/api')>()),
  listCourses: vi.fn(),
  listChats: vi.fn(),
  listModels: vi.fn(),
  getSettings: vi.fn(),
  getUsage: vi.fn(),
}))

import { getSettings, getUsage, listChats, listCourses, listModels } from '@/lib/api'

const health = { kind: 'loading' } as const
const usage = { guest: false, limits: { messages: 40, uploads: 10, web: 5 }, used: { messages: 0, uploads: 0, web: 0 }, resets_at: '2026-10-02T00:00:00Z' }

async function openDrawer() {
  renderWithData(<AppShell route={{ name: 'notFound' }} health={health} />)
  await userEvent.click(screen.getByRole('button', { name: 'Open sidebar' }))
  return screen.getByRole('dialog', { name: 'Sidebar' })
}

describe('AppShell drawer', () => {
  beforeEach(() => {
    goTo('/missing')
    vi.mocked(listCourses).mockResolvedValue(courses)
    vi.mocked(listChats).mockResolvedValue([chat('h1', 'c1', 'Vectors')])
    vi.mocked(listModels).mockResolvedValue(catalog)
    vi.mocked(getSettings).mockResolvedValue(settings)
    vi.mocked(getUsage).mockResolvedValue(usage)
  })
  afterEach(() => goTo('/'))

  it('opens as a modal dialog and moves focus into it', async () => {
    const drawer = await openDrawer()
    expect(drawer).toHaveAttribute('aria-modal', 'true')
    expect(drawer).toContainElement(document.activeElement as HTMLElement)
    expect(document.activeElement).not.toHaveAccessibleName('Close sidebar')
  })

  it('closes on Escape and returns focus to the menu button', async () => {
    await openDrawer()
    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Open sidebar' })).toHaveFocus()
  })

  it('closes from the backdrop and returns focus to the menu button', async () => {
    await openDrawer()
    await userEvent.click(screen.getByRole('button', { name: 'Close sidebar' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Open sidebar' })).toHaveFocus()
  })

  it('keeps the drawer open when Escape cancels a chat rename', async () => {
    const drawer = await openDrawer()
    await userEvent.click(await within(drawer).findByRole('button', { name: 'Rename Vectors' }))
    expect(within(drawer).getByLabelText('Chat title')).toHaveFocus()
    await userEvent.keyboard('{Escape}')
    expect(within(drawer).queryByLabelText('Chat title')).not.toBeInTheDocument()
    expect(screen.getByRole('dialog', { name: 'Sidebar' })).toBeInTheDocument()
  })
})
