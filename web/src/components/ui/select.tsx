import { Select as Base } from '@base-ui/react/select'
import { Check, ChevronsUpDown } from 'lucide-react'
import type { ReactNode } from 'react'
import { cn } from 'cn'

export type SelectOption = { value: string; label: string; group?: string; disabled?: boolean }

type Group = { value: string; items: SelectOption[] }

/** Keeps first-seen group order. Options without a group come first, under no heading. */
function grouped(options: SelectOption[]): Group[] {
  const out: Group[] = []
  for (const option of options) {
    const key = option.group ?? ''
    let group = out.find((row) => row.value === key)
    if (!group) {
      group = { value: key, items: [] }
      if (key === '') out.unshift(group)
      else out.push(group)
    }
    group.items.push(option)
  }
  return out
}

const sizes = {
  sm: 'h-8 px-2 text-xs',
  md: 'h-9 px-3 text-sm',
}

/**
 * Themed single select. The trigger carries the accessible name, so pass `label` even when an
 * outer text label is shown. An empty `value` shows the placeholder.
 */
export function Select({
  label,
  value,
  onValueChange,
  options,
  placeholder = 'Select…',
  icon,
  disabled,
  size = 'md',
  className,
}: {
  label: string
  value: string
  onValueChange: (value: string) => void
  options: SelectOption[]
  placeholder?: string
  icon?: ReactNode
  disabled?: boolean
  size?: keyof typeof sizes
  className?: string
}) {
  const groups = grouped(options)
  const items = options.map((option) => ({ value: option.value, label: option.label }))
  const current = options.find((option) => option.value === value)

  return (
    <Base.Root
      items={items}
      value={current ? value : null}
      onValueChange={(next) => {
        if (typeof next === 'string') onValueChange(next)
      }}
      disabled={disabled}
      modal={false}
    >
      <Base.Trigger
        aria-label={label}
        className={cn(
          'group/select inline-flex min-w-0 items-center gap-2 border border-input bg-background text-left font-normal text-foreground outline-none transition-colors select-none',
          'hover:not-data-disabled:border-foreground/40 focus-visible:border-primary data-popup-open:border-primary',
          'data-disabled:cursor-not-allowed data-disabled:opacity-60',
          sizes[size],
          className,
        )}
      >
        {icon ? <span className="shrink-0 text-muted-foreground [&_svg]:size-3.5">{icon}</span> : null}
        <Base.Value className="min-w-0 flex-1 truncate data-placeholder:text-muted-foreground" placeholder={placeholder}>
          {() => current?.label ?? placeholder}
        </Base.Value>
        <Base.Icon className="shrink-0 text-muted-foreground">
          <ChevronsUpDown className="size-3.5" aria-hidden />
        </Base.Icon>
      </Base.Trigger>
      <Base.Portal>
        <Base.Positioner className="z-50 outline-none select-none" sideOffset={4} alignItemWithTrigger={false}>
          <Base.Popup
            className={cn(
              'max-h-[min(var(--available-height),20rem)] min-w-(--anchor-width) max-w-[min(24rem,calc(100vw-2rem))] origin-(--transform-origin) overflow-y-auto border border-border bg-popover py-1 text-sm text-popover-foreground shadow-[0.25rem_0.25rem_0] shadow-black/8 outline-none',
              'transition-[opacity] duration-100 data-ending-style:opacity-0 data-starting-style:opacity-0',
            )}
          >
            <Base.List>
              {groups.map((group) =>
                group.value === '' ? (
                  group.items.map((option) => <Item key={option.value} option={option} />)
                ) : (
                  <Base.Group key={group.value} className="not-first:mt-1 not-first:border-t not-first:border-border not-first:pt-1">
                    <Base.GroupLabel className="px-3 pt-1.5 pb-1 text-[0.7rem] font-semibold tracking-wide text-muted-foreground uppercase">
                      {group.value}
                    </Base.GroupLabel>
                    {group.items.map((option) => (
                      <Item key={option.value} option={option} />
                    ))}
                  </Base.Group>
                ),
              )}
            </Base.List>
          </Base.Popup>
        </Base.Positioner>
      </Base.Portal>
    </Base.Root>
  )
}

function Item({ option }: { option: SelectOption }) {
  return (
    <Base.Item
      value={option.value}
      label={option.label}
      disabled={option.disabled}
      className={cn(
        'relative grid cursor-default grid-cols-[1fr_1rem] items-center gap-3 border-l-2 border-transparent py-1.5 pr-3 pl-2.5 outline-none select-none',
        'data-highlighted:bg-muted data-selected:border-primary data-selected:font-medium',
        'data-disabled:cursor-not-allowed data-disabled:opacity-50',
      )}
    >
      <Base.ItemText className="truncate">{option.label}</Base.ItemText>
      <Base.ItemIndicator className="text-primary">
        <Check className="size-3.5" aria-hidden />
      </Base.ItemIndicator>
    </Base.Item>
  )
}
