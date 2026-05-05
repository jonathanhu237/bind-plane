import {
  Activity,
  ClipboardList,
  Database,
  FileClock,
  KeyRound,
  ListRestart,
  LogOut,
  Network,
  Play,
  RefreshCcw,
  Shield,
  TerminalSquare,
  UserCog,
  Users,
} from "lucide-react";
import { type FormEvent, type ReactNode, useEffect, useMemo, useState } from "react";

import "./styles.css";

type RoleName = "operator" | "admin";
type ReleaseReason =
  | "temporary_test"
  | "user_report"
  | "ip_mac_change"
  | "wrong_binding_fix"
  | "security_response"
  | "other";

type UserRead = {
  id: string;
  username: string;
  display_name: string | null;
  is_active: boolean;
  must_change_password: boolean;
  roles: RoleName[];
};

type ReleasePreparation = {
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

type ReleaseJob = {
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

type Credential = {
  id: string;
  name: string;
  username: string;
  description: string | null;
  is_active: boolean;
};

type CommandProfile = {
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

type SwitchRecord = {
  id: string;
  name: string;
  management_ip: string;
  is_enabled: boolean;
  networks: { id: string; cidr: string; is_validated: boolean }[];
};

type ImportBatch = {
  id: string;
  status: string;
  source_filename: string | null;
  summary: Record<string, unknown>;
  issues: { id: string; row_number: number | null; severity: string; message: string }[];
  created_at: string;
};

type AuditLog = {
  id: string;
  action: string;
  target_type: string;
  target_id: string | null;
  payload: Record<string, unknown>;
  created_at: string;
};

type ViewKey =
  | "release"
  | "history"
  | "job"
  | "users"
  | "credentials"
  | "imports"
  | "profiles"
  | "audit";

const API_BASE = import.meta.env.VITE_API_BASE ?? "/api";

const reasonLabels: Record<ReleaseReason, string> = {
  temporary_test: "Temporary test",
  user_report: "User report",
  ip_mac_change: "IP or MAC change",
  wrong_binding_fix: "Wrong binding fix",
  security_response: "Security response",
  other: "Other",
};

const terminalStatuses = new Set([
  "succeeded",
  "failed",
  "timeout",
  "needs_manual_confirmation",
  "cancelled",
]);

async function apiRequest<T>(path: string, token: string | null, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set("Content-Type", "application/json");
  if (token) {
    headers.set("Authorization", `Bearer ${token}`);
  }
  const response = await fetch(`${API_BASE}${path}`, { ...init, headers });
  if (!response.ok) {
    const text = await response.text();
    let message = text;
    try {
      const parsed = JSON.parse(text) as { detail?: unknown };
      message = typeof parsed.detail === "string" ? parsed.detail : JSON.stringify(parsed.detail);
    } catch {
      message = text || response.statusText;
    }
    throw new Error(message);
  }
  return (await response.json()) as T;
}

function pretty(value: unknown): string {
  return JSON.stringify(value ?? {}, null, 2);
}

function formatDate(value: string | null | undefined): string {
  if (!value) {
    return "";
  }
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}

type CommandProfileFormState = {
  name: string;
  description: string;
  loginPromptPatterns: string;
  commandTemplates: string;
  promptPatterns: string;
  paginationRules: string;
  successPatterns: string;
  errorPatterns: string;
  parserRules: string;
  isActive: boolean;
};

type CommandProfilePayload = {
  name: string;
  description: string | null;
  login_prompt_patterns: Record<string, unknown>;
  command_templates: Record<string, unknown>;
  prompt_patterns: Record<string, unknown>;
  pagination_rules: Record<string, unknown>;
  success_patterns: string[];
  error_patterns: string[];
  parser_rules: Record<string, unknown>;
  is_active: boolean;
};

const defaultCommandProfileForm: CommandProfileFormState = {
  name: "",
  description: "",
  loginPromptPatterns: pretty({
    username_pattern: "Username:",
    password_pattern: "Password:",
  }),
  commandTemplates: pretty({
    single_arp_query: "display arp $ip",
    config: "system-view",
    arp_release: "undo arp static $ip",
    exit_config: "return",
  }),
  promptPatterns: pretty({
    connection_options: {
      device_type: "hp_comware_telnet",
    },
    query_expect_string: "[>#]",
    release_expect_string: "[>#]",
  }),
  paginationRules: pretty({
    disable_paging_command: "screen-length disable",
  }),
  successPatterns: pretty([]),
  errorPatterns: pretty(["Error", "Invalid"]),
  parserRules: pretty({
    arp_entry_regex: "(?P<ip>\\S+)\\s+(?P<mac>[0-9a-f-]+).*(?P<type>S|D)",
    static_type_values: ["S"],
    dynamic_type_values: ["D"],
    missing_patterns: ["not found", "No matching"],
  }),
  isActive: true,
};

function parseJsonObject(value: string, label: string): Record<string, unknown> {
  const parsed = JSON.parse(value.trim() || "{}") as unknown;
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error(`${label} must be a JSON object`);
  }
  return parsed as Record<string, unknown>;
}

function parseStringArray(value: string, label: string): string[] {
  const parsed = JSON.parse(value.trim() || "[]") as unknown;
  if (!Array.isArray(parsed) || parsed.some((item) => typeof item !== "string")) {
    throw new Error(`${label} must be a JSON array of strings`);
  }
  return parsed;
}

function commandProfileToForm(profile: CommandProfile): CommandProfileFormState {
  return {
    name: profile.name,
    description: profile.description ?? "",
    loginPromptPatterns: pretty(profile.login_prompt_patterns),
    commandTemplates: pretty(profile.command_templates),
    promptPatterns: pretty(profile.prompt_patterns),
    paginationRules: pretty(profile.pagination_rules),
    successPatterns: pretty(profile.success_patterns),
    errorPatterns: pretty(profile.error_patterns),
    parserRules: pretty(profile.parser_rules),
    isActive: profile.is_active,
  };
}

function commandProfilePayloadFromForm(form: CommandProfileFormState): CommandProfilePayload {
  return {
    name: form.name,
    description: form.description || null,
    login_prompt_patterns: parseJsonObject(form.loginPromptPatterns, "Login prompt patterns"),
    command_templates: parseJsonObject(form.commandTemplates, "Command templates"),
    prompt_patterns: parseJsonObject(form.promptPatterns, "Prompt patterns"),
    pagination_rules: parseJsonObject(form.paginationRules, "Pagination rules"),
    success_patterns: parseStringArray(form.successPatterns, "Success patterns"),
    error_patterns: parseStringArray(form.errorPatterns, "Error patterns"),
    parser_rules: parseJsonObject(form.parserRules, "Parser rules"),
    is_active: form.isActive,
  };
}

function StatusPill({ value }: { value: string }) {
  return <span className={`pill pill-${value}`}>{value.replace(/_/g, " ")}</span>;
}

function EmptyState({ label }: { label: string }) {
  return <div className="empty">{label}</div>;
}

function LoginScreen({ onLogin }: { onLogin: (token: string) => void }) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setLoading(true);
    setError(null);
    try {
      const response = await apiRequest<{ access_token: string }>("/auth/login", null, {
        method: "POST",
        body: JSON.stringify({ username, password }),
      });
      onLogin(response.access_token);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Login failed");
    } finally {
      setLoading(false);
    }
  }

  return (
    <main className="login-shell">
      <form className="login-panel" onSubmit={submit}>
        <div className="brand-row">
          <Shield size={28} />
          <h1>bind-plane</h1>
        </div>
        <label>
          Username
          <input value={username} onChange={(event) => setUsername(event.target.value)} />
        </label>
        <label>
          Password
          <input
            type="password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
          />
        </label>
        {error ? <div className="error-banner">{error}</div> : null}
        <button className="primary-button" type="submit" disabled={loading}>
          <KeyRound size={16} />
          {loading ? "Signing in" : "Sign in"}
        </button>
      </form>
    </main>
  );
}

