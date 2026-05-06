import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Navigate, Outlet, useLocation } from "react-router-dom";

import { useCurrentUser } from "@/api/hooks";
import { useAuthStore } from "@/stores/auth";

export function RequireAuth() {
  const { t } = useTranslation();
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
        {t("guards.loadingSession")}
      </main>
    );
  }

  if (userQuery.isError || !userQuery.data) {
    return <Navigate replace to="/login" />;
  }

  return <Outlet />;
}

export function RequireAdmin() {
  const { t } = useTranslation();
  const userQuery = useCurrentUser();
  const isAdmin = userQuery.data?.roles.includes("admin") ?? false;
  if (!isAdmin) {
    return (
      <section className="rounded-lg border bg-card p-6">
        <h2 className="text-lg font-semibold">{t("guards.accessDenied")}</h2>
        <p className="mt-2 text-sm text-muted-foreground">
          {t("guards.adminRequired")}
        </p>
      </section>
    );
  }
  return <Outlet />;
}
