import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { apiRequest } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import type {
  AuditLog,
  CommandProfile,
  Credential,
  ImportBatch,
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
      queryFn: () => apiRequest<UserRead[]>("/admin/users", token),
      enabled: Boolean(token),
    }),
    credentials: useQuery({
      queryKey: queryKeys.credentials,
      queryFn: () => apiRequest<Credential[]>("/admin/credentials", token),
      enabled: Boolean(token),
    }),
    profiles: useQuery({
      queryKey: queryKeys.commandProfiles,
      queryFn: () => apiRequest<CommandProfile[]>("/admin/command-profiles", token),
      enabled: Boolean(token),
    }),
    switches: useQuery({
      queryKey: queryKeys.switches,
      queryFn: () => apiRequest<SwitchRecord[]>("/admin/switches", token),
      enabled: Boolean(token),
    }),
    imports: useQuery({
      queryKey: queryKeys.imports,
      queryFn: () => apiRequest<ImportBatch[]>("/admin/imports", token),
      enabled: Boolean(token),
    }),
    audit: useQuery({
      queryKey: queryKeys.audit,
      queryFn: () => apiRequest<AuditLog[]>("/audit", token),
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
