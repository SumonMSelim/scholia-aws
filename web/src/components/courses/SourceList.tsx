import type { Source, SourceStatus } from '@/lib/api'
import { Badge } from '@/components/ui/badge'

const labels: Record<SourceStatus, string> = {
  queued: 'Queued',
  processing: 'Processing',
  ready: 'Ready',
  failed: 'Failed',
}

function variant(status: SourceStatus): 'success' | 'secondary' | 'destructive' {
  if (status === 'failed') return 'destructive'
  if (status === 'ready') return 'success'
  return 'secondary'
}

export function SourceList({ sources }: { sources: Source[] }) {
  if (sources.length === 0) {
    return <p className="text-sm text-muted-foreground">No files yet. Add lectures, slides, notes, or assignments.</p>
  }
  return (
    <ul className="divide-y divide-border rounded-xl border border-border bg-card">
      {sources.map((source) => (
        <li key={source.id} className="flex items-center justify-between gap-3 px-4 py-2.5">
          <span className="truncate text-sm">{source.name}</span>
          <span className="flex items-center gap-2">
            {source.status === 'failed' && source.failure_reason ? (
              <span className="text-xs text-destructive">{source.failure_reason}</span>
            ) : null}
            <Badge variant={variant(source.status)}>{labels[source.status]}</Badge>
          </span>
        </li>
      ))}
    </ul>
  )
}
