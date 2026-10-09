import { describe, expect, it } from 'vitest'
import type { Chat, Model } from './api'
import { availableModels, groupChats, modelLabel, pickModel, serverModel } from './models'

const models: Model[] = [
  { id: 'gpt-4.1-mini', provider: 'openai', label: 'GPT-4.1 mini' },
  { id: 'nova-lite', provider: 'bedrock', label: 'Nova Lite' },
  { id: 'claude', provider: 'bedrock', label: 'Claude' },
]

function chat(id: string, course: string): Chat {
  return { id, course_id: course, title: id, model: '', created_at: '', updated_at: '' }
}

describe('availableModels', () => {
  it('keeps every model when nothing is connected', () => {
    expect(availableModels(models, undefined)).toEqual(models)
    expect(availableModels(models, [{ provider: 'openai', connected: false }])).toEqual(models)
  })

  it('filters to connected providers', () => {
    expect(availableModels(models, [{ provider: 'bedrock', connected: true }, { provider: 'openai', connected: false }]).map((m) => m.id)).toEqual(['nova-lite', 'claude'])
  })

  it('keeps the server-paid model beside a connected key, and only that one', () => {
    const openaiOnly = [{ provider: 'openai' as const, connected: true }]
    expect(availableModels(models, openaiOnly).map((m) => m.id)).toEqual(['gpt-4.1-mini'])
    expect(availableModels(models, openaiOnly, 'nova-lite').map((m) => m.id)).toEqual(['gpt-4.1-mini', 'nova-lite'])
  })

  it('offers a guest or keyless user only the server-paid model', () => {
    // Any other pick would be answered by the server model under the wrong label.
    expect(availableModels(models, undefined, 'nova-lite').map((m) => m.id)).toEqual(['nova-lite'])
    expect(availableModels(models, [{ provider: 'openai', connected: false }], 'nova-lite').map((m) => m.id)).toEqual(['nova-lite'])
  })
})

describe('serverModel', () => {
  it.each([
    ['on', { chat_model: 'nova-lite', embed_model: 'titan', web_search: false, server_models: true }, 'nova-lite'],
    ['off', { chat_model: 'nova-lite', embed_model: 'titan', web_search: false, server_models: false }, ''],
    ['unknown', null, ''],
  ])('is the default chat model only while server models are %s', (_name, defaults, want) => {
    expect(serverModel(defaults)).toBe(want)
  })
})

describe('pickModel', () => {
  it.each([
    ['the user default', 'claude', 'gpt-4.1-mini', 'claude'],
    ['the server default next', 'gone', 'claude', 'claude'],
    ['the first model otherwise', '', 'gone', 'gpt-4.1-mini'],
  ])('prefers %s', (_name, defaultModel, serverDefault, want) => {
    expect(pickModel(models, defaultModel, serverDefault)).toBe(want)
  })

  it('returns empty without models', () => {
    expect(pickModel([], 'claude')).toBe('')
  })
})

describe('modelLabel', () => {
  it('uses the label, the id, or a default', () => {
    expect(modelLabel(models, 'claude')).toBe('Claude')
    expect(modelLabel(models, 'custom')).toBe('custom')
    expect(modelLabel(models, '')).toBe('Default model')
  })
})

describe('groupChats', () => {
  it('groups by course in order of the latest chat, then empty owned courses', () => {
    const groups = groupChats(
      [chat('h3', 'b'), chat('h2', 'a'), chat('h1', 'b'), chat('h0', 'gone')],
      [
        { id: 'a', title: 'Algebra' },
        { id: 'b', title: 'Biology' },
        { id: 'c', title: 'Chemistry', mine: true },
        { id: 'p', title: 'Public', mine: false },
      ],
    )
    expect(groups.map((g) => [g.title, g.chats.map((c) => c.id)])).toEqual([
      ['Biology', ['h3', 'h1']],
      ['Algebra', ['h2']],
      ['Unknown course', ['h0']],
      ['Chemistry', []],
    ])
  })
})