function ReleaseConsole({
  token,
  user,
  openJob,
}: {
  token: string;
  user: UserRead;
  openJob: (jobId: string) => void;
}) {
  const [targetIp, setTargetIp] = useState("");
  const [reason, setReason] = useState<ReleaseReason>("temporary_test");
  const [ticketId, setTicketId] = useState("");
  const [force, setForce] = useState(false);
  const [selectedSwitchId, setSelectedSwitchId] = useState("");
  const [switches, setSwitches] = useState<SwitchRecord[]>([]);
  const [preparation, setPreparation] = useState<ReleasePreparation | null>(null);
  const [preparationSeed, setPreparationSeed] = useState<ReleasePreparation | null>(null);
  const [preparationJobId, setPreparationJobId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const isAdmin = user.roles.includes("admin");
  const hasPreparedJob = Boolean(preparation?.preparation_job_id);
  const canForceStoppedNoRecord =
    isAdmin &&
    preparation?.status === "stopped_no_record" &&
    preparation.observation?.entry_type === "missing" &&
    Boolean(preparation.preparation_job_id);
  const displayedForce = preparation?.force || canForceStoppedNoRecord;
  const showForcedSwitchSelector =
    isAdmin && force && !hasPreparedJob && (preparation?.status ?? null) !== "ready";
  const enabledSwitches = switches.filter((item) => item.is_enabled);

  useEffect(() => {
    if (!isAdmin) {
      return;
    }
    void apiRequest<SwitchRecord[]>("/admin/switches", token)
      .then(setSwitches)
      .catch(() => setSwitches([]));
  }, [isAdmin, token]);

  function preparationFromJob(job: ReleaseJob, seed: ReleasePreparation): ReleasePreparation {
    const beforeState = job.before_state as {
      entry_type?: "static" | "dynamic" | "missing" | "unknown";
      mac?: string | null;
    };
    const preparationStatus =
      typeof job.result.preparation_status === "string"
        ? (job.result.preparation_status as ReleasePreparation["status"])
        : null;
    const status =
      job.status === "waiting_confirmation"
        ? "ready"
        : job.status === "failed" || job.status === "timeout"
          ? job.status
        : job.status === "cancelled" && preparationStatus === "stopped_no_record"
          ? "stopped_no_record"
          : job.status === "needs_manual_confirmation" &&
              preparationStatus === "needs_manual_confirmation"
            ? "needs_manual_confirmation"
          : seed.status;

    return {
      ...seed,
      status,
      target_ip: job.target_ip,
      force: job.force,
      reason: job.reason,
      observation: beforeState.entry_type
        ? {
            entry_type: beforeState.entry_type,
            mac: beforeState.mac ?? null,
            raw_output: job.raw_output?.before ?? "",
          }
        : null,
    };
  }

  async function prepare(event: FormEvent) {
    event.preventDefault();
    setLoading(true);
    setError(null);
    setPreparation(null);
    setPreparationSeed(null);
    setPreparationJobId(null);
    try {
      const response = await apiRequest<ReleasePreparation>("/releases/prepare", token, {
        method: "POST",
        body: JSON.stringify({
          target_ip: targetIp,
          reason,
          force,
          selected_switch_id: force && selectedSwitchId ? selectedSwitchId : undefined,
        }),
      });
      setPreparation(response);
      setPreparationSeed(response);
      setPreparationJobId(response.preparation_job_id);
      setForce(response.force);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Preparation failed");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    if (!preparationJobId || !preparationSeed) {
      return;
    }
    let active = true;
    const seed = preparationSeed;

    async function load() {
      try {
        const job = await apiRequest<ReleaseJob>(`/releases/jobs/${preparationJobId}`, token);
        if (!active) {
          return;
        }
        const nextPreparation = preparationFromJob(job, seed);
        setPreparation(nextPreparation);
        setForce(nextPreparation.force);
        if (job.status === "waiting_confirmation" || terminalStatuses.has(job.status)) {
          setPreparationJobId(null);
        }
        if (job.status === "failed" || job.status === "timeout") {
          setError(job.error_message || "Pre-release query failed");
        }
      } catch (err) {
        if (active) {
          setError(err instanceof Error ? err.message : "Preparation polling failed");
          setPreparationJobId(null);
        }
      }
    }

    void load();
    const timer = window.setInterval(() => void load(), 3000);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [preparationJobId, preparationSeed, token]);

  async function createJob(forceOverride?: boolean) {
    if (!preparation?.preparation_job_id) {
      setError("Preparation job is required before confirmation");
      return;
    }
    const confirmedForce = forceOverride ?? preparation.force;
    setLoading(true);
    setError(null);
    try {
      const response = await apiRequest<{ job_id: string }>("/releases/jobs", token, {
        method: "POST",
        body: JSON.stringify({
          preparation_job_id: preparation.preparation_job_id,
          target_ip: preparation.target_ip,
          reason: preparation.reason ?? reason,
          ticket_id: ticketId || null,
          force: confirmedForce,
          confirmed: true,
        }),
      });
      openJob(response.job_id);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Job creation failed");
    } finally {
      setLoading(false);
    }
  }

  return (
    <section className="workspace-panel release-grid">
      <form className="tool-panel" onSubmit={prepare}>
        <div className="panel-heading">
          <TerminalSquare size={18} />
          <h2>Release console</h2>
        </div>
        <label>
          IPv4 address
          <input
            placeholder="10.44.132.254"
            value={targetIp}
            onChange={(event) => setTargetIp(event.target.value)}
          />
        </label>
        <label>
          Reason
          <select value={reason} onChange={(event) => setReason(event.target.value as ReleaseReason)}>
            {Object.entries(reasonLabels).map(([value, label]) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </select>
        </label>
        <label>
          Ticket ID
          <input value={ticketId} onChange={(event) => setTicketId(event.target.value)} />
        </label>
        {isAdmin ? (
          <label className="check-row">
            <input
              type="checkbox"
              checked={preparation?.force ?? force}
              disabled={hasPreparedJob || loading}
              onChange={(event) => setForce(event.target.checked)}
            />
            Force release
          </label>
        ) : null}
        {showForcedSwitchSelector ? (
          <label>
            Forced switch
            <select
              value={selectedSwitchId}
              onChange={(event) => setSelectedSwitchId(event.target.value)}
            >
              <option value="">Automatic resolution</option>
              {enabledSwitches.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.name} ({item.management_ip})
                </option>
              ))}
            </select>
          </label>
        ) : null}
        {error ? <div className="error-banner">{error}</div> : null}
        <button className="primary-button" disabled={loading} type="submit">
          <RefreshCcw size={16} />
          {loading ? "Preparing" : "Prepare"}
        </button>
      </form>

      <div className="tool-panel">
        <div className="panel-heading">
          <ClipboardList size={18} />
          <h2>Confirmation</h2>
        </div>
        {preparation ? (
          <div className="stack">
            <StatusPill value={preparation.status} />
            <dl className="summary-list">
              <div>
                <dt>Target</dt>
                <dd>{preparation.target_ip}</dd>
              </div>
              <div>
                <dt>Switch</dt>
                <dd>
                  {preparation.resolved_switch
                    ? `${preparation.resolved_switch.name} (${preparation.resolved_switch.management_ip})`
                    : "Not resolved"}
                </dd>
              </div>
              <div>
                <dt>Network</dt>
                <dd>{preparation.resolved_switch?.cidr ?? "None"}</dd>
              </div>
              <div>
                <dt>Current state</dt>
                <dd>
                  {preparation.observation
                    ? `${preparation.observation.entry_type}${
                        preparation.observation.mac ? `, ${preparation.observation.mac}` : ""
                      }`
                    : "Unknown"}
                </dd>
              </div>
              <div>
                <dt>Reason</dt>
                <dd>{reasonLabels[preparation.reason ?? reason]}</dd>
              </div>
              <div>
                <dt>Force</dt>
                <dd>{displayedForce ? "Yes" : "No"}</dd>
              </div>
            </dl>
            {preparation.observation ? (
              <details>
                <summary>Raw pre-query output</summary>
                <pre>{preparation.observation.raw_output}</pre>
              </details>
            ) : null}
            <button
              className="danger-button"
              disabled={loading || preparation.status !== "ready" || !preparation.preparation_job_id}
              type="button"
              onClick={() => void createJob()}
            >
              <Play size={16} />
              Create job
            </button>
            {canForceStoppedNoRecord ? (
              <button
                className="danger-button"
                disabled={loading}
                type="button"
                onClick={() => void createJob(true)}
              >
                <Play size={16} />
                Force release job
              </button>
            ) : null}
          </div>
        ) : (
          <EmptyState label="No preparation result" />
        )}
      </div>
    </section>
  );
}

