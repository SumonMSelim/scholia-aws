import { useState } from 'react'
import { relativeFrom } from '@/lib/time'
import { useAppData } from '@/components/app/appDataContext'
import { Button } from '@/components/ui/button'

export function GuestBanner() {
  const { guestExpiresAt, usage, signOut } = useAppData()
  const [confirming, setConfirming] = useState(false)
  if (!guestExpiresAt) return null

  const left = usage ? Math.max(0, usage.limits.messages - usage.used.messages) : null

  return (
    <div role="region" aria-label="Guest demo" className="border-b border-primary/30 bg-primary/5 px-4 py-2 text-xs sm:px-6 lg:px-8">
      {confirming ? (
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-medium">Your guest chats and files will be lost.</span>
          <Button type="button" size="xs" onClick={signOut}>
            Sign out and create an account
          </Button>
          <Button type="button" size="xs" variant="ghost" onClick={() => setConfirming(false)}>
            Cancel
          </Button>
        </div>
      ) : (
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
          <span className="font-semibold text-primary">Guest demo</span>
          {left !== null ? (
            <>
              <span aria-hidden className="text-muted-foreground">
                ·
              </span>
              <span>
                {left} {left === 1 ? 'message' : 'messages'} left today
              </span>
            </>
          ) : null}
          <span aria-hidden className="hidden text-muted-foreground sm:inline">
            ·
          </span>
          <span className="hidden text-muted-foreground sm:inline">data deleted {relativeFrom(guestExpiresAt)}</span>
          <Button type="button" size="xs" variant="outline" className="ml-auto" onClick={() => setConfirming(true)}>
            Create an account
          </Button>
        </div>
      )}
    </div>
  )
}
