import { createContext, useContext } from 'react'
import type { Chat, Course, Model, ModelDefaults, Settings, Usage } from '@/lib/api'

export type AppData = {
  email: string
  /** Guest demo sessions carry the time their data is deleted. Empty for an email account. */
  guestExpiresAt: string
  usage: Usage | null
  courses: Course[] | null
  coursesError: string | null
  chats: Chat[] | null
  /** Chat models only. Embedding models are listed apart so chat pickers never offer them. */
  models: Model[]
  defaults: ModelDefaults | null
  settings: Settings | null
  setCourses: (update: (rows: Course[]) => Course[]) => void
  setChats: (update: (rows: Chat[]) => Chat[]) => void
  setSettings: (settings: Settings) => void
  refreshChats: () => Promise<void>
  refreshSettings: () => Promise<void>
  refreshUsage: () => Promise<void>
  signOut: () => void
}

export const AppDataContext = createContext<AppData | null>(null)

export function useAppData(): AppData {
  const value = useContext(AppDataContext)
  if (!value) throw new Error('useAppData needs AppDataProvider')
  return value
}
