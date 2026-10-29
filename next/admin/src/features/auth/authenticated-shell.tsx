import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Activity, BellRing, CalendarClock, Check, ChevronDown, ClipboardCheck, History, House, LayoutDashboard, LogOut, Mail, Monitor, Network, ScrollText, Server, Settings, ShieldCheck, UserCog, UserRound, Users, Wrench } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useLocation, useNavigate, Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { DropdownMenu, DropdownMenuContent, DropdownMenuGroup, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
  SidebarProvider,
  SidebarTrigger,
  useSidebar,
} from '@/components/ui/sidebar'
import { translateProblemWithFields } from '@/shared/api/problems'
import { notifyRequestError, notifySuccess } from '@/shared/feedback'
import type { ApiClient } from '@/shared/api/client'
import { currentUserQueryKey } from './queries'
import type { User } from '@/shared/api/contracts'
import { clearAccessDrafts, useAccessDraftStore } from '@/features/access/drafts'
import { IdentityMark } from '@/features/identity/system-identity'
import { UserAvatar } from './user-avatar'
import { changeAccountLocale, restoreGuestLocale } from '@/shared/i18n'

function AccountMenu({ user, isPersonalSettings, logoutPending, onLogout }: { user: User; isPersonalSettings: boolean; logoutPending: boolean; onLogout: () => void }) {
  const { setOpenMobile } = useSidebar()
  const { t } = useTranslation(['common', 'auth'])

  return <DropdownMenu modal={false}>
    <DropdownMenuTrigger asChild>
      <SidebarMenuButton size="lg" className="data-[state=open]:bg-sidebar-accent" aria-label={`${user.name}, ${t('menu')}`}>
        <UserAvatar user={user} className="size-8 rounded-md" />
        <span className="flex min-w-0 flex-1 flex-col items-start gap-0.5 text-left group-data-[collapsible=icon]:hidden">
          <span className="w-full truncate text-sm font-medium">{user.name}</span>
          <span className="w-full truncate text-xs text-muted-foreground">{user.email}</span>
        </span>
      </SidebarMenuButton>
    </DropdownMenuTrigger>
    <DropdownMenuContent side="top" align="start" className="w-64">
      <DropdownMenuLabel className="font-normal">
        <p className="truncate text-sm font-medium">{user.name}</p>
        <p className="truncate text-xs text-muted-foreground">{user.email}</p>
      </DropdownMenuLabel>
      <DropdownMenuSeparator />
      <DropdownMenuGroup>
        <DropdownMenuItem
          asChild
          data-active={isPersonalSettings ? 'true' : undefined}
          onSelect={() => setOpenMobile(false)}
        >
          <Link to="/personal-settings" aria-current={isPersonalSettings ? 'page' : undefined}>
            <UserCog aria-hidden="true" data-icon="inline-start" />
            {t('personalSettings')}
            {isPersonalSettings ? <Check aria-hidden="true" className="ml-auto" /> : null}
          </Link>
        </DropdownMenuItem>
      </DropdownMenuGroup>
      <DropdownMenuSeparator />
      <DropdownMenuGroup>
        <DropdownMenuItem disabled={logoutPending} onSelect={(event) => { event.preventDefault(); onLogout() }}>
          <LogOut aria-hidden="true" data-icon="inline-start" />
          {logoutPending ? t('auth:loggingOut') : t('logout')}
        </DropdownMenuItem>
      </DropdownMenuGroup>
    </DropdownMenuContent>
  </DropdownMenu>
}

