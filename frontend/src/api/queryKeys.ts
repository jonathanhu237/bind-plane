export const queryKeys = {
  me: (token: string | null | undefined) => ["auth", "me", token ?? "anonymous"] as const,
  jobs: ["jobs"] as const,
  job: (id: string | null | undefined) => ["jobs", id] as const,
  users: ["admin", "users"] as const,
  credentials: ["admin", "credentials"] as const,
  commandProfiles: ["admin", "command-profiles"] as const,
  switches: ["admin", "switches"] as const,
  imports: ["admin", "imports"] as const,
  audit: ["admin", "audit"] as const,
};
