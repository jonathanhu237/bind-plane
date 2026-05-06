import type { ListQueryParams } from "@/api/types";

export const queryKeys = {
  me: (token: string | null | undefined) => ["auth", "me", token ?? "anonymous"] as const,
  jobs: ["jobs"] as const,
  jobsList: (params: ListQueryParams) => ["jobs", "list", params] as const,
  job: (id: string | null | undefined) => ["jobs", id] as const,
  users: ["admin", "users"] as const,
  usersList: (params: ListQueryParams) => ["admin", "users", params] as const,
  credentials: ["admin", "credentials"] as const,
  credentialsList: (params: ListQueryParams) =>
    ["admin", "credentials", params] as const,
  commandProfiles: ["admin", "command-profiles"] as const,
  commandProfilesList: (params: ListQueryParams) =>
    ["admin", "command-profiles", params] as const,
  switches: ["admin", "switches"] as const,
  switchOptions: ["admin", "switches", "options"] as const,
  switchesList: (params: ListQueryParams) =>
    ["admin", "switches", params] as const,
  imports: ["admin", "imports"] as const,
  importsList: (params: ListQueryParams) =>
    ["admin", "imports", params] as const,
  audit: ["admin", "audit"] as const,
  auditList: (params: ListQueryParams) => ["admin", "audit", params] as const,
};
