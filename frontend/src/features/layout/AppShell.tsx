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
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { cn } from "@/lib/utils";

const navItems = [
  { href: "/release", label: "Release console", icon: TerminalSquare },
  { href: "/jobs", label: "Job history", icon: FileClock },
  { href: "/admin/users", label: "User management", icon: Users, admin: true },
  { href: "/admin/credentials", label: "Credential management", icon: KeyRound, admin: true },
  { href: "/admin/imports", label: "Switch/network imports", icon: Network, admin: true },
  { href: "/admin/profiles", label: "Command profiles", icon: TerminalSquare, admin: true },
  { href: "/admin/audit", label: "Audit logs", icon: ClipboardList, admin: true },
];

export function AppShell() {
  const userQuery = useCurrentUser();
  const logout = useLogout();
  const navigate = useNavigate();
  const location = useLocation();
  const user = userQuery.data;
  const isAdmin = user?.roles.includes("admin") ?? false;
  const visibleNav = navItems.filter((item) => !item.admin || isAdmin);
  const title =
    visibleNav.find((item) => location.pathname === item.href)?.label ??
    (location.pathname.startsWith("/jobs/") ? "Job detail" : "Bind Plane");

  function signOut() {
    logout();
    navigate("/login", { replace: true });
  }

  return (
    <main className="grid min-h-screen bg-background lg:grid-cols-[250px_1fr]">
      <aside className="border-r bg-card">
        <div className="flex h-16 items-center gap-3 px-5">
          <div className="flex h-9 w-9 items-center justify-center rounded-md bg-primary text-primary-foreground">
            <Shield size={20} />
          </div>
          <div>
            <div className="text-sm font-semibold">Bind Plane</div>
            <div className="text-xs text-muted-foreground">IPv4 release ops</div>
          </div>
        </div>
        <Separator />
        <nav className="grid gap-1 p-3">
          {visibleNav.map((item) => {
            const Icon = item.icon;
            return (
              <NavLink
                key={item.href}
                className={({ isActive }) =>
                  cn(
                    "flex items-center gap-2 rounded-md px-3 py-2 text-sm text-muted-foreground hover:bg-accent hover:text-accent-foreground",
                    isActive && "bg-accent text-accent-foreground",
                  )
                }
                to={item.href}
              >
                <Icon size={17} />
                <span>{item.label}</span>
              </NavLink>
            );
          })}
        </nav>
      </aside>
      <section className="min-w-0">
        <header className="flex h-16 items-center justify-between border-b bg-card px-5">
          <div>
            <h1 className="text-lg font-semibold">{title}</h1>
            <p className="text-xs text-muted-foreground">{user?.display_name || user?.username}</p>
          </div>
          <Button type="button" variant="secondary" onClick={signOut}>
            <LogOut size={16} />
            Sign out
          </Button>
        </header>
        <div className="mx-auto max-w-7xl p-5">
          <Outlet />
        </div>
      </section>
    </main>
  );
}
