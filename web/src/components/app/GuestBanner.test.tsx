import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { courses, renderWithData, settings, catalog } from '@/test/harness'
import { GuestBanner } from './GuestBanner'

vi.mock('@/lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/api')>()),
  listCourses: vi.fn(),
  listChats: vi.fn(),
  listModels: vi.fn(),
  getSettings: vi.fn(),
  getUsage: vi.fn(),
}))

import { getSettings, getUsage, listChats, listCourses, listModels } from '@/lib/api'

const usage = { guest: true, limits: { messages: 40, uploads: 10, web: 5 }, used: { messages: 39, uploads: 0, web: 0 }, resets_at: '2026-10-02T00:00:00Z' }

describe('GuestBanner', () => {
  beforeEach(() => {
    vi.mocked(listCourses).mockResolvedValue(courses)
    vi.mocked(listChats).mockResolvedValue([])
    vi.mocked(listModels).mockResolvedValue(catalog)
    vi.mocked(getSettings).mockResolvedValue(settings)
    vi.mocked(getUsage).mockResolvedValue(usage)
  })
  afterEach(() => sessionStorage.clear())

  it('shows nothing for an email account', async () => {
    renderWithData(<GuestBanner />)
    await waitFor(() => expect(getUsage).toHaveBeenCalled())
    expect(screen.queryByRole('region', { name: 'Guest demo' })).not.toBeInTheDocument()
  })

  it('shows messages left and when the data is deleted', async () => {
    sessionStorage.setItem('scholia.guest_expires', new Date(Date.now() + 47 * 3_600_000 + 60_000).toISOString())
    renderWithData(<GuestBanner />)
    const banner = screen.getByRole('region', { name: 'Guest demo' })
    expect(await screen.findByText('1 message left today')).toBeInTheDocument()
    expect(banner).toHaveTextContent('data deleted in 47 hours')
  })

  it('never shows a negative count', async () => {
    sessionStorage.setItem('scholia.guest_expires', '2099-01-01T00:00:00Z')
    vi.mocked(getUsage).mockResolvedValue({ ...usage, used: { ...usage.used, messages: 45 } })
    renderWithData(<GuestBanner />)
    expect(await screen.findByText('0 messages left today')).toBeInTheDocument()
  })

  it('confirms before signing out to create an account', async () => {
    sessionStorage.setItem('scholia.guest_expires', '2099-01-01T00:00:00Z')
    const { onSignOut } = renderWithData(<GuestBanner />)
    await userEvent.click(screen.getByRole('button', { name: 'Create an account' }))
    expect(screen.getByText('Your guest chats and files will be lost.')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onSignOut).not.toHaveBeenCalled()
    await userEvent.click(screen.getByRole('button', { name: 'Create an account' }))
    await userEvent.click(screen.getByRole('button', { name: 'Sign out and create an account' }))
    expect(onSignOut).toHaveBeenCalledOnce()
  })
})
