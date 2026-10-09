import { useState, type FormEvent } from 'react'
import { BookOpen, ChevronDown, ChevronRight, LogOut, MessageSquare, Pencil, Plus, Settings, SquarePen, Trash2, X } from 'lucide-react'
import { deleteChat, renameChat, type Chat } from '@/lib/api'
import { groupChats } from '@/lib/models'
import { navigate, type Route } from '@/lib/router'
import { Link } from '@/components/Link'
import { useAppData } from '@/components/app/appDataContext'
import { HealthIndicator } from '@/components/HealthIndicator'
import { Version } from '@/components/Version'
import { Button } from '@/components/ui/button'
import type { HealthState } from '@/hooks/useHealth'
import { siteName } from '@/lib/site'

const navItems = [
  { to: '/chat', label: 'Chat', icon: MessageSquare, match: 'chat' },
  { to: '/knowledge', label: 'Knowledge', icon: BookOpen, match: 'knowledge' },
  { to: '/settings', label: 'Settings', icon: Settings, match: 'settings' },
] as const

function navClass(active: boolean): string {
  return `flex items-center gap-2 rounded-md px-2.5 py-1.5 text-sm transition-colors ${
    active ? 'bg-sidebar-accent font-medium text-sidebar-accent-foreground' : 'text-sidebar-foreground/80 hover:bg-sidebar-accent/70'
  }`
}

export function Sidebar({ route, health, onNavigate }: { route: Route; health: HealthState; onNavigate: () => void }) {
  const { courses, chats, email, guestExpiresAt, setChats, signOut } = useAppData()
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({})
  const groups = groupChats(chats ?? [], courses ?? [])
  const activeChat = route.name === 'chat' ? route.chatId : null

  async function onDelete(chat: Chat) {
    if (!window.confirm(`Delete "${chat.title || 'New chat'}"?`)) return
    await deleteChat(chat.id)
    setChats((rows) => rows.filter((row) => row.id !== chat.id))
    if (activeChat === chat.id) navigate('/chat')
  }

  async function onRename(chat: Chat, title: string) {
    const updated = await renameChat(chat.id, title)
    setChats((rows) => rows.map((row) => (row.id === updated.id ? updated : row)))
  }

  return (
    <nav aria-label="Sidebar" className="flex h-full flex-col bg-sidebar text-sidebar-foreground">
      <div className="flex items-center justify-between px-3 pt-3 pb-2">
        <Link to="/chat" onClick={onNavigate} className="px-1.5 text-base font-semibold tracking-tight">
          {siteName}
        </Link>
      </div>
      <div className="px-3 pb-2">
        <Link
          to="/chat"
          onClick={onNavigate}
          className="flex items-center gap-2 rounded-md border border-sidebar-border bg-background px-2.5 py-1.5 text-sm font-medium shadow-xs hover:bg-sidebar-accent"
        >
          <SquarePen className="size-4" aria-hidden />
          New chat
        </Link>
      </div>
      <ul className="space-y-0.5 px-3">
        {navItems.map(({ to, label, icon: Icon, match }) => (
          <li key={to}>
            <Link to={to} onClick={onNavigate} className={navClass(route.name === match && !(match === 'chat' && activeChat))} aria-current={route.name === match ? 'page' : undefined}>
              <Icon className="size-4" aria-hidden />
              {label}
            </Link>
          </li>
        ))}
      </ul>

      <div className="mt-4 flex-1 overflow-y-auto px-3 pb-3">
        <p className="px-2.5 pb-1 text-xs font-medium text-muted-foreground">Courses</p>
        {courses === null ? <p className="px-2.5 text-xs text-muted-foreground">Loading…</p> : null}
        {courses !== null && groups.length === 0 ? (
          <Link to="/knowledge" onClick={onNavigate} className="block px-2.5 py-1 text-xs text-muted-foreground underline-offset-4 hover:underline">
            Add a course in Knowledge
          </Link>
        ) : null}
        <ul className="space-y-1">
          {groups.map((group) => {
            const open = !(collapsed[group.courseId] ?? group.chats.length === 0)
            return (
              <li key={group.courseId} aria-label={group.title}>
                <div className="group/course flex items-center rounded-md hover:bg-sidebar-accent/70">
                  <button
                    type="button"
                    aria-expanded={open}
                    className="flex min-w-0 flex-1 items-center gap-1.5 px-2 py-1.5 text-left text-sm font-medium"
                    onClick={() => setCollapsed((prev) => ({ ...prev, [group.courseId]: open }))}
                  >
                    {open ? <ChevronDown className="size-3.5 shrink-0" aria-hidden /> : <ChevronRight className="size-3.5 shrink-0" aria-hidden />}
                    <span className="truncate">{group.title}</span>
                  </button>
                  <Link
                    to={`/chat?course=${encodeURIComponent(group.courseId)}`}
                    onClick={onNavigate}
                    aria-label={`New chat in ${group.title}`}
                    className="mr-1 inline-flex size-6 items-center justify-center rounded text-muted-foreground opacity-100 hover:bg-background hover:text-foreground md:opacity-0 md:group-hover/course:opacity-100 md:focus-visible:opacity-100"
                  >
                    <Plus className="size-3.5" aria-hidden />
                  </Link>
                </div>
                {open ? (
                  <ul className="mt-0.5 ml-3 space-y-0.5 border-l border-sidebar-border pl-2">
                    {group.chats.length === 0 ? <li className="px-2 py-1 text-xs text-muted-foreground">No chats yet</li> : null}
                    {group.chats.map((chat) => (
                      <ChatRow
                        key={chat.id}
                        chat={chat}
                        active={chat.id === activeChat}
                        onNavigate={onNavigate}
                        onDelete={() => void onDelete(chat)}
                        onRename={(title) => onRename(chat, title)}
                      />
                    ))}
                  </ul>
                ) : null}
              </li>
            )
          })}
        </ul>
      </div>

      <div className="space-y-2 border-t border-sidebar-border px-3 py-3">
        <div className="flex items-center justify-between gap-2">
          <span className="truncate text-sm" title={email || undefined}>
            {guestExpiresAt ? 'Guest' : email || 'Signed in'}
          </span>
          <HealthIndicator state={health} />
        </div>
        <Button type="button" variant="ghost" size="sm" className="w-full justify-start" onClick={signOut}>
          <LogOut aria-hidden />
          Sign out
        </Button>
        <Version state={health} className="block px-1.5 text-xs text-muted-foreground tabular-nums" />
      </div>
    </nav>
  )
}

