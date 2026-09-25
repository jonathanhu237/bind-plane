import { Globe2, SunMoon, type LucideIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { cn } from '@/lib/utils'
import { changeLocale } from '@/shared/i18n'
import type { Locale } from '@/shared/i18n/resources'
import { useOptionalTheme, type Theme } from '@/shared/theme'

const preferenceButtonClassName = 'max-sm:size-11 max-sm:shrink-0'

function PreferenceMenu({
  ariaLabel,
  className,
  icon: Icon,
  onValueChange,
  options,
  value,
}: {
  ariaLabel: string
  className?: string
  icon: LucideIcon
  onValueChange: (value: string) => void
  options: Array<{ value: string; label: string }>
  value: string
}) {
  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          className={cn(preferenceButtonClassName, className)}
          aria-label={ariaLabel}
          title={ariaLabel}
        >
          <Icon aria-hidden="true" data-icon="inline-start" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-48">
        <DropdownMenuRadioGroup value={value} onValueChange={onValueChange}>
          {options.map((option) => (
            <DropdownMenuRadioItem key={option.value} value={option.value}>
              {option.label}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export function AppearanceMenu({ className }: { className?: string }) {
  const { t } = useTranslation('common')
  const { theme, setTheme } = useOptionalTheme()

  return (
    <PreferenceMenu
      ariaLabel={t('appearanceSettings')}
      className={className}
      icon={SunMoon}
      onValueChange={(value) => setTheme(value as Theme)}
      options={[
        { value: 'system', label: t('system') },
        { value: 'light', label: t('light') },
        { value: 'dark', label: t('dark') },
      ]}
      value={theme}
    />
  )
}

export function LanguageMenu({ className }: { className?: string }) {
  const { t, i18n } = useTranslation('common')
  const locale: Locale = i18n.language.toLowerCase().startsWith('zh') ? 'zh-CN' : 'en'

  return (
    <PreferenceMenu
      ariaLabel={t('languageSettings')}
      className={className}
      icon={Globe2}
      onValueChange={(value) => void changeLocale(value as Locale)}
      options={[
        { value: 'zh-CN', label: t('chinese') },
        { value: 'en', label: t('english') },
      ]}
      value={locale}
    />
  )
}

export function PreferencesButtons({ className }: { className?: string }) {
  return (
    <div className={cn('flex items-center gap-3', className)}>
      <AppearanceMenu />
      <LanguageMenu />
    </div>
  )
}
