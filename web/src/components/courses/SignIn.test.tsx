import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { SignIn } from './SignIn'

vi.mock('@/lib/api', () => ({
  startSession: vi.fn(),
  confirmSession: vi.fn(),
  sessionToken: vi.fn(() => ''),
  setSessionToken: vi.fn(),
  ApiError: class ApiError extends Error {
    status: number
    constructor(status: number, message: string) {
      super(message)
      this.status = status
    }
  },
}))

import { confirmSession, sessionToken, setSessionToken, startSession } from '@/lib/api'

const startMock = vi.mocked(startSession)
const confirmMock = vi.mocked(confirmSession)
const tokenMock = vi.mocked(sessionToken)
const setTokenMock = vi.mocked(setSessionToken)

describe('SignIn', () => {
  beforeEach(() => {
    startMock.mockReset()
    confirmMock.mockReset()
    setTokenMock.mockReset()
    tokenMock.mockReset()
    tokenMock.mockReturnValue('')
  })

  it('sends a code, confirms it, and signs out', async () => {
    startMock.mockResolvedValue('sess')
    confirmMock.mockResolvedValue()
    render(<SignIn />)
    await userEvent.type(screen.getByLabelText('Email'), 'ada@example.com')
    await userEvent.click(screen.getByRole('button', { name: 'Send code' }))
    expect(startMock).toHaveBeenCalledWith('ada@example.com')
    await userEvent.type(screen.getByLabelText('Email code'), '123456')
    await userEvent.click(screen.getByRole('button', { name: 'Confirm code' }))
    expect(confirmMock).toHaveBeenCalledWith('ada@example.com', 'sess', '123456')
    expect(await screen.findByText('Signed in')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Sign out' }))
    expect(setTokenMock).toHaveBeenCalledWith('')
    expect(screen.getByLabelText('Email')).toBeInTheDocument()
  })

  it('shows a send failure', async () => {
    startMock.mockRejectedValue(new Error('could not send a code'))
    render(<SignIn />)
    await userEvent.type(screen.getByLabelText('Email'), 'ada@example.com')
    await userEvent.click(screen.getByRole('button', { name: 'Send code' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('could not send a code')
  })

  it('shows a confirm failure', async () => {
    startMock.mockResolvedValue('sess')
    confirmMock.mockRejectedValue('nope')
    render(<SignIn />)
    await userEvent.type(screen.getByLabelText('Email'), 'ada@example.com')
    await userEvent.click(screen.getByRole('button', { name: 'Send code' }))
    await userEvent.type(await screen.findByLabelText('Email code'), '000000')
    await userEvent.click(screen.getByRole('button', { name: 'Confirm code' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('code was not accepted')
  })

  it('starts signed in when a token is stored', () => {
    tokenMock.mockReturnValue('tok')
    render(<SignIn />)
    expect(screen.getByText('Signed in')).toBeInTheDocument()
  })
})
