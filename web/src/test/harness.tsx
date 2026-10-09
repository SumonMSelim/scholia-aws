import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactElement } from 'react'
import { vi } from 'vitest'
import type { Chat, Course, Model, ModelCatalog, ModelDefaults, Settings } from '@/lib/api'
import { AppDataProvider } from '@/components/app/AppData'

export const courses: Course[] = [
  { id: 'c1', title: 'Algebra', mine: true },
  { id: 'c2', title: 'Biology', mine: true },
]

export const models: Model[] = [
  { id: 'gpt-4.1-mini', provider: 'openai', label: 'GPT-4.1 mini' },
  { id: 'nova-lite', provider: 'bedrock', label: 'Nova Lite' },
]

// Server models are off here so provider filtering is visible. Tests that need server Bedrock override it.
export const defaults: ModelDefaults = { chat_model: 'gpt-4.1-mini', embed_model: 'titan-v2', web_search: true, server_models: false }

export const embedModels: Model[] = [{ id: 'titan-v2', provider: 'bedrock', label: 'Titan Text Embeddings V2', kind: 'embedding' }]

/** The /api/models body: chat models, the embedding model, and the server defaults. */
export const catalog: ModelCatalog = { models: [...models, ...embedModels], defaults }

export const settings: Settings = {
  default_model: '',
  providers: [
    { provider: 'openai', connected: false },
    { provider: 'bedrock', connected: false },
  ],
}

export function chat(id: string, courseId: string, title = id): Chat {
  return { id, course_id: courseId, title, model: 'nova-lite', created_at: '2026-09-30T10:00:00Z', updated_at: '2026-09-30T10:00:00Z' }
}

/** Renders inside the real data provider. The caller mocks the list calls in @/lib/api. */
export function renderWithData(ui: ReactElement) {
  const onSignOut = vi.fn()
  return { onSignOut, ...render(<AppDataProvider onSignOut={onSignOut}>{ui}</AppDataProvider>) }
}

export function goTo(path: string) {
  window.history.replaceState(null, '', path)
}

/** Opens the themed select named `name` and picks the option labelled `option`. */
export async function pick(name: string, option: string) {
  await userEvent.click(screen.getByRole('combobox', { name }))
  await userEvent.click(await screen.findByRole('option', { name: option }))
}
