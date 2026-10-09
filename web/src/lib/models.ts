import type { Chat, Course, Model, ProviderStatus } from '@/lib/api'

/**
 * Models for connected providers, or every model when nothing is connected yet.
 * Server-paid Bedrock needs no key, so its models stay offered while it is on.
 */
export function availableModels(models: Model[], providers: ProviderStatus[] | undefined, serverModels = false): Model[] {
  const connected = new Set((providers ?? []).filter((p) => p.connected).map((p) => p.provider))
  if (connected.size === 0) return models
  if (serverModels) connected.add('bedrock')
  return models.filter((model) => connected.has(model.provider))
}

/** The user's default wins, then the server default, then the first choice. */
export function pickModel(available: Model[], defaultModel: string | undefined, serverDefault = ''): string {
  const ids = new Set(available.map((model) => model.id))
  if (defaultModel && ids.has(defaultModel)) return defaultModel
  if (serverDefault && ids.has(serverDefault)) return serverDefault
  return available[0]?.id ?? ''
}

export function modelLabel(models: Model[], id: string): string {
  return models.find((model) => model.id === id)?.label ?? (id || 'Default model')
}

export type ChatGroup = { courseId: string; title: string; chats: Chat[] }

/**
 * Groups chats under their course, like projects. Chats arrive newest first, so
 * groups follow their latest chat. Owned courses without chats follow after.
 */
export function groupChats(chats: Chat[], courses: Course[]): ChatGroup[] {
  const titles = new Map(courses.map((course) => [course.id, course.title]))
  const groups = new Map<string, ChatGroup>()
  for (const chat of chats) {
    let group = groups.get(chat.course_id)
    if (!group) {
      group = { courseId: chat.course_id, title: titles.get(chat.course_id) ?? 'Unknown course', chats: [] }
      groups.set(chat.course_id, group)
    }
    group.chats.push(chat)
  }
  for (const course of courses) {
    if (course.mine !== false && !groups.has(course.id)) groups.set(course.id, { courseId: course.id, title: course.title, chats: [] })
  }
  return [...groups.values()]
}

export const providerLabels: Record<Model['provider'], string> = { openai: 'OpenAI', bedrock: 'Bedrock' }

/** Select options for models, grouped under their provider's name. */
export function modelOptions(models: Model[]): { value: string; label: string; group: string }[] {
  return models.map((model) => ({ value: model.id, label: model.label, group: providerLabels[model.provider] }))
}
