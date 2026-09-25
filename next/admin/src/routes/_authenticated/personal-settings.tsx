import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { PersonalAccountSettingsPage } from '@/features/settings/personal-account-settings-page'
import { SettingsNavigation } from '@/features/settings/settings-navigation'
import { useTranslation } from 'react-i18next'

export const Route = createFileRoute('/_authenticated/personal-settings')({ component: PersonalSettingsRoute })

function PersonalSettingsRoute() {
  const { api } = Route.useRouteContext()
  const { user } = useLoaderData({ from: '/_authenticated' })
  const { t } = useTranslation('personalSettings')
  const sectionIds = ['personal-profile-title', 'personal-appearance-title', 'personal-security-title'] as const
  return <div className="grid w-full max-w-[80rem] gap-8 xl:grid-cols-[minmax(0,1fr)_9rem]">
    <PersonalAccountSettingsPage api={api} userID={user.id} />
    <SettingsNavigation sectionIds={sectionIds} labels={[t('profile.title'), t('appearance.title'), t('security.title')]} />
  </div>
}
