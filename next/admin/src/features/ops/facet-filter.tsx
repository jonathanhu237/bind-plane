import { ChevronDown } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuLabel, DropdownMenuRadioGroup, DropdownMenuRadioItem, DropdownMenuSeparator, DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Separator } from '@/components/ui/separator'
import { cn } from '@/lib/utils'

// The menu value of "no filter" when the facet has no default of its own.
const ANY = '\u0000any'

/**
 * A compact filter button for one facet of a list: just the facet's name until a value is chosen, then
 * the value beside it. Choosing the default (everything, or `defaultValue` when the facet has one)
 * clears the filter, so `value` is only set while the list is actually narrowed by it.
 */
export function FacetFilter({ label, value, options, onChange, anyLabel, defaultValue }: {
  label: string
  value?: string
  options: [value: string, name: string][]
  onChange: (value: string | undefined) => void
  /** The first menu item, which shows everything. Leave it out when the facet has a `defaultValue`. */
  anyLabel?: string
  defaultValue?: string
}) {
  const unset = defaultValue ?? ANY
  const current = value ?? unset
  const chosen = value ? options.find(([option]) => option === value)?.[1] ?? value : undefined
  return <DropdownMenu>
    <DropdownMenuTrigger asChild>
      <Button type="button" variant="outline" size="sm" aria-label={chosen ? `${label}: ${chosen}` : label}
        className={cn('gap-1.5 px-2.5', !chosen && 'border-dashed text-muted-foreground')}>
        {label}
        {chosen ? <>
          <Separator orientation="vertical" className="mx-0.5 h-4" />
          <Badge variant="secondary" className="max-w-40 rounded-sm px-1.5 font-normal"><span className="truncate">{chosen}</span></Badge>
        </> : null}
        <ChevronDown aria-hidden="true" className="opacity-50" />
      </Button>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="start" className="min-w-40">
      <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">{label}</DropdownMenuLabel>
      <DropdownMenuSeparator />
      <DropdownMenuRadioGroup value={current} onValueChange={(next) => onChange(next === unset ? undefined : next)}>
        {anyLabel !== undefined ? <DropdownMenuRadioItem value={ANY}>{anyLabel}</DropdownMenuRadioItem> : null}
        {options.map(([option, name]) => <DropdownMenuRadioItem key={option} value={option}>{name}</DropdownMenuRadioItem>)}
      </DropdownMenuRadioGroup>
    </DropdownMenuContent>
  </DropdownMenu>
}