export function AuthenticatedShell({ api, user: initialUser, children }: { api: ApiClient; user: User; children: React.ReactNode }) {
  const { t } = useTranslation(['common', 'auth', 'problems', 'access', 'operationLog', 'onlineUsers', 'emailTasks'])
  const queryClient = useQueryClient()
  // Subscribe to the cache entry populated by the authenticated route loader.
  // Mutations from personal settings can then update the account area without
  // requiring a full route reload; disabled fetching keeps the loader as the
  // single source of network reads for this shell.
  const currentUserQuery = useQuery({ queryKey: currentUserQueryKey, queryFn: ({ signal }) => api.me(signal), enabled: false })
  const user = currentUserQuery.data ?? initialUser
  const navigate = useNavigate()
  const location = useLocation()
  const hasOnlineUsersAccess = Boolean(user.superAdmin || user.permissions?.includes('online-users.read'))
  const hasUsersAccess = Boolean(user.superAdmin || user.permissions?.includes('users.read'))
  const hasInvitationsAccess = Boolean(user.superAdmin || user.permissions?.includes('invitations.read'))
  const hasRolesAccess = Boolean(user.superAdmin || user.permissions?.includes('roles.read'))
  const hasSettingsAccess = Boolean(user.superAdmin || user.permissions?.includes('settings.read'))
  const hasOperationLogsAccess = Boolean(user.superAdmin || user.permissions?.includes('operation-logs.read'))
  const hasMailTasksAccess = Boolean(user.superAdmin || user.permissions?.includes('mail-tasks.read'))
  const hasAccessMenu = hasUsersAccess || hasInvitationsAccess || hasRolesAccess
  const hasMonitoringMenu = hasOnlineUsersAccess || hasOperationLogsAccess || hasMailTasksAccess
  const accessMenuActive = location.pathname.startsWith('/users') || location.pathname.startsWith('/invitations') || location.pathname.startsWith('/roles')
  const monitoringMenuActive = location.pathname.startsWith('/online-users') || location.pathname.startsWith('/operation-logs') || location.pathname.startsWith('/email-tasks')
  const [accessMenuOpen, setAccessMenuOpen] = useState(true)
  const [monitoringMenuOpen, setMonitoringMenuOpen] = useState(true)
  const [opsMenuOpen, setOpsMenuOpen] = useState(true)
  const zhShell = user.locale === 'zh-CN'
  const isBindPlaneAdmin = Boolean(user.superAdmin || user.roles?.some((role) => role.name === 'Admin'))
  const opsPages = [
    { to: '/ops', icon: LayoutDashboard, label: zhShell ? '运维概览' : 'Overview' },
    { to: '/devices', icon: Server, label: zhShell ? '设备' : 'Devices' },
    { to: '/schedules', icon: CalendarClock, label: zhShell ? '计划任务' : 'Schedules' },
    { to: '/compliance', icon: ClipboardCheck, label: zhShell ? '合规检查' : 'Compliance' },
    { to: '/alerts', icon: BellRing, label: zhShell ? '告警' : 'Alerts' },
    { to: '/audit', icon: ScrollText, label: zhShell ? '审计日志' : 'Audit log' },
    { to: '/assets', icon: Network, label: zhShell ? '资产配置' : 'Asset settings' },
  ] as const
  const opsPage = opsPages.find((page) => location.pathname.startsWith(page.to))
  const opsMenuExpanded = opsMenuOpen || Boolean(opsPage)
  const accessMenuExpanded = accessMenuOpen || accessMenuActive
  const monitoringMenuExpanded = monitoringMenuOpen || monitoringMenuActive
  useEffect(() => {
    void changeAccountLocale(user.locale === 'zh-CN' ? 'zh-CN' : 'en')
  }, [user.id, user.locale])
  useEffect(() => {
    const previousOwnerID = useAccessDraftStore.getState().ownerID
    if (previousOwnerID && previousOwnerID !== user.id) {
      queryClient.removeQueries({ queryKey: ['access'] })
      queryClient.removeQueries({ queryKey: ['personal-profile', previousOwnerID] })
      queryClient.removeQueries({ queryKey: ['operational-warnings', previousOwnerID] })
      queryClient.removeQueries({ queryKey: ['operation-log-status', previousOwnerID] })
      queryClient.removeQueries({ queryKey: ['operation-logs', previousOwnerID] })
      queryClient.removeQueries({ queryKey: ['operation-log', previousOwnerID] })
      queryClient.removeQueries({ queryKey: ['email-tasks', previousOwnerID] })
      queryClient.removeQueries({ queryKey: ['settings'] })
    }
    useAccessDraftStore.getState().setOwner(user.id)
  }, [queryClient, user.id])
  const logout = useMutation({
    retry: false,
    meta: { preserveAccessDraftsOnError: true },
    mutationFn: () => api.logout(),
    onSuccess: () => {
      const ownerID = useAccessDraftStore.getState().ownerID
      clearAccessDrafts()
      queryClient.removeQueries({ queryKey: currentUserQueryKey })
      queryClient.removeQueries({ queryKey: ['access'] })
      queryClient.removeQueries({ queryKey: ['settings'] })
      if (ownerID) {
        queryClient.removeQueries({ queryKey: ['personal-profile', ownerID] })
        queryClient.removeQueries({ queryKey: ['operational-warnings', ownerID] })
        queryClient.removeQueries({ queryKey: ['operation-log-status', ownerID] })
        queryClient.removeQueries({ queryKey: ['operation-logs', ownerID] })
        queryClient.removeQueries({ queryKey: ['operation-log', ownerID] })
        queryClient.removeQueries({ queryKey: ['email-tasks', ownerID] })
      }
      void restoreGuestLocale()
      notifySuccess(t('auth:logoutSuccess'))
      void navigate({ to: '/login', replace: true })
    },
    onError: (error) => notifyRequestError(error, t, { title: t('auth:logoutFailedTitle'), description: translateProblemWithFields(error, t) }),
  })

  return (
    <SidebarProvider defaultOpen>
      <Sidebar variant="inset" collapsible="icon" role="navigation" aria-label={t('menu')}>
        <SidebarContent>
          <div className="px-3 py-4"><IdentityMark api={api} compact /></div>
          <SidebarGroup>
            <SidebarGroupLabel>{t('menu')}</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                <SidebarMenuItem>
                  <SidebarMenuButton asChild isActive={location.pathname === '/'} tooltip={user.locale === 'zh-CN' ? '释放控制台' : 'Release console'}>
                    <Link to="/" aria-current={location.pathname === '/' ? 'page' : undefined}>
                      <House aria-hidden="true" data-icon="inline-start" />
                      <span>{user.locale === 'zh-CN' ? '释放控制台' : 'Release console'}</span>
                    </Link>
                  </SidebarMenuButton>
                </SidebarMenuItem>
                {isBindPlaneAdmin ? (
                  <SidebarMenuItem>
                    <SidebarMenuButton type="button" isActive={Boolean(opsPage)} tooltip={zhShell ? '设备运维' : 'Device operations'} aria-expanded={opsMenuExpanded} onClick={() => setOpsMenuOpen((open) => !open)}>
                      <Wrench aria-hidden="true" data-icon="inline-start" />
                      <span>{zhShell ? '设备运维' : 'Device operations'}</span>
                      <ChevronDown aria-hidden="true" className="ml-auto transition-transform data-[open=false]:-rotate-90 group-data-[collapsible=icon]:hidden" data-open={opsMenuExpanded} />
                    </SidebarMenuButton>
                    {opsMenuExpanded ? <SidebarMenuSub>
                      {opsPages.map((page) => <SidebarMenuSubItem key={page.to}><SidebarMenuSubButton asChild isActive={opsPage?.to === page.to}><Link to={page.to} aria-current={opsPage?.to === page.to ? 'page' : undefined}><page.icon aria-hidden="true" /><span>{page.label}</span></Link></SidebarMenuSubButton></SidebarMenuSubItem>)}
                    </SidebarMenuSub> : null}
                  </SidebarMenuItem>
                ) : null}
                {hasAccessMenu ? (
                  <SidebarMenuItem>
                    <SidebarMenuButton type="button" isActive={accessMenuActive} tooltip={t('access:usersAccess')} aria-expanded={accessMenuExpanded} onClick={() => setAccessMenuOpen((open) => !open)}>
                      <Users aria-hidden="true" data-icon="inline-start" />
                      <span>{t('access:usersAccess')}</span>
                      <ChevronDown aria-hidden="true" className="ml-auto transition-transform data-[open=false]:-rotate-90 group-data-[collapsible=icon]:hidden" data-open={accessMenuExpanded} />
                    </SidebarMenuButton>
                    {accessMenuExpanded ? <SidebarMenuSub>
                      {hasUsersAccess ? <SidebarMenuSubItem><SidebarMenuSubButton asChild isActive={location.pathname.startsWith('/users')}><Link to="/users" aria-current={location.pathname.startsWith('/users') ? 'page' : undefined}><UserRound aria-hidden="true" /><span>{t('access:users')}</span></Link></SidebarMenuSubButton></SidebarMenuSubItem> : null}
                      {hasInvitationsAccess ? <SidebarMenuSubItem><SidebarMenuSubButton asChild isActive={location.pathname.startsWith('/invitations')}><Link to="/invitations" aria-current={location.pathname.startsWith('/invitations') ? 'page' : undefined}><Mail aria-hidden="true" /><span>{t('access:invitations')}</span></Link></SidebarMenuSubButton></SidebarMenuSubItem> : null}
                      {hasRolesAccess ? <SidebarMenuSubItem><SidebarMenuSubButton asChild isActive={location.pathname.startsWith('/roles')}><Link to="/roles" aria-current={location.pathname.startsWith('/roles') ? 'page' : undefined}><ShieldCheck aria-hidden="true" /><span>{t('access:roles')}</span></Link></SidebarMenuSubButton></SidebarMenuSubItem> : null}
                    </SidebarMenuSub> : null}
                  </SidebarMenuItem>
                ) : null}
                {hasMonitoringMenu ? (
                  <SidebarMenuItem>
                    <SidebarMenuButton type="button" isActive={monitoringMenuActive} tooltip={t('systemMonitoring')} aria-expanded={monitoringMenuExpanded} onClick={() => setMonitoringMenuOpen((open) => !open)}>
                      <Monitor aria-hidden="true" data-icon="inline-start" />
                      <span>{t('systemMonitoring')}</span>
                      <ChevronDown aria-hidden="true" className="ml-auto transition-transform data-[open=false]:-rotate-90 group-data-[collapsible=icon]:hidden" data-open={monitoringMenuExpanded} />
                    </SidebarMenuButton>
                    {monitoringMenuExpanded ? <SidebarMenuSub>
                      {hasOnlineUsersAccess ? <SidebarMenuSubItem><SidebarMenuSubButton asChild isActive={location.pathname.startsWith('/online-users')}><Link to="/online-users" aria-current={location.pathname.startsWith('/online-users') ? 'page' : undefined}><Activity aria-hidden="true" /><span>{t('onlineUsers:title')}</span></Link></SidebarMenuSubButton></SidebarMenuSubItem> : null}
                      {hasOperationLogsAccess ? <SidebarMenuSubItem><SidebarMenuSubButton asChild isActive={location.pathname.startsWith('/operation-logs')}><Link to="/operation-logs" aria-current={location.pathname.startsWith('/operation-logs') ? 'page' : undefined}><History aria-hidden="true" /><span>{t('operationLog:title')}</span></Link></SidebarMenuSubButton></SidebarMenuSubItem> : null}
                      {hasMailTasksAccess ? <SidebarMenuSubItem><SidebarMenuSubButton asChild isActive={location.pathname.startsWith('/email-tasks')}><Link to="/email-tasks" aria-current={location.pathname.startsWith('/email-tasks') ? 'page' : undefined}><Mail aria-hidden="true" /><span>{t('emailTasks:title')}</span></Link></SidebarMenuSubButton></SidebarMenuSubItem> : null}
                    </SidebarMenuSub> : null}
                  </SidebarMenuItem>
                ) : null}
                {/* Keep system settings last; add new navigation items above it. */}
                {hasSettingsAccess ? <SidebarMenuItem><SidebarMenuButton asChild isActive={location.pathname.startsWith('/settings')} tooltip={t('settings')}><Link to="/settings" aria-current={location.pathname.startsWith('/settings') ? 'page' : undefined}><Settings aria-hidden="true" data-icon="inline-start" /><span>{t('settings')}</span></Link></SidebarMenuButton></SidebarMenuItem> : null}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        </SidebarContent>
        <SidebarFooter>
          <AccountMenu user={user} isPersonalSettings={location.pathname.startsWith('/personal-settings')} logoutPending={logout.isPending} onLogout={() => logout.mutate()} />
        </SidebarFooter>
      </Sidebar>
      <SidebarInset>
        <header className="flex h-14 shrink-0 items-center gap-2 border-b bg-background/95 px-4 backdrop-blur supports-[backdrop-filter]:bg-background/80">
          <div className="flex min-w-0 flex-1 items-center gap-2">
            <SidebarTrigger aria-label={t('menu')} />
            <div className="h-4 w-px bg-border" aria-hidden="true" />
            <p className="truncate text-sm font-medium text-muted-foreground">{location.pathname.startsWith('/personal-settings') ? t('personalSettings') : location.pathname.startsWith('/online-users') ? t('onlineUsers:title') : location.pathname.startsWith('/users') ? t('access:users') : location.pathname.startsWith('/invitations') ? t('access:invitations') : location.pathname.startsWith('/roles') ? t('access:roles') : location.pathname.startsWith('/settings') ? t('settings') : location.pathname.startsWith('/operation-logs') ? t('operationLog:title') : location.pathname.startsWith('/email-tasks') ? t('emailTasks:title') : opsPage ? opsPage.label : user.locale === 'zh-CN' ? '释放控制台' : 'Release console'}</p>
          </div>
        </header>
        <div className="flex min-h-[calc(100dvh-3.5rem)] flex-1 flex-col gap-5 p-4 sm:p-6 lg:p-8">
          {children}
        </div>
      </SidebarInset>
    </SidebarProvider>
  )
}
