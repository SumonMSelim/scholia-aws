import type { HealthState } from '@/hooks/useHealth'

/** The deployed release and commit, as the API reports them. Nothing until health answers. */
export function Version({ state, className }: { state: HealthState; className?: string }) {
  if (state.kind !== 'ok') return null
  const { version, commit } = state.health
  return (
    <span className={className} title={`Commit ${commit}`}>
      {version}
      {commit ? <span className="opacity-70"> · {commit}</span> : null}
    </span>
  )
}