function JobDetail({
  token,
  jobId,
  openJob,
}: {
  token: string;
  jobId: string | null;
  openJob: (jobId: string) => void;
}) {
  const [job, setJob] = useState<ReleaseJob | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!jobId) {
      return;
    }
    let active = true;
    async function load() {
      try {
        const response = await apiRequest<ReleaseJob>(`/releases/jobs/${jobId}`, token);
        if (active) {
          setJob(response);
          setError(null);
        }
      } catch (err) {
        if (active) {
          setError(err instanceof Error ? err.message : "Job load failed");
        }
      }
    }
    void load();
    const timer = window.setInterval(() => {
      if (!job || !terminalStatuses.has(job.status)) {
        void load();
      }
    }, 3000);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [jobId, job?.status, token]);

  async function retry() {
    if (!job) {
      return;
    }
    try {
      const response = await apiRequest<{ job_id: string }>(`/releases/jobs/${job.id}/retry`, token, {
        method: "POST",
        body: JSON.stringify({}),
      });
      setJob(null);
      openJob(response.job_id);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Retry failed");
    }
  }

  if (!jobId) {
    return <EmptyState label="No job selected" />;
  }

  return (
    <section className="workspace-panel">
      <div className="panel-heading">
        <Activity size={18} />
        <h2>Job detail</h2>
      </div>
      {error ? <div className="error-banner">{error}</div> : null}
      {job ? (
        <div className="stack">
          <div className="status-row">
            <StatusPill value={job.status} />
            <StatusPill value={job.phase} />
            {job.force ? <StatusPill value="force" /> : null}
          </div>
          <dl className="summary-list compact">
            <div>
              <dt>Target</dt>
              <dd>{job.target_ip}</dd>
            </div>
            <div>
              <dt>Switch</dt>
              <dd>
                {job.switch.name} ({job.switch.management_ip})
              </dd>
            </div>
            <div>
              <dt>Operator</dt>
              <dd>{job.operator.username}</dd>
            </div>
            <div>
              <dt>Reason</dt>
              <dd>{reasonLabels[job.reason]}</dd>
            </div>
            <div>
              <dt>Created</dt>
              <dd>{formatDate(job.created_at)}</dd>
            </div>
          </dl>
          {job.error_message ? <div className="error-banner">{job.error_message}</div> : null}
          <div className="result-grid">
            <div>
              <h3>Before</h3>
              <pre>{pretty(job.before_state)}</pre>
            </div>
            <div>
              <h3>After</h3>
              <pre>{pretty(job.after_state)}</pre>
            </div>
            <div>
              <h3>Result</h3>
              <pre>{pretty(job.result)}</pre>
            </div>
          </div>
          {job.raw_output ? (
            <details>
              <summary>Raw output</summary>
              <pre>{[job.raw_output.before, job.raw_output.release, job.raw_output.after].filter(Boolean).join("\n\n")}</pre>
            </details>
          ) : null}
          {job.kind === "release" &&
          ["failed", "timeout", "needs_manual_confirmation"].includes(job.status) ? (
            <button className="secondary-button" type="button" onClick={retry}>
              <ListRestart size={16} />
              Retry
            </button>
          ) : null}
        </div>
      ) : (
        <EmptyState label="Loading job" />
      )}
    </section>
  );
}