function ChatRow({
  chat,
  active,
  onNavigate,
  onDelete,
  onRename,
}: {
  chat: Chat
  active: boolean
  onNavigate: () => void
  onDelete: () => void
  onRename: (title: string) => Promise<void>
}) {
  const [editing, setEditing] = useState(false)
  const [title, setTitle] = useState(chat.title)
  const [error, setError] = useState(false)
  const label = chat.title || 'New chat'

  async function onSubmit(event: FormEvent) {
    event.preventDefault()
    const next = title.trim()
    if (next === '' || next === chat.title) {
      setEditing(false)
      return
    }
    try {
      await onRename(next)
      setEditing(false)
      setError(false)
    } catch {
      setError(true)
    }
  }

  if (editing) {
    return (
      <li>
        <form className="flex items-center gap-1" onSubmit={(event) => void onSubmit(event)}>
          <input
            aria-label="Chat title"
            aria-invalid={error || undefined}
            autoFocus
            className="h-7 min-w-0 flex-1 rounded-md border border-input bg-background px-2 text-sm outline-none focus-visible:border-ring"
            value={title}
            onChange={(event) => setTitle(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Escape') {
                setTitle(chat.title)
                setEditing(false)
              }
            }}
          />
          <Button type="button" variant="ghost" size="icon-xs" aria-label="Cancel rename" onClick={() => setEditing(false)}>
            <X aria-hidden />
          </Button>
        </form>
      </li>
    )
  }

  return (
    <li className={`group/chat flex items-center rounded-md ${active ? 'bg-sidebar-accent' : 'hover:bg-sidebar-accent/70'}`}>
      <Link
        to={`/chat/${encodeURIComponent(chat.id)}`}
        onClick={onNavigate}
        aria-current={active ? 'page' : undefined}
        className="min-w-0 flex-1 truncate px-2 py-1 text-sm"
      >
        {label}
      </Link>
      <span className="flex shrink-0 items-center opacity-100 md:opacity-0 md:group-focus-within/chat:opacity-100 md:group-hover/chat:opacity-100">
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          aria-label={`Rename ${label}`}
          onClick={() => {
            setTitle(chat.title)
            setEditing(true)
          }}
        >
          <Pencil aria-hidden />
        </Button>
        <Button type="button" variant="ghost" size="icon-xs" aria-label={`Delete ${label}`} onClick={onDelete}>
          <Trash2 aria-hidden />
        </Button>
      </span>
    </li>
  )
}
