import { useState, type FormEvent } from 'react'
import { ApiError, confirmSession, sessionToken, setSessionToken, startSession } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { fieldClass } from '@/lib/field'

function message(err: unknown, fallback: string): string {
  if (err instanceof ApiError || err instanceof Error) return err.message
  return fallback
}

export function SignIn({ onChange }: { onChange?: (signedIn: boolean) => void }) {
  const [email, setEmail] = useState('')
  const [session, setSession] = useState('')
  const [code, setCode] = useState('')
  const [signedIn, setSignedIn] = useState(sessionToken() !== '')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function onSend(event: FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError(null)
    try {
      setSession(await startSession(email.trim()))
    } catch (err) {
      setError(message(err, 'could not send a code'))
    } finally {
      setBusy(false)
    }
  }

  async function onConfirm(event: FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await confirmSession(email.trim(), session, code.trim())
      setSignedIn(true)
      onChange?.(true)
      setCode('')
    } catch (err) {
      setError(message(err, 'code was not accepted'))
    } finally {
      setBusy(false)
    }
  }

  function onSignOut() {
    setSessionToken('')
    setSignedIn(false)
    setSession('')
    onChange?.(false)
  }

  if (signedIn) {
    return (
      <div className="flex items-center gap-3 text-sm">
        <span className="text-muted-foreground">Signed in</span>
        <Button type="button" variant="outline" onClick={onSignOut}>
          Sign out
        </Button>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <form className="space-y-3" onSubmit={(event) => void onSend(event)}>
        <label className="block space-y-1.5 text-sm font-medium">
          Email
          <input
            aria-label="Email"
            type="email"
            autoComplete="email"
            className={fieldClass}
            value={email}
            onChange={(event) => setEmail(event.target.value)}
          />
        </label>
        <Button className="w-full" type="submit" disabled={busy || email.trim() === ''}>
          Send code
        </Button>
      </form>
      {session !== '' ? (
        <form className="space-y-3" onSubmit={(event) => void onConfirm(event)}>
          <label className="block space-y-1.5 text-sm font-medium">
            Email code
            <input
              aria-label="Email code"
              inputMode="numeric"
              autoComplete="one-time-code"
              className={fieldClass}
              value={code}
              onChange={(event) => setCode(event.target.value)}
            />
          </label>
          <Button className="w-full" type="submit" disabled={busy || code.trim() === ''}>
            Confirm code
          </Button>
        </form>
      ) : null}
      {error ? (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      ) : null}
    </div>
  )
}
