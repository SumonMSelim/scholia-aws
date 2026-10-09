import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { AppDataContext, type AppData } from '@/components/app/appDataContext'
import { getSettings, getUsage, guestExpiresAt, listChats, listCourses, listModels, sessionEmail, type Chat, type Course, type Model, type ModelDefaults, type Settings, type Usage } from '@/lib/api'

function message(err: unknown, fallback: string): string {
  return err instanceof Error ? err.message : fallback
}

export function AppDataProvider({ onSignOut, children }: { onSignOut: () => void; children: ReactNode }) {
  const [courses, setCourseRows] = useState<Course[] | null>(null)
  const [coursesError, setCoursesError] = useState<string | null>(null)
  const [chats, setChatRows] = useState<Chat[] | null>(null)
  const [models, setModels] = useState<Model[]>([])
  const [defaults, setDefaults] = useState<ModelDefaults | null>(null)
  const [settings, setSettings] = useState<Settings | null>(null)
  const [usage, setUsage] = useState<Usage | null>(null)

  const refreshChats = useCallback(async () => {
    try {
      setChatRows(await listChats())
    } catch {
      setChatRows((rows) => rows ?? [])
    }
  }, [])

  const refreshSettings = useCallback(async () => {
    try {
      setSettings(await getSettings())
    } catch {
      // Keep the last settings. The settings page shows its own error.
    }
  }, [])

  const refreshUsage = useCallback(async () => {
    try {
      setUsage(await getUsage())
    } catch {
      // Usage only feeds the guest banner. Keep the last counts.
    }
  }, [])

  useEffect(() => {
    const ctrl = new AbortController()
    getUsage(ctrl.signal)
      .then((row) => {
        if (!ctrl.signal.aborted) setUsage(row)
      })
      .catch(() => {})
    listCourses(ctrl.signal)
      .then((rows) => {
        if (!ctrl.signal.aborted) setCourseRows(rows)
      })
      .catch((err: unknown) => {
        if (ctrl.signal.aborted) return
        setCourseRows([])
        setCoursesError(message(err, 'could not list courses'))
      })
    listModels(ctrl.signal)
      .then((catalog) => {
        if (ctrl.signal.aborted) return
        setModels(catalog.models.filter((model) => model.kind !== 'embedding'))
        setDefaults(catalog.defaults)
      })
      .catch(() => {})
    getSettings(ctrl.signal)
      .then((row) => {
        if (!ctrl.signal.aborted) setSettings(row)
      })
      .catch(() => {})
    listChats(ctrl.signal)
      .then((rows) => {
        if (!ctrl.signal.aborted) setChatRows(rows)
      })
      .catch(() => {
        if (!ctrl.signal.aborted) setChatRows([])
      })
    return () => ctrl.abort()
  }, [])

  const value = useMemo<AppData>(
    () => ({
      email: sessionEmail(),
      guestExpiresAt: guestExpiresAt(),
      usage,
      courses,
      coursesError,
      chats,
      models,
      defaults,
      settings,
      setCourses: (update) => setCourseRows((rows) => update(rows ?? [])),
      setChats: (update) => setChatRows((rows) => update(rows ?? [])),
      setSettings,
      refreshChats,
      refreshSettings,
      refreshUsage,
      signOut: onSignOut,
    }),
    [courses, coursesError, chats, models, defaults, settings, usage, refreshChats, refreshSettings, refreshUsage, onSignOut],
  )

  return <AppDataContext.Provider value={value}>{children}</AppDataContext.Provider>
}
