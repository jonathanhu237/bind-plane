import { useEffect } from "react";
import { Navigate, Outlet, useLocation } from "react-router-dom";

import { useCurrentUser } from "@/api/hooks";
import { useAuthStore } from "@/stores/auth";

export function RequireAuth() {
  const location = useLocation();
  const token = useAuthStore((state) => state.token);
  const userQuery = useCurrentUser();
  const clearToken = useAuthStore((state) => state.clearToken);

  useEffect(() => {
    if (userQuery.isError) {
      clearToken();
    }
  }, [clearToken, userQuery.isError]);

  if (!token) {
    return <Navigate replace state={{ from: location }} to="/login" />;
  }

  if (userQuery.isLoading) {
    return (
      <main className="flex min-h-screen items-center justify-center text-sm text-muted-foreground">
        Loading session
      </main>
    );
  }

  if (userQuery.isError || !userQuery.data) {
    return <Navigate replace to="/login" />;
  }

  return <Outlet />;
}

export function RequireAdmin() {
  const userQuery = useCurrentUser();
  const isAdmin = userQuery.data?.roles.includes("admin") ?? false;
  if (!isAdmin) {
    return (
      <section className="rounded-lg border bg-card p-6">
        <h2 className="text-lg font-semibold">Access denied</h2>
        <p className="mt-2 text-sm text-muted-foreground">Admin role is required for this page.</p>
      </section>
    );
  }
  return <Outlet />;
}
