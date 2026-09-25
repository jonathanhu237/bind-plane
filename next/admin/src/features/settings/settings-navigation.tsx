import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'

const defaultSectionIds = ['identity-settings-title', 'email-settings-title', 'retention-settings-title'] as const

type SettingsNavigationProps = {
  sectionIds?: readonly string[]
  labels?: readonly string[]
}

export function SettingsNavigation({ sectionIds = defaultSectionIds, labels }: SettingsNavigationProps = {}) {
  const { t } = useTranslation(['settings', 'operationLog', 'common'])
  const [active, setActive] = useState<string>(sectionIds[0] ?? '')

  useEffect(() => {
    let frame = 0
    const update = () => {
      cancelAnimationFrame(frame)
      frame = requestAnimationFrame(() => {
        const headings = sectionIds.map((id) => document.getElementById(id)).filter((heading) => heading !== null)
        if (!headings.length) return
        const atBottom = window.scrollY > 0 && window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - 2
        const current = atBottom ? headings.at(-1) : headings.filter((heading) => heading.getBoundingClientRect().top <= 120).at(-1) ?? headings[0]
        if (current) setActive(current.id)
      })
    }
    window.addEventListener('scroll', update, { passive: true })
    window.addEventListener('resize', update)
    const observer = new ResizeObserver(update)
    observer.observe(document.body)
    update()
    return () => {
      cancelAnimationFrame(frame)
      observer.disconnect()
      window.removeEventListener('scroll', update)
      window.removeEventListener('resize', update)
    }
  }, [])

  const navigationLabels = labels ?? [t('identity.title'), t('email.title'), t('operationLog:retentionTitle')]
  return <nav aria-label={t('onThisPage')} className="sticky top-6 hidden self-start pt-24 xl:block">
    <ul className="flex flex-col border-l border-border">
      {sectionIds.map((id, index) => <li key={id}>
        <a href={`#${id}`} aria-current={active === id ? 'location' : undefined} className={cn('-ml-px flex min-h-11 items-center border-l-2 px-4 py-2 text-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring', active === id ? 'border-primary font-medium text-foreground' : 'border-transparent text-muted-foreground hover:border-border hover:text-foreground')} onClick={(event) => {
          const heading = document.getElementById(id)
          if (!heading) return
          event.preventDefault()
          heading.focus({ preventScroll: true })
          heading.scrollIntoView({ behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'instant' : 'smooth', block: 'start' })
          setActive(id)
        }}>{navigationLabels[index]}</a>
      </li>)}
    </ul>
  </nav>
}
