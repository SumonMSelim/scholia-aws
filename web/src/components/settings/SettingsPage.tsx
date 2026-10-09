import { useState, type FormEvent } from 'react'
import { ApiError, deleteProviderKey, providers, putProviderKey, putSettings, type Provider } from '@/lib/api'
import { availableModels, modelLabel, modelOptions, serverModel } from '@/lib/models'
import { fieldClass } from '@/lib/field'
import { useAppData } from '@/components/app/appDataContext'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Select } from '@/components/ui/select'

const providerInfo: Record<Provider, { name: string; hint: string }> = {
  openai: { name: 'OpenAI', hint: 'An API key from platform.openai.com.' },
  bedrock: { name: 'Amazon Bedrock', hint: 'A Bedrock API key for your AWS account.' },
}

function message(err: unknown, fallback: string): string {
  if (err instanceof ApiError || err instanceof Error) return err.message
  return fallback
}

export function SettingsPage() {
  const { settings, email, guestExpiresAt, signOut } = useAppData()

  return (
    <div className="space-y-8 px-4 py-8 sm:px-6 lg:px-8">
      <h1 className="text-2xl font-semibold tracking-tight">Settings</h1>

      <section aria-labelledby="providers" className="space-y-3">
        <div>
          <h2 id="providers" className="text-base font-medium">
            Providers
          </h2>
          <p className="text-sm text-muted-foreground">
            {guestExpiresAt
              ? 'The demo answers with the built-in model. Sign in with email to use your own API keys.'
              : 'Connect a provider with your own API key. Keys are encrypted on your account and never shown again.'}
          </p>
        </div>
        {/* A guest's data is deleted after the demo, so it never stores a key. */}
        {guestExpiresAt ? null : (
          <>
            {settings === null ? <p className="text-sm text-muted-foreground">Loading…</p> : null}
            <div className="grid gap-3 md:grid-cols-2 2xl:grid-cols-3">
              {providers.map((provider) => (
                <ProviderCard key={provider} provider={provider} connected={settings?.providers.find((row) => row.provider === provider)?.connected ?? false} />
              ))}
            </div>
          </>
        )}
      </section>

      <DefaultModel />

      <section aria-labelledby="account" className="space-y-3">
        <h2 id="account" className="text-base font-medium">
          Account
        </h2>
        <Card size="sm">
          <CardContent className="flex flex-wrap items-center justify-between gap-3">
            <span className="text-sm">{guestExpiresAt ? 'Guest demo' : email || 'Signed in'}</span>
            <Button type="button" variant="outline" onClick={signOut}>
              Sign out
            </Button>
          </CardContent>
        </Card>
      </section>
    </div>
  )
}

function ProviderCard({ provider, connected }: { provider: Provider; connected: boolean }) {
  const { refreshSettings } = useAppData()
  const [apiKey, setApiKey] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const info = providerInfo[provider]

  async function run(action: () => Promise<void>, fallback: string) {
    setBusy(true)
    setError(null)
    try {
      await action()
      await refreshSettings()
    } catch (err) {
      setError(message(err, fallback))
    } finally {
      setBusy(false)
    }
  }

  function onSave(event: FormEvent) {
    event.preventDefault()
    void run(async () => {
      await putProviderKey(provider, apiKey.trim())
      setApiKey('')
    }, 'could not store the key')
  }

  return (
    <Card size="sm" aria-label={info.name}>
      <CardHeader>
        <div className="flex items-center justify-between gap-2">
          <CardTitle>{info.name}</CardTitle>
          <Badge variant={connected ? 'success' : 'outline'}>{connected ? 'Connected' : 'Not connected'}</Badge>
        </div>
        <CardDescription>{info.hint}</CardDescription>
      </CardHeader>
      <CardContent>
        <form className="space-y-3" onSubmit={onSave}>
          <label className="block space-y-1.5 text-sm font-medium">
            <span className="sr-only">{info.name} API key</span>
            <input
              aria-label={`${info.name} API key`}
              type="password"
              autoComplete="off"
              spellCheck={false}
              className={fieldClass}
              placeholder={connected ? '•••••••• stored' : 'Paste API key'}
              value={apiKey}
              onChange={(event) => setApiKey(event.target.value)}
            />
          </label>
          <div className="flex gap-2">
            <Button type="submit" disabled={busy || apiKey.trim() === ''}>
              {connected ? 'Replace key' : 'Save key'}
            </Button>
            {connected ? (
              <Button type="button" variant="ghost" disabled={busy} onClick={() => void run(() => deleteProviderKey(provider), 'could not remove the key')}>
                Disconnect
              </Button>
            ) : null}
          </div>
          {error ? (
            <Alert variant="destructive">
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}
        </form>
      </CardContent>
    </Card>
  )
}

function DefaultModel() {
  const { models, defaults, settings, setSettings } = useAppData()
  const [picked, setPicked] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [saved, setSaved] = useState(false)
  const choices = availableModels(models, settings?.providers, serverModel(defaults))

  const value = picked ?? settings?.default_model ?? ''

  async function onSubmit(event: FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError(null)
    setSaved(false)
    try {
      setSettings(await putSettings(value))
      setPicked(null)
      setSaved(true)
    } catch (err) {
      setError(message(err, 'could not save settings'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <section aria-labelledby="default-model" className="space-y-3">
      <div>
        <h2 id="default-model" className="text-base font-medium">
          Default model
        </h2>
        <p className="text-sm text-muted-foreground">New chats start with this model. You can still pick another per chat.</p>
      </div>
      <form className="flex flex-wrap items-center gap-2" onSubmit={(event) => void onSubmit(event)}>
        <Select
          label="Default model"
          className="w-full min-w-0 sm:w-[20rem]"
          value={value}
          onValueChange={setPicked}
          options={[
            { value: '', label: defaults ? `Server default (${modelLabel(models, defaults.chat_model)})` : 'Server default' },
            ...(value !== '' && !choices.some((model) => model.id === value) ? [{ value, label: value }] : []),
            ...modelOptions(choices),
          ]}
        />
        <Button type="submit" variant="outline" disabled={busy || settings === null || value === settings.default_model}>
          Save
        </Button>
        {saved ? <span className="text-xs text-muted-foreground">Saved</span> : null}
      </form>
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
    </section>
  )
}
