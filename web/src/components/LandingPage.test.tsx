import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { LandingPage } from './LandingPage'

vi.mock('@/lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/api')>()),
  startGuest: vi.fn(),
}))

import { ApiError, startGuest } from '@/lib/api'
import { faq, hero } from '@/lib/content'

describe('LandingPage guest demo', () => {
  it('starts the demo in the chat, on a public course', async () => {
    vi.mocked(startGuest).mockResolvedValue()
    const onSignedIn = vi.fn()
    render(<LandingPage onSignedIn={onSignedIn} />)
    expect(screen.getByText(hero.demoNote)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Try the demo, no sign-up' }))
    expect(onSignedIn).toHaveBeenCalledWith('/chat')
  })

  it.each([
    [new ApiError(503, 'paused', 'paused'), 'The demo is paused right now. Please try again later.'],
    [new ApiError(429, 'too many', 'quota'), 'Too many people are trying the demo right now. Please try again in a few minutes.'],
    [new Error('network down'), 'network down'],
  ])('shows why the demo did not start', async (err, text) => {
    vi.mocked(startGuest).mockRejectedValue(err)
    const onSignedIn = vi.fn()
    render(<LandingPage onSignedIn={onSignedIn} />)
    const button = screen.getByRole('button', { name: 'Try the demo, no sign-up' })
    await userEvent.click(button)
    expect(await screen.findByRole('alert')).toHaveTextContent(text)
    expect(onSignedIn).not.toHaveBeenCalled()
    expect(button).toBeEnabled()
  })
})

describe('LandingPage questions', () => {
  it('opens an answer from its question', async () => {
    render(<LandingPage onSignedIn={vi.fn()} />)
    const first = screen.getByText(faq[0].q).closest('details')!
    expect(first).not.toHaveAttribute('open')
    await userEvent.click(screen.getByText(faq[0].q))
    expect(first).toHaveAttribute('open')
    expect(screen.getByText(faq[0].a)).toBeVisible()
  })
})
