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
import { ThemeModeToggle } from "@/features/preferences/ThemeModeToggle";

const navItems = [
  { href: "/release", label: "Release console", icon: TerminalSquare },
  { href: "/jobs", label: "Job history", icon: FileClock },
  { href: "/admin/users", label: "User management", icon: Users, admin: true },
  {
    href: "/admin/credentials",
    label: "Credential management",
    icon: KeyRound,
    admin: true,
  },
  {
    href: "/admin/imports",
    label: "Switch/network imports",
    icon: Network,
    admin: true,
  },
  {
    href: "/admin/profiles",
    label: "Command profiles",
    icon: TerminalSquare,
    admin: true,
  },
  {
    href: "/admin/audit",
    label: "Audit logs",
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
  const visibleNav = navItems.filter((item) => !item.admin || isAdmin);

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton className="h-12" size="lg" tooltip="Bind Plane">
              <div className="flex aspect-square size-8 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground">
                <Shield size={18} />
              </div>
              <div className="grid flex-1 text-left text-sm leading-tight">
                <span className="truncate font-semibold">Bind Plane</span>
                <span className="truncate text-xs">IPv4 release ops</span>
              </div>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarSeparator />
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel>Operations</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              {visibleNav.map((item) => {
                const Icon = item.icon;
                return (
                  <SidebarMenuItem key={item.href}>
                    <SidebarMenuButton
                      asChild
                      isActive={isActivePath(pathname, item.href)}
                      tooltip={item.label}
                    >
                      <NavLink to={item.href}>
                        <Icon />
                        <span>{item.label}</span>
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
              tooltip={userLabel ?? "Current user"}
            >
              <div className="flex aspect-square size-8 items-center justify-center rounded-lg bg-sidebar-accent text-sidebar-accent-foreground">
                <Shield size={16} />
              </div>
              <div className="grid flex-1 text-left text-sm leading-tight">
                <span className="truncate font-medium">
                  {userLabel ?? "Signed in"}
                </span>
                <span className="truncate text-xs text-sidebar-foreground/70">
                  {isAdmin ? "admin" : "operator"}
                </span>
              </div>
            </SidebarMenuButton>
          </SidebarMenuItem>
          <SidebarMenuItem>
            <SidebarMenuButton onClick={onSignOut} tooltip="Sign out">
              <LogOut />
              <span>Sign out</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}

export function AppShell() {
  const userQuery = useCurrentUser();
  const logout = useLogout();
  const navigate = useNavigate();
  const location = useLocation();
  const user = userQuery.data;
  const isAdmin = user?.roles.includes("admin") ?? false;
  const title =
    navItems.find((item) => isActivePath(location.pathname, item.href))
      ?.label ??
    (location.pathname.startsWith("/jobs/") ? "Job detail" : "Bind Plane");
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
            <h1 className="truncate text-base font-semibold">{title}</h1>
            <p className="text-xs text-muted-foreground">{userLabel}</p>
          </div>
          <ThemeModeToggle />
        </header>
        <div className="mx-auto max-w-7xl p-5">
          <Outlet />
        </div>
      </SidebarInset>
    </SidebarProvider>
  );
}
