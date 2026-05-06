export type RoleName = "operator" | "admin";

export type ReleaseReason =
  | "temporary_test"
  | "user_report"
  | "ip_mac_change"
  | "wrong_binding_fix"
  | "security_response"
  | "other";

export type UserRead = {
  id: string;
  username: string;
  display_name: string | null;
  is_active: boolean;
  must_change_password: boolean;
  roles: RoleName[];
};

export type ReleasePreparation = {
  preparation_job_id: string | null;
  status:
    | "query_queued"
    | "ready"
    | "stopped_no_record"
    | "stopped_no_switch"
    | "stopped_ambiguous_switch"
    | "needs_manual_confirmation"
    | "failed"
    | "timeout";
  target_ip: string;
  resolved_switch: {
    switch_id: string;
    network_id: string | null;
    command_profile_id: string;
    management_ip: string;
    name: string;
    cidr: string | null;
    prefix_length: number | null;
    selection_source?: "resolved_network" | "selected_switch";
  } | null;
  observation: {
    entry_type: "static" | "dynamic" | "missing" | "unknown";
    mac: string | null;
    raw_output: string;
  } | null;
  force: boolean;
  reason: ReleaseReason | null;
};

export type ReleaseJob = {
  id: string;
  target_ip: string;
  kind: "pre_release_query" | "release";
  reason: ReleaseReason;
  ticket_id: string | null;
  force: boolean;
  status: string;
  phase: string;
  before_state: Record<string, unknown>;
  after_state: Record<string, unknown>;
  result: Record<string, unknown>;
  error_message: string | null;
  operator: { id: string; username: string; display_name: string | null };
  switch: { id: string; name: string; management_ip: string };
  retry_of_id: string | null;
  preparation_job_id: string | null;
  raw_output: { before: string | null; release: string | null; after: string | null } | null;
  events: { id: string; phase: string; status: string; message: string | null; created_at: string }[];
  created_at: string;
  started_at: string | null;
  finished_at: string | null;
};

export type Credential = {
  id: string;
  name: string;
  username: string;
  description: string | null;
  is_active: boolean;
};

export type CommandProfile = {
  id: string;
  name: string;
  description: string | null;
  login_prompt_patterns: Record<string, unknown>;
  command_templates: Record<string, unknown>;
  prompt_patterns: Record<string, unknown>;
  pagination_rules: Record<string, unknown>;
  success_patterns: string[];
  parser_rules: Record<string, unknown>;
  error_patterns: string[];
  is_active: boolean;
};

export type SwitchRecord = {
  id: string;
  name: string;
  management_ip: string;
  is_enabled: boolean;
  networks: { id: string; cidr: string; is_validated: boolean }[];
};

export type ImportBatch = {
  id: string;
  status: string;
  source_filename: string | null;
  summary: Record<string, unknown>;
  issues: { id: string; row_number: number | null; severity: string; message: string }[];
  created_at: string;
};

export type AuditLog = {
  id: string;
  action: string;
  target_type: string;
  target_id: string | null;
  payload: Record<string, unknown>;
  created_at: string;
};

export const reasonLabels: Record<ReleaseReason, string> = {
  temporary_test: "Temporary test",
  user_report: "User report",
  ip_mac_change: "IP or MAC change",
  wrong_binding_fix: "Wrong binding fix",
  security_response: "Security response",
  other: "Other",
};

export const terminalStatuses = new Set([
  "succeeded",
  "failed",
  "timeout",
  "needs_manual_confirmation",
  "cancelled",
]);
