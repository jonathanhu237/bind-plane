import { useEffect, useState } from 'react'
import type { User } from '@/shared/api/contracts'
import { cn } from '@/lib/utils'

export function UserAvatar({ user, className }: { user: Pick<User, 'name' | 'avatarUrl'> & { hasAvatar?: boolean }; className?: string }) {
  const [failed, setFailed] = useState(false)
  useEffect(() => { setFailed(false) }, [user.avatarUrl, user.hasAvatar])
  const showImage = Boolean(user.avatarUrl && user.hasAvatar && !failed)
  return showImage ? (
    <img src={user.avatarUrl} alt="" aria-hidden="true" className={cn('shrink-0 object-cover', className)} onError={() => setFailed(true)} />
  ) : (
    <span aria-hidden="true" className={cn('flex shrink-0 items-center justify-center bg-sidebar-accent text-xs font-semibold text-sidebar-accent-foreground', className)}>
      {initialFor(user.name)}
    </span>
  )
}

function initialFor(name: string): string {
  const value = name.trim()
  if (!value) return ''
  try {
    if (typeof Intl.Segmenter === 'function') {
      const first = new Intl.Segmenter(undefined, { granularity: 'grapheme' }).segment(value)[Symbol.iterator]().next().value
      if (first && typeof first === 'object' && 'segment' in first) return first.segment.toUpperCase()
    }
  } catch {
    // Fall back to a complete Unicode code point on older browsers.
  }
  return Array.from(value)[0]?.toUpperCase() ?? ''
}
