import { useEffect } from 'react'
import { act, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { catalog, chat, courses, settings } from '@/test/harness'
import { AppDataProvider } from './AppData'
import { useAppData, type AppData } from './appDataContext'

vi.mock('@/lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/api')>()),
  listCourses: vi.fn(),
  listChats: vi.fn(),
  listModels: vi.fn(),
  getSettings: vi.fn(),
  getUsage: vi.fn(),
}))

import { getSettings, getUsage, listChats, listCourses, listModels } from '@/lib/api'

const usage = { guest: true, limits: { messages: 30, uploads: 5, web: 10 }, used: { messages: 1, uploads: 0, web: 0 }, resets_at: '2026-10-02T00:00:00Z' }

let data: AppData
const keep = (value: AppData) => {
  data = value
}

function Probe({ onData = keep }: { onData?: (value: AppData) => void }) {
  const value = useAppData()
  useEffect(() => onData(value))
  return <p>{value.coursesError ?? 'ok'}</p>
}

beforeEach(() => {
  vi.mocked(listCourses).mockResolvedValue(courses)
  vi.mocked(listChats).mockResolvedValue([chat('h1', 'c1')])
  vi.mocked(listModels).mockResolvedValue(catalog)
  vi.mocked(getSettings).mockResolvedValue(settings)
  vi.mocked(getUsage).mockResolvedValue(usage)
})

describe('AppDataProvider', () => {
  it('falls back to empty lists and keeps the course error when the first loads fail', async () => {
    vi.mocked(listCourses).mockRejectedValue(new Error('could not list courses'))
    vi.mocked(listChats).mockRejectedValue(new Error('down'))
    vi.mocked(listModels).mockRejectedValue(new Error('down'))
    vi.mocked(getSettings).mockRejectedValue(new Error('down'))
    vi.mocked(getUsage).mockRejectedValue(new Error('down'))
    render(<AppDataProvider onSignOut={() => {}}><Probe /></AppDataProvider>)
    expect(await screen.findByText('could not list courses')).toBeInTheDocument()
    await waitFor(() => expect(data.chats).toEqual([]))
    expect(data.courses).toEqual([])
    expect(data.models).toEqual([])
    expect(data.settings).toBeNull()
    expect(data.usage).toBeNull()
  })

  it('uses a generic message when a course load fails with a non-error', async () => {
    vi.mocked(listCourses).mockRejectedValue('nope')
    render(<AppDataProvider onSignOut={() => {}}><Probe /></AppDataProvider>)
    expect(await screen.findByText('could not list courses')).toBeInTheDocument()
  })

  it('keeps the last rows when a refresh fails', async () => {
    render(<AppDataProvider onSignOut={() => {}}><Probe /></AppDataProvider>)
    await waitFor(() => expect(data.settings).toEqual(settings))
    await waitFor(() => expect(data.usage).toEqual(usage))
    await waitFor(() => expect(data.chats).toHaveLength(1))
    vi.mocked(listChats).mockRejectedValue(new Error('down'))
    vi.mocked(getSettings).mockRejectedValue(new Error('down'))
    vi.mocked(getUsage).mockRejectedValue(new Error('down'))
    await act(async () => {
      await data.refreshChats()
      await data.refreshSettings()
      await data.refreshUsage()
    })
    expect(data.chats).toHaveLength(1)
    expect(data.settings).toEqual(settings)
    expect(data.usage).toEqual(usage)
  })

  it('keeps embedding models out of the chat models and applies updates', async () => {
    render(<AppDataProvider onSignOut={() => {}}><Probe /></AppDataProvider>)
    await waitFor(() => expect(data.models.length).toBeGreaterThan(0))
    expect(data.models.every((m) => m.kind !== 'embedding')).toBe(true)
    expect(data.defaults).toEqual(catalog.defaults)
    act(() => data.setCourses((rows) => [...rows, { id: 'c9', title: 'New' }]))
    await waitFor(() => expect(data.courses?.map((c) => c.id)).toContain('c9'))
    act(() => data.setChats(() => []))
    await waitFor(() => expect(data.chats).toEqual([]))
  })

  it('ignores results that arrive after unmount', async () => {
    let finish: (rows: typeof courses) => void = () => {}
    vi.mocked(listCourses).mockReturnValue(new Promise((resolve) => (finish = resolve)))
    const view = render(<AppDataProvider onSignOut={() => {}}><Probe /></AppDataProvider>)
    view.unmount()
    finish(courses)
    await Promise.resolve()
    expect(data.courses).toBeNull()
  })

  it('needs the provider', () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    expect(() => render(<Probe />)).toThrow('useAppData needs AppDataProvider')
  })
})
