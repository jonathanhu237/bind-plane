import {
  ClipboardList,
  FileClock,
  KeyRound,
  LogOut,
  Network,
  Shield,
  TerminalSquare,
  Users,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { NavLink, Outlet, useLocation, useNavigate } from "react-router-dom";

import { useCurrentUser, useLogout } from "@/api/hooks";
import { Separator } from "@/components/ui/separator";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarRail,
  SidebarSeparator,
  SidebarTrigger,
} from "@/components/ui/sidebar";
import { LocaleSwitcher } from "@/features/preferences/LocaleSwitcher";
import { ThemeModeToggle } from "@/features/preferences/ThemeModeToggle";
import { roleLabel } from "@/i18n/labels";

const navItems = [
  { href: "/release", labelKey: "nav.release", icon: TerminalSquare },
  { href: "/jobs", labelKey: "nav.jobs", icon: FileClock },
  { href: "/admin/users", labelKey: "nav.users", icon: Users, admin: true },
  {
    href: "/admin/credentials",
    labelKey: "nav.credentials",
    icon: KeyRound,
    admin: true,
  },
  {
    href: "/admin/imports",
    labelKey: "nav.imports",
    icon: Network,
    admin: true,
  },
  {
    href: "/admin/profiles",
    labelKey: "nav.profiles",
    icon: TerminalSquare,
    admin: true,
  },
  {
    href: "/admin/audit",
    labelKey: "nav.audit",
    icon: ClipboardList,
    admin: true,
  },
];

type AppSidebarProps = {
  isAdmin: boolean;
  onSignOut: () => void;
  pathname: string;
  userLabel?: string;
};

function isActivePath(pathname: string, href: string) {
  if (href === "/jobs") {
    return pathname === href || pathname.startsWith("/jobs/");
  }
  return pathname === href;
}

function AppSidebar({
  isAdmin,
  onSignOut,
  pathname,
  userLabel,
}: AppSidebarProps) {
  const { t } = useTranslation();
  const visibleNav = navItems.filter((item) => !item.admin || isAdmin);

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              className="h-12"
              size="lg"
              tooltip={t("app.name")}
            >
              <div className="flex aspect-square size-8 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground">
                <Shield size={18} />
              </div>
              <div className="grid flex-1 text-left text-sm leading-tight">
                <span className="truncate font-semibold">{t("app.name")}</span>
                <span className="truncate text-xs">{t("app.tagline")}</span>
              </div>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarSeparator />
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel>{t("nav.operations")}</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              {visibleNav.map((item) => {
                const Icon = item.icon;
                return (
                  <SidebarMenuItem key={item.href}>
                    <SidebarMenuButton
                      asChild
                      isActive={isActivePath(pathname, item.href)}
                      tooltip={t(item.labelKey)}
                    >
                      <NavLink to={item.href}>
                        <Icon />
                        <span>{t(item.labelKey)}</span>
                      </NavLink>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                );
              })}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
      <SidebarFooter>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              className="h-12"
              size="lg"
              tooltip={userLabel ?? t("nav.currentUser")}
            >
              <div className="flex aspect-square size-8 items-center justify-center rounded-lg bg-sidebar-accent text-sidebar-accent-foreground">
                <Shield size={16} />
              </div>
              <div className="grid flex-1 text-left text-sm leading-tight">
                <span className="truncate font-medium">
                  {userLabel ?? t("nav.signedIn")}
                </span>
                <span className="truncate text-xs text-sidebar-foreground/70">
                  {roleLabel(t, isAdmin ? "admin" : "operator")}
                </span>
              </div>
            </SidebarMenuButton>
          </SidebarMenuItem>
          <SidebarMenuItem>
            <SidebarMenuButton onClick={onSignOut} tooltip={t("nav.signOut")}>
              <LogOut />
              <span>{t("nav.signOut")}</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}

export function AppShell() {
  const { t } = useTranslation();
  const userQuery = useCurrentUser();
  const logout = useLogout();
  const navigate = useNavigate();
  const location = useLocation();
  const user = userQuery.data;
  const isAdmin = user?.roles.includes("admin") ?? false;
  const title =
    navItems.find((item) => isActivePath(location.pathname, item.href))
      ?.labelKey ??
    (location.pathname.startsWith("/jobs/") ? "nav.jobDetail" : "app.name");
  const userLabel = user?.display_name || user?.username;

  function signOut() {
    logout();
    navigate("/login", { replace: true });
  }

  return (
    <SidebarProvider>
      <AppSidebar
        isAdmin={isAdmin}
        pathname={location.pathname}
        userLabel={userLabel}
        onSignOut={signOut}
      />
      <SidebarInset>
        <header className="sticky top-0 z-10 flex h-16 shrink-0 items-center gap-3 border-b bg-background/95 px-4 backdrop-blur supports-[backdrop-filter]:bg-background/60">
          <SidebarTrigger />
          <Separator className="h-4" orientation="vertical" />
          <div className="min-w-0 flex-1">
            <h1 className="truncate text-base font-semibold">{t(title)}</h1>
            <p className="text-xs text-muted-foreground">{userLabel}</p>
          </div>
          <LocaleSwitcher />
          <ThemeModeToggle />
        </header>
        <div className="mx-auto max-w-7xl p-5">
          <Outlet />
        </div>
      </SidebarInset>
    </SidebarProvider>
  );
}