function JobHistory({
  token,
  openJob,
}: {
  token: string;
  openJob: (jobId: string) => void;
}) {
  const [jobs, setJobs] = useState<ReleaseJob[]>([]);
  const [error, setError] = useState<string | null>(null);

  async function load() {
    try {
      setJobs(await apiRequest<ReleaseJob[]>("/releases/jobs", token));
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Job history failed");
    }
  }

  useEffect(() => {
    void load();
  }, [token]);

  return (
    <section className="workspace-panel">
      <div className="panel-heading between">
        <div>
          <FileClock size={18} />
          <h2>Job history</h2>
        </div>
        <button className="icon-button" type="button" onClick={load} title="Refresh">
          <RefreshCcw size={16} />
        </button>
      </div>
      {error ? <div className="error-banner">{error}</div> : null}
      {jobs.length ? (
        <table>
          <thead>
            <tr>
              <th>Target</th>
              <th>Switch</th>
              <th>Status</th>
              <th>Created</th>
            </tr>
          </thead>
          <tbody>
            {jobs.map((job) => (
              <tr key={job.id} onClick={() => openJob(job.id)}>
                <td>{job.target_ip}</td>
                <td>{job.switch.name}</td>
                <td>
                  <StatusPill value={job.status} />
                </td>
                <td>{formatDate(job.created_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <EmptyState label="No jobs" />
      )}
    </section>
  );
}

function UsersAdmin({ token }: { token: string }) {
  const [users, setUsers] = useState<UserRead[]>([]);
  const [username, setUsername] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState<RoleName>("operator");
  const [resetPasswords, setResetPasswords] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);

  async function load() {
    setUsers(await apiRequest<UserRead[]>("/admin/users", token));
  }

  useEffect(() => {
    void load().catch((err) => setError(err instanceof Error ? err.message : "Load failed"));
  }, [token]);

  async function create(event: FormEvent) {
    event.preventDefault();
    try {
      await apiRequest<UserRead>("/admin/users", token, {
        method: "POST",
        body: JSON.stringify({
          username,
          display_name: displayName || null,
          password,
          roles: [role],
        }),
      });
      setUsername("");
      setDisplayName("");
      setPassword("");
      await load();
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Create user failed");
    }
  }

  async function resetPassword(username: string) {
    try {
      await apiRequest<UserRead>(`/admin/users/${username}/reset-password`, token, {
        method: "POST",
        body: JSON.stringify({ password: resetPasswords[username] ?? "" }),
      });
      setResetPasswords({ ...resetPasswords, [username]: "" });
      await load();
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Reset password failed");
    }
  }

  return (
    <AdminTablePanel
      title="User management"
      icon={<Users size={18} />}
      error={error}
      form={
        <form className="inline-form" onSubmit={create}>
          <input placeholder="Username" value={username} onChange={(event) => setUsername(event.target.value)} />
          <input
            placeholder="Display name"
            value={displayName}
            onChange={(event) => setDisplayName(event.target.value)}
          />
          <input
            placeholder="Initial password"
            type="password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
          />
          <select value={role} onChange={(event) => setRole(event.target.value as RoleName)}>
            <option value="operator">operator</option>
            <option value="admin">admin</option>
          </select>
          <button className="primary-button" type="submit">
            <UserCog size={16} />
            Create
          </button>
        </form>
      }
    >
      <table>
        <thead>
          <tr>
            <th>Username</th>
            <th>Roles</th>
            <th>State</th>
            <th>Reset password</th>
          </tr>
        </thead>
        <tbody>
          {users.map((item) => (
            <tr key={item.id}>
              <td>{item.username}</td>
              <td>{item.roles.join(", ")}</td>
              <td>{item.is_active ? "active" : "inactive"}</td>
              <td>
                <div className="inline-form compact-actions">
                  <input
                    aria-label={`New password for ${item.username}`}
                    placeholder="New password"
                    type="password"
                    value={resetPasswords[item.username] ?? ""}
                    onChange={(event) =>
                      setResetPasswords({
                        ...resetPasswords,
                        [item.username]: event.target.value,
                      })
                    }
                  />
                  <button
                    className="secondary-button"
                    type="button"
                    onClick={() => void resetPassword(item.username)}
                  >
                    Reset
                  </button>
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </AdminTablePanel>
  );
}

function CredentialsAdmin({ token }: { token: string }) {
  const [items, setItems] = useState<Credential[]>([]);
  const [form, setForm] = useState({ name: "", username: "", password: "", secret: "" });
  const [error, setError] = useState<string | null>(null);

  async function load() {
    setItems(await apiRequest<Credential[]>("/admin/credentials", token));
  }

  useEffect(() => {
    void load().catch((err) => setError(err instanceof Error ? err.message : "Load failed"));
  }, [token]);

  async function create(event: FormEvent) {
    event.preventDefault();
    try {
      await apiRequest<Credential>("/admin/credentials", token, {
        method: "POST",
        body: JSON.stringify({ ...form, secret: form.secret || null }),
      });
      setForm({ name: "", username: "", password: "", secret: "" });
      await load();
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Create credential failed");
    }
  }

  return (
    <AdminTablePanel
      title="Credential management"
      icon={<KeyRound size={18} />}
      error={error}
      form={
        <form className="inline-form" onSubmit={create}>
          <input placeholder="Name" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} />
          <input
            placeholder="Username"
            value={form.username}
            onChange={(event) => setForm({ ...form, username: event.target.value })}
          />
          <input
            placeholder="Password"
            type="password"
            value={form.password}
            onChange={(event) => setForm({ ...form, password: event.target.value })}
          />
          <input
            placeholder="Enable secret"
            type="password"
            value={form.secret}
            onChange={(event) => setForm({ ...form, secret: event.target.value })}
          />
          <button className="primary-button" type="submit">
            <KeyRound size={16} />
            Save
          </button>
        </form>
      }
    >
      <table>
        <thead>
          <tr>
            <th>Name</th>
            <th>Username</th>
            <th>State</th>
          </tr>
        </thead>
        <tbody>
          {items.map((item) => (
            <tr key={item.id}>
              <td>{item.name}</td>
              <td>{item.username}</td>
              <td>{item.is_active ? "active" : "inactive"}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </AdminTablePanel>
  );
}

function ProfilesAdmin({ token }: { token: string }) {
  const [items, setItems] = useState<CommandProfile[]>([]);
  const [form, setForm] = useState<CommandProfileFormState>(defaultCommandProfileForm);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  async function load() {
    setItems(await apiRequest<CommandProfile[]>("/admin/command-profiles", token));
  }

  useEffect(() => {
    void load().catch((err) => setError(err instanceof Error ? err.message : "Load failed"));
  }, [token]);

  function resetForm() {
    setForm(defaultCommandProfileForm);
    setEditingId(null);
  }

  function startEdit(profile: CommandProfile) {
    setForm(commandProfileToForm(profile));
    setEditingId(profile.id);
    setError(null);
  }

  async function save(event: FormEvent) {
    event.preventDefault();
    try {
      await apiRequest<CommandProfile>(
        editingId ? `/admin/command-profiles/${editingId}` : "/admin/command-profiles",
        token,
        {
          method: editingId ? "PATCH" : "POST",
          body: JSON.stringify(commandProfilePayloadFromForm(form)),
        },
      );
      resetForm();
      await load();
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Save profile failed");
    }
  }

  const formTitle = editingId ? "Edit command profile" : "Create command profile";

  return (
    <section className="workspace-panel">
      <div className="panel-heading">
        <TerminalSquare size={18} />
        <h2>Command profiles</h2>
      </div>
      {error ? <div className="error-banner">{error}</div> : null}
      <form className="json-form" onSubmit={save}>
        <h3>{formTitle}</h3>
        <input
          placeholder="Profile name"
          value={form.name}
          onChange={(event) => setForm({ ...form, name: event.target.value })}
        />
        <input
          placeholder="Description"
          value={form.description}
          onChange={(event) => setForm({ ...form, description: event.target.value })}
        />
        <label>
          Login prompt patterns
          <textarea
            value={form.loginPromptPatterns}
            onChange={(event) => setForm({ ...form, loginPromptPatterns: event.target.value })}
          />
        </label>
        <label>
          Command templates
          <textarea
            value={form.commandTemplates}
            onChange={(event) => setForm({ ...form, commandTemplates: event.target.value })}
          />
        </label>
        <label>
          Prompt patterns
          <textarea
            value={form.promptPatterns}
            onChange={(event) => setForm({ ...form, promptPatterns: event.target.value })}
          />
        </label>
        <label>
          Pagination rules
          <textarea
            value={form.paginationRules}
            onChange={(event) => setForm({ ...form, paginationRules: event.target.value })}
          />
        </label>
        <label>
          Parser rules
          <textarea
            value={form.parserRules}
            onChange={(event) => setForm({ ...form, parserRules: event.target.value })}
          />
        </label>
        <label>
          Error patterns
          <textarea
            value={form.errorPatterns}
            onChange={(event) => setForm({ ...form, errorPatterns: event.target.value })}
          />
        </label>
        <label>
          Success patterns
          <textarea
            value={form.successPatterns}
            onChange={(event) => setForm({ ...form, successPatterns: event.target.value })}
          />
        </label>
        <label className="check-row">
          <input
            type="checkbox"
            checked={form.isActive}
            onChange={(event) => setForm({ ...form, isActive: event.target.checked })}
          />
          Active
        </label>
        <div className="button-row">
          <button className="primary-button" type="submit">
            <TerminalSquare size={16} />
            {editingId ? "Save changes" : "Create"}
          </button>
          {editingId ? (
            <button className="secondary-button" type="button" onClick={resetForm}>
              Cancel edit
            </button>
          ) : null}
        </div>
      </form>
      <table>
        <thead>
          <tr>
            <th>Name</th>
            <th>Templates</th>
            <th>State</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          {items.map((item) => (
            <tr key={item.id}>
              <td>{item.name}</td>
              <td>{Object.keys(item.command_templates).join(", ")}</td>
              <td>{item.is_active ? "active" : "inactive"}</td>
              <td>
                <button className="secondary-button" type="button" onClick={() => startEdit(item)}>
                  Edit
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}

function ImportsAdmin({ token }: { token: string }) {
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [profiles, setProfiles] = useState<CommandProfile[]>([]);
  const [switches, setSwitches] = useState<SwitchRecord[]>([]);
  const [batches, setBatches] = useState<ImportBatch[]>([]);
  const [recordsJson, setRecordsJson] = useState("[]");
  const [error, setError] = useState<string | null>(null);

  async function load() {
    const [credentialRows, profileRows, switchRows, batchRows] = await Promise.all([
      apiRequest<Credential[]>("/admin/credentials", token),
      apiRequest<CommandProfile[]>("/admin/command-profiles", token),
      apiRequest<SwitchRecord[]>("/admin/switches", token),
      apiRequest<ImportBatch[]>("/admin/imports", token),
    ]);
    setCredentials(credentialRows);
    setProfiles(profileRows);
    setSwitches(switchRows);
    setBatches(batchRows);
  }

  useEffect(() => {
    void load().catch((err) => setError(err instanceof Error ? err.message : "Load failed"));
  }, [token]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    try {
      await apiRequest<ImportBatch>("/admin/imports/switch-networks", token, {
        method: "POST",
        body: JSON.stringify({ source_filename: "manual-json", records: JSON.parse(recordsJson) }),
      });
      await load();
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Import failed");
    }
  }

  const sample = useMemo(
    () =>
      JSON.stringify(
        [
          {
            switch_name: "edge-sw-01",
            management_ip: "10.0.0.10",
            cidr: "10.44.132.0/24",
            credential_id: credentials[0]?.id ?? "credential-uuid",
            command_profile_id: profiles[0]?.id ?? "profile-uuid",
            network_validated: true,
          },
        ],
        null,
        2,
      ),
    [credentials, profiles],
  );

  return (
    <section className="workspace-panel">
      <div className="panel-heading">
        <Network size={18} />
        <h2>Switch and network imports</h2>
      </div>
      {error ? <div className="error-banner">{error}</div> : null}
      <form className="json-form" onSubmit={submit}>
        <label>
          Import records JSON
          <textarea value={recordsJson} onChange={(event) => setRecordsJson(event.target.value)} placeholder={sample} />
        </label>
        <button className="primary-button" type="submit">
          <Database size={16} />
          Import
        </button>
      </form>
      <div className="two-column">
        <div>
          <h3>Switches</h3>
          {switches.length ? (
            <table>
              <tbody>
                {switches.map((item) => (
                  <tr key={item.id}>
                    <td>{item.name}</td>
                    <td>{item.management_ip}</td>
                    <td>{item.networks.map((network) => network.cidr).join(", ")}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <EmptyState label="No switches" />
          )}
        </div>
        <div>
          <h3>Import batches</h3>
          {batches.length ? (
            <table>
              <tbody>
                {batches.map((item) => (
                  <tr key={item.id}>
                    <td>{item.status}</td>
                    <td>{pretty(item.summary)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <EmptyState label="No imports" />
          )}
        </div>
      </div>
    </section>
  );
}

function AuditLogs({ token }: { token: string }) {
  const [logs, setLogs] = useState<AuditLog[]>([]);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    void apiRequest<AuditLog[]>("/audit", token)
      .then(setLogs)
      .catch((err) => setError(err instanceof Error ? err.message : "Audit load failed"));
  }, [token]);

  return (
    <section className="workspace-panel">
      <div className="panel-heading">
        <ClipboardList size={18} />
        <h2>Audit logs</h2>
      </div>
      {error ? <div className="error-banner">{error}</div> : null}
      {logs.length ? (
        <table>
          <thead>
            <tr>
              <th>Time</th>
              <th>Action</th>
              <th>Target</th>
              <th>Payload</th>
            </tr>
          </thead>
          <tbody>
            {logs.map((log) => (
              <tr key={log.id}>
                <td>{formatDate(log.created_at)}</td>
                <td>{log.action}</td>
                <td>{log.target_type}</td>
                <td>
                  <code>{pretty(log.payload)}</code>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <EmptyState label="No audit logs" />
      )}
    </section>
  );
}

function AdminTablePanel({
  title,
  icon,
  error,
  form,
  children,
}: {
  title: string;
  icon: ReactNode;
  error: string | null;
  form: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="workspace-panel">
      <div className="panel-heading">
        {icon}
        <h2>{title}</h2>
      </div>
      {error ? <div className="error-banner">{error}</div> : null}
      {form}
      {children}
    </section>
  );
}

function AppShell({
  token,
  user,
  onLogout,
}: {
  token: string;
  user: UserRead;
  onLogout: () => void;
}) {
  const [view, setView] = useState<ViewKey>("release");
  const [jobId, setJobId] = useState<string | null>(null);
  const isAdmin = user.roles.includes("admin");

  function openJob(id: string) {
    setJobId(id);
    setView("job");
  }

  type NavItem = { key: ViewKey; label: string; icon: ReactNode; admin?: boolean };
  const allNavItems: NavItem[] = [
    { key: "release", label: "Release console", icon: <TerminalSquare size={17} /> },
    { key: "history", label: "Job history", icon: <FileClock size={17} /> },
    { key: "users", label: "User management", icon: <Users size={17} />, admin: true },
    { key: "credentials", label: "Credential management", icon: <KeyRound size={17} />, admin: true },
    { key: "imports", label: "Switch/network imports", icon: <Network size={17} />, admin: true },
    { key: "profiles", label: "Command profiles", icon: <TerminalSquare size={17} />, admin: true },
    { key: "audit", label: "Audit logs", icon: <ClipboardList size={17} />, admin: true },
  ];
  const navItems = allNavItems.filter((item) => !item.admin || isAdmin);

  return (
    <main className="app-shell">
      <aside className="sidebar">
        <div className="brand-row">
          <Shield size={24} />
          <span>bind-plane</span>
        </div>
        <nav>
          {navItems.map((item) => (
            <button
              key={item.key}
              className={view === item.key ? "nav-button active" : "nav-button"}
              type="button"
              onClick={() => setView(item.key)}
            >
              {item.icon}
              <span>{item.label}</span>
            </button>
          ))}
        </nav>
      </aside>
      <section className="content">
        <header className="topbar">
          <div>
            <h1>{navItems.find((item) => item.key === view)?.label ?? "Job detail"}</h1>
            <p>{user.display_name || user.username}</p>
          </div>
          <button className="secondary-button" type="button" onClick={onLogout}>
            <LogOut size={16} />
            Sign out
          </button>
        </header>
        {view === "release" ? <ReleaseConsole token={token} user={user} openJob={openJob} /> : null}
        {view === "history" ? <JobHistory token={token} openJob={openJob} /> : null}
        {view === "job" ? <JobDetail token={token} jobId={jobId} openJob={openJob} /> : null}
        {view === "users" ? <UsersAdmin token={token} /> : null}
        {view === "credentials" ? <CredentialsAdmin token={token} /> : null}
        {view === "imports" ? <ImportsAdmin token={token} /> : null}
        {view === "profiles" ? <ProfilesAdmin token={token} /> : null}
        {view === "audit" ? <AuditLogs token={token} /> : null}
      </section>
    </main>
  );
}

export function App() {
  const [token, setToken] = useState(() => localStorage.getItem("bind-plane-token"));
  const [user, setUser] = useState<UserRead | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!token) {
      setUser(null);
      return;
    }
    void apiRequest<UserRead>("/auth/me", token)
      .then((response) => {
        setUser(response);
        setError(null);
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : "Session expired");
        localStorage.removeItem("bind-plane-token");
        setToken(null);
      });
  }, [token]);

  function login(nextToken: string) {
    localStorage.setItem("bind-plane-token", nextToken);
    setToken(nextToken);
  }

  function logout() {
    localStorage.removeItem("bind-plane-token");
    setToken(null);
    setUser(null);
  }

  if (!token) {
    return <LoginScreen onLogin={login} />;
  }

  if (!user) {
    return (
      <main className="login-shell">
        <div className="login-panel">
          <div className="brand-row">
            <Shield size={28} />
            <h1>bind-plane</h1>
          </div>
          {error ? <div className="error-banner">{error}</div> : <EmptyState label="Loading session" />}
        </div>
      </main>
    );
  }

  return <AppShell token={token} user={user} onLogout={logout} />;
}
