import { useEffect, useRef, useState } from 'react'
import { Menu } from 'lucide-react'
import type { Route } from '@/lib/router'
import { GuestBanner } from '@/components/app/GuestBanner'
import { Sidebar } from '@/components/app/Sidebar'
import { ChatPage } from '@/components/chat/ChatPage'
import { KnowledgePage } from '@/components/knowledge/KnowledgePage'
import { CoursePage } from '@/components/knowledge/CoursePage'
import { SettingsPage } from '@/components/settings/SettingsPage'
import { Button } from '@/components/ui/button'
import { Link } from '@/components/Link'
import type { HealthState } from '@/hooks/useHealth'
import { siteName } from '@/lib/site'

function Page({ route }: { route: Route }) {
  if (route.name === 'chat') return <ChatPage chatId={route.chatId} />
  if (route.name === 'knowledge') return route.courseId ? <CoursePage courseId={route.courseId} /> : <KnowledgePage />
  if (route.name === 'settings') return <SettingsPage />
  return (
    <div className="mx-auto max-w-md px-6 py-24 text-center">
      <h1 className="text-xl font-semibold">Page not found</h1>
      <Link to="/chat" className="mt-3 inline-block text-sm text-primary underline-offset-4 hover:underline">
        Back to chat
      </Link>
    </div>
  )
}

export function AppShell({ route, health }: { route: Route; health: HealthState }) {
  const [open, setOpen] = useState(false)
  const menuRef = useRef<HTMLButtonElement>(null)
  const drawerRef = useRef<HTMLElement>(null)
  const wasOpen = useRef(false)

  // Keyboard and screen reader users land inside the drawer when it opens and
  // back on the menu button when it closes, instead of at the top of the page.
  useEffect(() => {
    if (open) drawerRef.current?.querySelector<HTMLElement>('a[href], button, input')?.focus()
    else if (wasOpen.current) menuRef.current?.focus()
    wasOpen.current = open
  }, [open])

  return (
    <div className="flex h-svh overflow-hidden bg-background text-[15px] text-foreground">
      <aside className="hidden w-72 shrink-0 border-r border-sidebar-border md:block">
        <Sidebar route={route} health={health} onNavigate={() => {}} />
      </aside>
      {open ? (
        <div
          role="dialog"
          aria-modal="true"
          aria-label="Sidebar"
          className="fixed inset-0 z-40 md:hidden"
          onKeyDown={(event) => {
            // Escape in the chat rename field cancels the rename, not the drawer.
            if (event.key === 'Escape' && !(event.target instanceof HTMLInputElement)) setOpen(false)
          }}
        >
          <button type="button" className="absolute inset-0 bg-black/30" aria-label="Close sidebar" onClick={() => setOpen(false)} />
          <aside ref={drawerRef} className="absolute inset-y-0 left-0 w-[85vw] max-w-72 border-r border-sidebar-border shadow-xl">
            <Sidebar route={route} health={health} onNavigate={() => setOpen(false)} />
          </aside>
        </div>
      ) : null}
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex items-center gap-2 border-b border-border px-3 py-2 md:hidden">
          <Button ref={menuRef} type="button" variant="ghost" size="icon-sm" aria-label="Open sidebar" onClick={() => setOpen(true)}>
            <Menu aria-hidden />
          </Button>
          <span className="font-semibold tracking-tight">{siteName}</span>
        </header>
        <GuestBanner />
        <main className="min-h-0 flex-1 overflow-y-auto">
          <Page route={route} />
        </main>
      </div>
    </div>
  )
}
