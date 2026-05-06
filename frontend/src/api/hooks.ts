import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { apiRequest } from "@/api/client";
import { withListParams } from "@/api/listParams";
import { queryKeys } from "@/api/queryKeys";
import type {
  AuditLog,
  CommandProfile,
  Credential,
  ImportBatch,
  PaginatedResponse,
  ReleaseJob,
  SwitchRecord,
  UserRead,
} from "@/api/types";
import { terminalStatuses } from "@/api/types";
import { useAuthStore } from "@/stores/auth";

export function useToken() {
  return useAuthStore((state) => state.token);
}

export function useCurrentUser() {
  const token = useToken();
  return useQuery({
    queryKey: queryKeys.me(token),
    queryFn: () => apiRequest<UserRead>("/auth/me", token),
    enabled: Boolean(token),
  });
}

export function useJob(jobId: string | null | undefined) {
  const token = useToken();
  return useQuery({
    queryKey: queryKeys.job(jobId),
    queryFn: () => apiRequest<ReleaseJob>(`/releases/jobs/${jobId}`, token),
    enabled: Boolean(token && jobId),
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status && !terminalStatuses.has(status) ? 2000 : false;
    },
  });
}

export function useAdminLists() {
  const token = useToken();
  return {
    users: useQuery({
      queryKey: queryKeys.users,
      queryFn: () =>
        apiRequest<PaginatedResponse<UserRead>>(
          withListParams("/admin/users", { pageSize: 200, sortBy: "username" }),
          token,
        ),
      enabled: Boolean(token),
    }),
    credentials: useQuery({
      queryKey: queryKeys.credentials,
      queryFn: () =>
        apiRequest<PaginatedResponse<Credential>>(
          withListParams("/admin/credentials", { pageSize: 200, sortBy: "name" }),
          token,
        ),
      enabled: Boolean(token),
    }),
    profiles: useQuery({
      queryKey: queryKeys.commandProfiles,
      queryFn: () =>
        apiRequest<PaginatedResponse<CommandProfile>>(
          withListParams("/admin/command-profiles", {
            pageSize: 200,
            sortBy: "name",
          }),
          token,
        ),
      enabled: Boolean(token),
    }),
    switches: useQuery({
      queryKey: queryKeys.switches,
      queryFn: () =>
        apiRequest<PaginatedResponse<SwitchRecord>>(
          withListParams("/admin/switches", { pageSize: 200, sortBy: "name" }),
          token,
        ),
      enabled: Boolean(token),
    }),
    imports: useQuery({
      queryKey: queryKeys.imports,
      queryFn: () =>
        apiRequest<PaginatedResponse<ImportBatch>>(
          withListParams("/admin/imports", {
            pageSize: 200,
            sortBy: "created_at",
            sortOrder: "desc",
          }),
          token,
        ),
      enabled: Boolean(token),
    }),
    audit: useQuery({
      queryKey: queryKeys.audit,
      queryFn: () =>
        apiRequest<PaginatedResponse<AuditLog>>(
          withListParams("/audit", {
            pageSize: 200,
            sortBy: "created_at",
            sortOrder: "desc",
          }),
          token,
        ),
      enabled: Boolean(token),
    }),
  };
}

export function useLogout() {
  const queryClient = useQueryClient();
  const clearToken = useAuthStore((state) => state.clearToken);
  return () => {
    clearToken();
    queryClient.clear();
  };
}

export function useApiMutation<TResponse, TVariables>(
  mutationFn: (variables: TVariables, token: string | null) => Promise<TResponse>,
) {
  const token = useToken();
  return useMutation({
    mutationFn: (variables: TVariables) => mutationFn(variables, token),
  });
}
