import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { courses, pick, renderWithData, catalog } from '@/test/harness'
import { SettingsPage } from './SettingsPage'

vi.mock('@/lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/api')>()),
  listCourses: vi.fn(),
  listChats: vi.fn(),
  listModels: vi.fn(),
  getSettings: vi.fn(),
  putSettings: vi.fn(),
  putProviderKey: vi.fn(),
  deleteProviderKey: vi.fn(),
}))

import { deleteProviderKey, getSettings, listChats, listCourses, listModels, putProviderKey, putSettings } from '@/lib/api'

const disconnected = { default_model: '', providers: [{ provider: 'openai' as const, connected: false }, { provider: 'bedrock' as const, connected: false }] }
const openaiOn = { default_model: '', providers: [{ provider: 'openai' as const, connected: true }, { provider: 'bedrock' as const, connected: false }] }

describe('SettingsPage', () => {
  beforeEach(() => {
    vi.mocked(listCourses).mockResolvedValue(courses)
    vi.mocked(listChats).mockResolvedValue([])
    vi.mocked(listModels).mockResolvedValue(catalog)
  })

  it('saves a key, then disconnects it', async () => {
    vi.mocked(getSettings).mockResolvedValueOnce(disconnected).mockResolvedValueOnce(openaiOn).mockResolvedValueOnce(disconnected)
    vi.mocked(putProviderKey).mockResolvedValue()
    vi.mocked(deleteProviderKey).mockResolvedValue()
    renderWithData(<SettingsPage />)
    const card = await screen.findByLabelText('OpenAI')
    await waitFor(() => expect(within(card).getByText('Not connected')).toBeInTheDocument())
    const input = within(card).getByLabelText('OpenAI API key')
    expect(input).toHaveAttribute('type', 'password')
    await userEvent.type(input, 'sk-secret')
    await userEvent.click(within(card).getByRole('button', { name: 'Save key' }))
    expect(putProviderKey).toHaveBeenCalledWith('openai', 'sk-secret')
    expect(await within(card).findByText('Connected')).toBeInTheDocument()
    expect(input).toHaveValue('')
    expect(within(card).getByRole('button', { name: 'Replace key' })).toBeDisabled()

    await userEvent.click(within(card).getByRole('button', { name: 'Disconnect' }))
    expect(deleteProviderKey).toHaveBeenCalledWith('openai')
    expect(await within(card).findByText('Not connected')).toBeInTheDocument()
  })

  it('shows a key error', async () => {
    vi.mocked(getSettings).mockResolvedValue(disconnected)
    vi.mocked(putProviderKey).mockRejectedValue(new Error('key rejected'))
    renderWithData(<SettingsPage />)
    const card = await screen.findByLabelText('Amazon Bedrock')
    await userEvent.type(within(card).getByLabelText('Amazon Bedrock API key'), 'bad')
    await userEvent.click(within(card).getByRole('button', { name: 'Save key' }))
    expect(await within(card).findByRole('alert')).toHaveTextContent('key rejected')
  })

  it('saves the default model from connected providers', async () => {
    vi.mocked(getSettings).mockResolvedValue(openaiOn)
    vi.mocked(putSettings).mockResolvedValue({ ...openaiOn, default_model: 'gpt-4.1-mini' })
    renderWithData(<SettingsPage />)
    const select = await screen.findByRole('combobox', { name: 'Default model' })
    const save = screen.getByRole('button', { name: 'Save' })
    await waitFor(() => expect(select).toHaveTextContent('Server default'))
    expect(save).toBeDisabled()
    await userEvent.click(select)
    expect(await screen.findByRole('option', { name: 'GPT-4.1 mini' })).toBeInTheDocument()
    expect(within(screen.getByRole('listbox')).getByText('OpenAI')).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: 'Nova Lite' })).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('option', { name: 'GPT-4.1 mini' }))
    await userEvent.click(save)
    expect(putSettings).toHaveBeenCalledWith('gpt-4.1-mini')
    expect(await screen.findByText('Saved')).toBeInTheDocument()
    expect(select).toHaveTextContent('GPT-4.1 mini')
  })

  it('shows a settings save error and signs out', async () => {
    vi.mocked(getSettings).mockResolvedValue({ ...disconnected, default_model: 'retired-model' })
    vi.mocked(putSettings).mockRejectedValue(new Error('unknown model'))
    const { onSignOut } = renderWithData(<SettingsPage />)
    await waitFor(() => expect(screen.getByRole('combobox', { name: 'Default model' })).toHaveTextContent('retired-model'))
    await pick('Default model', 'Server default (GPT-4.1 mini)')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('unknown model')
    await userEvent.click(screen.getByRole('button', { name: 'Sign out' }))
    expect(onSignOut).toHaveBeenCalled()
  })

  it('hides provider keys for a guest and keeps the default model', async () => {
    sessionStorage.setItem('scholia.guest_expires', '2026-10-03T00:00:00Z')
    vi.mocked(getSettings).mockResolvedValue(disconnected)
    renderWithData(<SettingsPage />)
    expect(await screen.findByText(/Sign in with email to use your own API keys/)).toBeInTheDocument()
    expect(screen.queryByLabelText('OpenAI')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('OpenAI API key')).not.toBeInTheDocument()
    expect(screen.getByRole('combobox', { name: 'Default model' })).toBeInTheDocument()
    expect(screen.getByText('Guest demo')).toBeInTheDocument()
    sessionStorage.clear()
  })
})
