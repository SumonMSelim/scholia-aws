import { useCallback, useEffect, useState } from 'react'
import { sessionToken, setSessionToken } from '@/lib/api'
import { matchRoute, navigate, useLocation } from '@/lib/router'
import { AppDataProvider } from '@/components/app/AppData'
import { AppShell } from '@/components/app/AppShell'
import { HealthIndicator } from '@/components/HealthIndicator'
import { LandingPage } from '@/components/LandingPage'
import { Version } from '@/components/Version'
import { useHealth } from '@/hooks/useHealth'
import { siteName, siteTagline, siteTitle } from '@/lib/site'

export default function App() {
  const health = useHealth()
  const { path } = useLocation()
  const [signedIn, setSignedIn] = useState(sessionToken() !== '')
  const route = matchRoute(path)
  const redirect = signedIn ? (route.name === 'home' ? '/chat' : null) : route.name === 'home' ? null : '/'

  useEffect(() => {
    if (redirect) navigate(redirect, { replace: true })
  }, [redirect])

  const onSignOut = useCallback(() => {
    setSessionToken('')
    setSignedIn(false)
    navigate('/')
  }, [])

  if (redirect) return null

  if (signedIn) {
    return (
      <AppDataProvider onSignOut={onSignOut}>
        <AppShell route={route} health={health} />
      </AppDataProvider>
    )
  }

  return (
    <div className="min-h-svh bg-background text-foreground">
      <header className="mx-auto flex max-w-6xl items-center justify-between border-b border-border px-4 py-4 sm:px-6">
        <div className="flex items-baseline gap-2">
          <span className="text-lg font-semibold tracking-tight">{siteName}</span>
          <span className="hidden text-xs text-muted-foreground sm:inline">{siteTagline}</span>
        </div>
        <HealthIndicator state={health} />
      </header>
      <LandingPage
        onSignedIn={(path) => {
          setSignedIn(true)
          navigate(path ?? '/chat')
        }}
      />
      <footer className="mx-auto flex max-w-6xl flex-wrap items-center justify-between gap-2 px-4 py-10 text-sm text-muted-foreground sm:px-6">
        <span>{siteTitle}</span>
        <Version state={health} className="text-xs tabular-nums" />
      </footer>
    </div>
  )
}
