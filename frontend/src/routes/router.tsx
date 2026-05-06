import { Navigate, createBrowserRouter, type RouteObject } from "react-router-dom";

import {
  AuditLogsPage,
  CredentialsAdminPage,
  ImportsAdminPage,
  ProfilesAdminPage,
  UsersAdminPage,
} from "@/features/admin/AdminPages";
import { LoginPage } from "@/features/auth/LoginPage";
import { JobDetailPage } from "@/features/jobs/JobDetailPage";
import { JobHistoryPage } from "@/features/jobs/JobHistoryPage";
import { AppShell } from "@/features/layout/AppShell";
import { ReleaseConsole } from "@/features/release/ReleaseConsole";
import { RequireAdmin, RequireAuth } from "@/routes/guards";

export const routes: RouteObject[] = [
  {
    path: "/login",
    element: <LoginPage />,
  },
  {
    element: <RequireAuth />,
    children: [
      {
        element: <AppShell />,
        children: [
          { index: true, element: <Navigate replace to="/release" /> },
          { path: "/release", element: <ReleaseConsole /> },
          { path: "/jobs", element: <JobHistoryPage /> },
          { path: "/jobs/:jobId", element: <JobDetailPage /> },
          {
            path: "/admin",
            element: <RequireAdmin />,
            children: [
              { index: true, element: <Navigate replace to="/admin/users" /> },
              { path: "users", element: <UsersAdminPage /> },
              { path: "credentials", element: <CredentialsAdminPage /> },
              { path: "imports", element: <ImportsAdminPage /> },
              { path: "profiles", element: <ProfilesAdminPage /> },
              { path: "audit", element: <AuditLogsPage /> },
            ],
          },
        ],
      },
    ],
  },
  {
    path: "*",
    element: <Navigate replace to="/release" />,
  },
];

export function createAppRouter() {
  return createBrowserRouter(routes);
}
