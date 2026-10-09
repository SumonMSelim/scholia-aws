import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import type { HealthState } from '@/hooks/useHealth'

export function HealthIndicator({ state }: { state: HealthState }) {
  if (state.kind === 'loading') {
    return <Skeleton className="h-5 w-28" aria-label="Checking status" />
  }
  if (state.kind === 'error') {
    return (
      <Badge variant="destructive">
        <span className="size-1.5 rounded-full bg-current" aria-hidden />
        Unavailable
      </Badge>
    )
  }
  return (
    <Badge variant="success">
      <span className="size-1.5 rounded-full bg-current" aria-hidden />
      Operational
    </Badge>
  )
}
