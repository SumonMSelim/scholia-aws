import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { goTo } from '@/test/harness'
import { hero } from '@/lib/content'
import { siteName, siteTagline, siteTitle } from '@/lib/site'
import App from './App'

const health = { status: 'ok', version: 'v0.1.0', commit: 'abc123', env: 'prod', time: '2026-09-27T06:00:00Z' }

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    sessionToken: vi.fn(() => ''),
    setSessionToken: vi.fn(),
    listCourses: vi.fn(async () => []),
    listChats: vi.fn(async () => []),
    listModels: vi.fn(async () => []),
    getSettings: vi.fn(async () => ({ default_model: '', providers: [] })),
    startSession: vi.fn(async () => 'sess'),
    confirmSession: vi.fn(async () => {}),
  }
})

import { sessionToken, setSessionToken } from '@/lib/api'

const tokenMock = vi.mocked(sessionToken)

function healthy() {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(Response.json(health))
}

describe('App', () => {
  beforeEach(() => {
    goTo('/')
    tokenMock.mockReturnValue('')
  })
  afterEach(() => goTo('/'))

  it('shows a loading indicator, then ready when healthy', async () => {
    healthy()
    render(<App />)
    expect(screen.getByLabelText('Checking status')).toBeInTheDocument()
    expect(await screen.findByText('Operational')).toBeInTheDocument()
    // The footer names the deployed release and commit.
    const footer = screen.getByRole('contentinfo')
    expect(footer).toHaveTextContent(siteTitle)
    expect(footer).toHaveTextContent('v0.1.0 · abc123')
  })

  it('uses one title for the header and footer', () => {
    vi.spyOn(globalThis, 'fetch').mockReturnValue(new Promise(() => {}))
    render(<App />)
    expect(screen.getByRole('banner')).toHaveTextContent(`${siteName}${siteTagline}`)
    expect(screen.getByRole('contentinfo')).toHaveTextContent(siteTitle)
    // No version until health answers.
    expect(screen.getByRole('contentinfo')).not.toHaveTextContent(/v\d/)
  })

  it('shows an unavailable badge when the service fails', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('', { status: 502 }))
    render(<App />)
    expect(await screen.findByText('Unavailable')).toBeInTheDocument()
  })

  it('shows an unavailable badge on network errors', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValue('offline')
    render(<App />)
    expect(await screen.findByText('Unavailable')).toBeInTheDocument()
  })

  it('renders the landing page when signed out', () => {
    vi.spyOn(globalThis, 'fetch').mockReturnValue(new Promise(() => {}))
    render(<App />)
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent(hero.title)
    expect(screen.getByText(siteTagline)).toBeInTheDocument()
    for (const name of ['What Scholia does', 'How it works', 'Files you can upload', 'Questions']) {
      expect(screen.getByRole('heading', { level: 2, name })).toBeInTheDocument()
    }
    expect(screen.getByLabelText('Email')).toBeInTheDocument()
    expect(screen.queryByRole('navigation', { name: 'Sidebar' })).not.toBeInTheDocument()
    expect(screen.getByTestId('hero-graphic')).toHaveAttribute('aria-hidden', 'true')
    // The repository name is a link, not jargon.
    const text = (document.body.textContent ?? '').replaceAll('scholia-aws', 'scholia')
    for (const term of ['AWS', 'Lambda', 'Bedrock', 'Transcribe', 'KMS', 'OpenAI', 'API']) {
      expect(text).not.toMatch(new RegExp(term, 'i'))
    }
  })

  it.each(['/chat', '/chat/h1', '/knowledge', '/settings'])('sends a signed-out visit to %s back to the landing page', async (path) => {
    healthy()
    goTo(path)
    render(<App />)
    await waitFor(() => expect(window.location.pathname).toBe('/'))
    expect(screen.getByLabelText('Email')).toBeInTheDocument()
  })

  it('sends a signed-in visit to / to the chat', async () => {
    tokenMock.mockReturnValue('tok')
    healthy()
    render(<App />)
    await waitFor(() => expect(window.location.pathname).toBe('/chat'))
    expect(await screen.findByRole('heading', { name: 'Study with your course' })).toBeInTheDocument()
    expect(screen.getAllByRole('navigation', { name: 'Sidebar' }).length).toBeGreaterThan(0)
  })

  it('moves between private pages and back', async () => {
    tokenMock.mockReturnValue('tok')
    healthy()
    goTo('/knowledge')
    render(<App />)
    expect(await screen.findByRole('heading', { name: 'Knowledge' })).toBeInTheDocument()
    await userEvent.click(screen.getAllByRole('link', { name: 'Settings' })[0]!)
    expect(await screen.findByRole('heading', { name: 'Settings' })).toBeInTheDocument()
    window.history.back()
    expect(await screen.findByRole('heading', { name: 'Knowledge' })).toBeInTheDocument()
    goTo('/nowhere')
    window.dispatchEvent(new PopStateEvent('popstate'))
    expect(await screen.findByRole('heading', { name: 'Page not found' })).toBeInTheDocument()
  })

  it('opens the mobile sidebar and signs out to the landing page', async () => {
    tokenMock.mockReturnValue('tok')
    healthy()
    goTo('/settings')
    render(<App />)
    await userEvent.click(await screen.findByRole('button', { name: 'Open sidebar' }))
    expect(screen.getAllByRole('navigation', { name: 'Sidebar' })).toHaveLength(2)
    await userEvent.click(screen.getByRole('button', { name: 'Close sidebar' }))
    expect(screen.getAllByRole('navigation', { name: 'Sidebar' })).toHaveLength(1)
    tokenMock.mockReturnValue('')
    await userEvent.click(screen.getAllByRole('button', { name: 'Sign out' })[0]!)
    expect(setSessionToken).toHaveBeenCalledWith('')
    expect(await screen.findByLabelText('Email')).toBeInTheDocument()
    expect(window.location.pathname).toBe('/')
  })

  it('goes to the chat after signing in', async () => {
    healthy()
    render(<App />)
    await userEvent.type(screen.getByLabelText('Email'), 'ada@example.com')
    await userEvent.click(screen.getByRole('button', { name: 'Send code' }))
    await userEvent.type(await screen.findByLabelText('Email code'), '123456')
    await userEvent.click(screen.getByRole('button', { name: 'Confirm code' }))
    await waitFor(() => expect(window.location.pathname).toBe('/chat'))
    expect(await screen.findByRole('heading', { name: 'Study with your course' })).toBeInTheDocument()
  })

  it('ignores a late health failure after leaving the page', async () => {
    let rejectFetch: (err: unknown) => void = () => {}
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      () =>
        new Promise((_, reject) => {
          rejectFetch = reject
        }),
    )
    const view = render(<App />)
    view.unmount()
    rejectFetch(new Error('late'))
    await Promise.resolve()
  })
})
