import "@testing-library/jest-dom/vitest";

import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { App } from "./App";

const operator = {
  id: "user-1",
  username: "operator",
  display_name: "Operator",
  is_active: true,
  must_change_password: false,
  roles: ["operator"],
};

const admin = {
  ...operator,
  username: "admin",
  display_name: "Admin",
  roles: ["admin"],
};

const commandProfile = {
  id: "profile-1",
  name: "h3c",
  description: "H3C Telnet profile",
  login_prompt_patterns: { username_pattern: "Username:", password_pattern: "Password:" },
  command_templates: {
    single_arp_query: "display arp $ip",
    arp_release: "undo arp static $ip",
  },
  prompt_patterns: {
    connection_options: { device_type: "hp_comware_telnet" },
    query_expect_string: "[>#]",
  },
  pagination_rules: { disable_paging_command: "screen-length disable" },
  success_patterns: ["Succeeded"],
  error_patterns: ["Error"],
  parser_rules: {
    arp_entry_regex: "(?P<ip>\\S+)\\s+(?P<mac>\\S+)",
    static_type_values: ["S"],
  },
  is_active: true,
};

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function releaseJob(overrides: Record<string, unknown> = {}) {
  return {
    id: "job-1",
    target_ip: "10.44.132.254",
    kind: "release",
    reason: "temporary_test",
    ticket_id: null,
    force: false,
    status: "failed",
    phase: "finished",
    before_state: { entry_type: "static", mac: "0011-2233-4455" },
    after_state: {},
    result: {},
    error_message: "Command failed",
    operator: { id: "user-1", username: "operator", display_name: "Operator" },
    switch: { id: "switch-1", name: "edge-sw-01", management_ip: "10.0.0.10" },
    retry_of_id: null,
    preparation_job_id: "job-prep",
    raw_output: {
      before: "10.44.132.254 0011-2233-4455 S",
      release: "Error",
      after: null,
    },
    events: [],
    created_at: "2026-05-04T00:00:00Z",
    updated_at: "2026-05-04T00:00:00Z",
    started_at: "2026-05-04T00:00:10Z",
    finished_at: "2026-05-04T00:01:00Z",
    ...overrides,
  };
}

describe("App", () => {
  beforeEach(() => {
    const values = new Map<string, string>();
    vi.stubGlobal("localStorage", {
      get length() {
        return values.size;
      },
      clear: () => values.clear(),
      getItem: (key: string) => values.get(key) ?? null,
      key: (index: number) => Array.from(values.keys())[index] ?? null,
      removeItem: (key: string) => values.delete(key),
      setItem: (key: string, value: string) => values.set(key, value),
    } satisfies Storage);
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("logs in and opens the release console shell", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.endsWith("/api/auth/login")) {
        return jsonResponse({ access_token: "token-1" });
      }
      if (url.endsWith("/api/auth/me")) {
        return jsonResponse(operator);
      }
      return jsonResponse({ detail: "not found" }, 404);
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<App />);
    fireEvent.change(screen.getByLabelText("Username"), { target: { value: "operator" } });
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "password123" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    expect(await screen.findAllByRole("heading", { name: "Release console" })).not.toHaveLength(0);
    expect(screen.getByLabelText("IPv4 address")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /audit logs/i })).not.toBeInTheDocument();
  });

  it("prepares a release and opens the queued job detail", async () => {
    localStorage.setItem("bind-plane-token", "token-1");
    let confirmed = false;
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/api/auth/me")) {
        return jsonResponse(operator);
      }
      if (url.endsWith("/api/releases/prepare")) {
        expect(init?.method).toBe("POST");
        return jsonResponse({
          preparation_job_id: "job-1",
          status: "query_queued",
          target_ip: "10.44.132.254",
          resolved_switch: {
            switch_id: "switch-1",
            network_id: "network-1",
            command_profile_id: "profile-1",
            management_ip: "10.0.0.10",
            name: "edge-sw-01",
            cidr: "10.44.132.0/24",
            prefix_length: 24,
          },
          observation: null,
          force: false,
          reason: "temporary_test",
        });
      }
      if (url.endsWith("/api/releases/jobs") && init?.method === "POST") {
        expect(JSON.parse(String(init.body))).toMatchObject({ preparation_job_id: "job-1" });
        confirmed = true;
        return jsonResponse({ job_id: "job-2", status: "queued", phase: "queued" });
      }
      if (url.endsWith("/api/releases/jobs/job-1")) {
        return jsonResponse({
          id: "job-1",
          target_ip: "10.44.132.254",
          kind: "pre_release_query",
          reason: "temporary_test",
          ticket_id: null,
          force: false,
          status: confirmed ? "queued" : "waiting_confirmation",
          phase: confirmed ? "queued" : "waiting_confirmation",
          before_state: { entry_type: "static", mac: "0011-2233-4455" },
          after_state: {},
          result: {},
          error_message: null,
          operator: { id: "user-1", username: "operator", display_name: "Operator" },
          switch: { id: "switch-1", name: "edge-sw-01", management_ip: "10.0.0.10" },
          retry_of_id: null,
          preparation_job_id: null,
          raw_output: {
            before: "10.44.132.254 0011-2233-4455 S",
            release: null,
            after: null,
          },
          events: [],
          created_at: "2026-05-04T00:00:00Z",
          updated_at: "2026-05-04T00:00:00Z",
          started_at: null,
          finished_at: null,
        });
      }
      if (url.endsWith("/api/releases/jobs/job-2")) {
        return jsonResponse({
          id: "job-2",
          target_ip: "10.44.132.254",
          kind: "release",
          reason: "temporary_test",
          ticket_id: null,
          force: false,
          status: "queued",
          phase: "queued",
          before_state: { entry_type: "static", mac: "0011-2233-4455" },
          after_state: {},
          result: {},
          error_message: null,
          operator: { id: "user-1", username: "operator", display_name: "Operator" },
          switch: { id: "switch-1", name: "edge-sw-01", management_ip: "10.0.0.10" },
          retry_of_id: null,
          preparation_job_id: "job-1",
          raw_output: {
            before: "10.44.132.254 0011-2233-4455 S",
            release: null,
            after: null,
          },
          events: [],
          created_at: "2026-05-04T00:00:00Z",
          updated_at: "2026-05-04T00:00:00Z",
          started_at: null,
          finished_at: null,
        });
      }
      return jsonResponse({ detail: "not found" }, 404);
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<App />);
    await screen.findByLabelText("IPv4 address");
    fireEvent.change(screen.getByLabelText("IPv4 address"), {
      target: { value: "10.44.132.254" },
    });
    fireEvent.click(screen.getByRole("button", { name: /prepare/i }));
    expect(await screen.findByText(/edge-sw-01/)).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByRole("button", { name: /create job/i })).not.toBeDisabled(),
    );
    const confirmationPanel = screen
      .getByRole("heading", { name: "Confirmation" })
      .closest(".tool-panel");
    expect(confirmationPanel).not.toBeNull();
    expect(within(confirmationPanel as HTMLElement).getByText("Temporary test")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Reason"), { target: { value: "other" } });
    expect(within(confirmationPanel as HTMLElement).getByText("Temporary test")).toBeInTheDocument();
    expect(within(confirmationPanel as HTMLElement).queryByText("Other")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /create job/i }));

    expect(await screen.findAllByRole("heading", { name: "Job detail" })).not.toHaveLength(0);
    await waitFor(() => expect(screen.getAllByText("10.44.132.254")).not.toHaveLength(0));
  });

  it("resets the admin force checkbox to the prepared payload", async () => {
    localStorage.setItem("bind-plane-token", "token-1");
    let confirmedForce: boolean | null = null;
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/api/auth/me")) {
        return jsonResponse(admin);
      }
      if (url.endsWith("/api/releases/prepare")) {
        expect(JSON.parse(String(init?.body))).toMatchObject({ force: true });
        return jsonResponse({
          preparation_job_id: "job-force-prep",
          status: "ready",
          target_ip: "10.44.132.254",
          resolved_switch: {
            switch_id: "switch-1",
            network_id: "network-1",
            command_profile_id: "profile-1",
            management_ip: "10.0.0.10",
            name: "edge-sw-01",
            cidr: "10.44.132.0/24",
            prefix_length: 24,
          },
          observation: {
            entry_type: "static",
            mac: "0011-2233-4455",
            raw_output: "10.44.132.254 0011-2233-4455 S",
          },
          force: false,
          reason: "temporary_test",
        });
      }
      if (url.endsWith("/api/releases/jobs/job-force-prep")) {
        return jsonResponse({
          id: "job-force-prep",
          target_ip: "10.44.132.254",
          kind: "pre_release_query",
          reason: "temporary_test",
          ticket_id: null,
          force: false,
          status: "waiting_confirmation",
          phase: "waiting_confirmation",
          before_state: { entry_type: "static", mac: "0011-2233-4455" },
          after_state: {},
          result: {},
          error_message: null,
          operator: { id: "user-1", username: "admin", display_name: "Admin" },
          switch: { id: "switch-1", name: "edge-sw-01", management_ip: "10.0.0.10" },
          retry_of_id: null,
          preparation_job_id: null,
          raw_output: {
            before: "10.44.132.254 0011-2233-4455 S",
            release: null,
            after: null,
          },
          events: [],
          created_at: "2026-05-04T00:00:00Z",
          updated_at: "2026-05-04T00:00:00Z",
          started_at: null,
          finished_at: null,
        });
      }
      if (url.endsWith("/api/releases/jobs") && init?.method === "POST") {
        const payload = JSON.parse(String(init.body));
        confirmedForce = payload.force;
        return jsonResponse({ job_id: "job-force-release", status: "queued", phase: "queued" });
      }
      if (url.endsWith("/api/releases/jobs/job-force-release")) {
        return jsonResponse({
          id: "job-force-release",
          target_ip: "10.44.132.254",
          kind: "release",
          reason: "temporary_test",
          ticket_id: null,
          force: false,
          status: "queued",
          phase: "queued",
          before_state: { entry_type: "static", mac: "0011-2233-4455" },
          after_state: {},
          result: {},
          error_message: null,
          operator: { id: "user-1", username: "admin", display_name: "Admin" },
          switch: { id: "switch-1", name: "edge-sw-01", management_ip: "10.0.0.10" },
          retry_of_id: null,
          preparation_job_id: "job-force-prep",
          raw_output: {
            before: "10.44.132.254 0011-2233-4455 S",
            release: null,
            after: null,
          },
          events: [],
          created_at: "2026-05-04T00:00:00Z",
          updated_at: "2026-05-04T00:00:00Z",
          started_at: null,
          finished_at: null,
        });
      }
      return jsonResponse({ detail: "not found" }, 404);
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<App />);
    await screen.findByLabelText("IPv4 address");
    fireEvent.change(screen.getByLabelText("IPv4 address"), {
      target: { value: "10.44.132.254" },
    });
    const forceCheckbox = screen.getByLabelText("Force release");
    fireEvent.click(forceCheckbox);
    expect(forceCheckbox).toBeChecked();
    fireEvent.click(screen.getByRole("button", { name: /prepare/i }));

    const confirmationPanel = await screen
      .findByRole("heading", { name: "Confirmation" })
      .then((heading) => heading.closest(".tool-panel"));
    expect(confirmationPanel).not.toBeNull();
    await waitFor(() => expect(forceCheckbox).not.toBeChecked());
    expect(forceCheckbox).toBeDisabled();
    expect(within(confirmationPanel as HTMLElement).getByText("Force")).toBeInTheDocument();
    expect(within(confirmationPanel as HTMLElement).getByText("No")).toBeInTheDocument();

    await waitFor(() =>
      expect(screen.getByRole("button", { name: /create job/i })).not.toBeDisabled(),
    );
    fireEvent.click(screen.getByRole("button", { name: /create job/i }));
    await screen.findAllByRole("heading", { name: "Job detail" });
    expect(confirmedForce).toBe(false);
  });

  it("allows an admin to force after a no-record pre-query", async () => {
    localStorage.setItem("bind-plane-token", "token-1");
    let confirmedForce: boolean | null = null;
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/api/auth/me")) {
        return jsonResponse(admin);
      }
      if (url.endsWith("/api/admin/switches")) {
        return jsonResponse([]);
      }
      if (url.endsWith("/api/releases/prepare")) {
        return jsonResponse({
          preparation_job_id: "job-missing-prep",
          status: "query_queued",
          target_ip: "10.44.132.254",
          resolved_switch: {
            switch_id: "switch-1",
            network_id: "network-1",
            command_profile_id: "profile-1",
            management_ip: "10.0.0.10",
            name: "edge-sw-01",
            cidr: "10.44.132.0/24",
            prefix_length: 24,
          },
          observation: null,
          force: false,
          reason: "temporary_test",
        });
      }
      if (url.endsWith("/api/releases/jobs/job-missing-prep")) {
        return jsonResponse({
          id: "job-missing-prep",
          target_ip: "10.44.132.254",
          kind: "pre_release_query",
          reason: "temporary_test",
          ticket_id: null,
          force: false,
          status: "cancelled",
          phase: "finished",
          before_state: { entry_type: "missing", mac: null },
          after_state: {},
          result: { preparation_status: "stopped_no_record" },
          error_message: null,
          operator: { id: "user-1", username: "admin", display_name: "Admin" },
          switch: { id: "switch-1", name: "edge-sw-01", management_ip: "10.0.0.10" },
          retry_of_id: null,
          preparation_job_id: null,
          raw_output: { before: "No matching ARP entry", release: null, after: null },
          events: [],
          created_at: "2026-05-04T00:00:00Z",
          updated_at: "2026-05-04T00:00:00Z",
          started_at: "2026-05-04T00:00:10Z",
          finished_at: "2026-05-04T00:00:20Z",
        });
      }
      if (url.endsWith("/api/releases/jobs") && init?.method === "POST") {
        const payload = JSON.parse(String(init.body));
        confirmedForce = payload.force;
        return jsonResponse({ job_id: "job-forced-release", status: "queued", phase: "queued" });
      }
      if (url.endsWith("/api/releases/jobs/job-forced-release")) {
        return jsonResponse({
          id: "job-forced-release",
          target_ip: "10.44.132.254",
          kind: "release",
          reason: "temporary_test",
          ticket_id: null,
          force: true,
          status: "queued",
          phase: "queued",
          before_state: { entry_type: "missing", mac: null },
          after_state: {},
          result: {},
          error_message: null,
          operator: { id: "user-1", username: "admin", display_name: "Admin" },
          switch: { id: "switch-1", name: "edge-sw-01", management_ip: "10.0.0.10" },
          retry_of_id: null,
          preparation_job_id: "job-missing-prep",
          raw_output: { before: "No matching ARP entry", release: null, after: null },
          events: [],
          created_at: "2026-05-04T00:01:00Z",
          updated_at: "2026-05-04T00:01:00Z",
          started_at: null,
          finished_at: null,
        });
      }
      return jsonResponse({ detail: "not found" }, 404);
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<App />);
    await screen.findByLabelText("IPv4 address");
    fireEvent.change(screen.getByLabelText("IPv4 address"), {
      target: { value: "10.44.132.254" },
    });
    fireEvent.click(screen.getByRole("button", { name: /prepare/i }));

    expect(await screen.findByText("stopped no record")).toBeInTheDocument();
    const confirmationPanel = screen
      .getByRole("heading", { name: "Confirmation" })
      .closest(".tool-panel");
    expect(confirmationPanel).not.toBeNull();
    expect(within(confirmationPanel as HTMLElement).getByText("Yes")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /force release job/i }));

    await screen.findAllByRole("heading", { name: "Job detail" });
    expect(confirmedForce).toBe(true);
  });

  it("lets an admin select a switch for forced preparation when automatic resolution has no match", async () => {
    localStorage.setItem("bind-plane-token", "token-1");
    let preparePayload: Record<string, unknown> | null = null;
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/api/auth/me")) {
        return jsonResponse(admin);
      }
      if (url.endsWith("/api/admin/switches")) {
        return jsonResponse([
          {
            id: "switch-1",
            name: "edge-sw-01",
            management_ip: "10.0.0.10",
            is_enabled: true,
            networks: [],
          },
        ]);
      }
      if (url.endsWith("/api/releases/prepare")) {
        preparePayload = JSON.parse(String(init?.body));
        return jsonResponse({
          preparation_job_id: "job-selected-prep",
          status: "query_queued",
          target_ip: "10.55.1.10",
          resolved_switch: {
            switch_id: "switch-1",
            network_id: null,
            command_profile_id: "profile-1",
            management_ip: "10.0.0.10",
            name: "edge-sw-01",
            cidr: null,
            prefix_length: null,
            selection_source: "selected_switch",
          },
          observation: null,
          force: true,
          reason: "temporary_test",
        });
      }
      if (url.endsWith("/api/releases/jobs/job-selected-prep")) {
        return jsonResponse(
          releaseJob({
            id: "job-selected-prep",
            kind: "pre_release_query",
            status: "waiting_confirmation",
            phase: "waiting_confirmation",
            force: true,
            before_state: { entry_type: "missing", mac: null },
            result: { preparation_status: "ready" },
            raw_output: { before: "No matching ARP entry", release: null, after: null },
          }),
        );
      }
      return jsonResponse({ detail: "not found" }, 404);
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<App />);
    await screen.findByLabelText("IPv4 address");
    fireEvent.change(screen.getByLabelText("IPv4 address"), {
      target: { value: "10.55.1.10" },
    });
    fireEvent.click(screen.getByLabelText("Force release"));
    fireEvent.change(await screen.findByLabelText("Forced switch"), {
      target: { value: "switch-1" },
    });
    fireEvent.click(screen.getByRole("button", { name: /prepare/i }));

    await waitFor(() => expect(preparePayload).not.toBeNull());
    expect(preparePayload).toMatchObject({
      target_ip: "10.55.1.10",
      force: true,
      selected_switch_id: "switch-1",
    });
    expect(await screen.findByText(/edge-sw-01/)).toBeInTheDocument();
  });

  it("shows terminal failed state when a queued pre-release query fails", async () => {
    localStorage.setItem("bind-plane-token", "token-1");
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.endsWith("/api/auth/me")) {
        return jsonResponse(operator);
      }
      if (url.endsWith("/api/releases/prepare")) {
        return jsonResponse({
          preparation_job_id: "job-prep-failed",
          status: "query_queued",
          target_ip: "10.44.132.254",
          resolved_switch: {
            switch_id: "switch-1",
            network_id: "network-1",
            command_profile_id: "profile-1",
            management_ip: "10.0.0.10",
            name: "edge-sw-01",
            cidr: "10.44.132.0/24",
            prefix_length: 24,
          },
          observation: null,
          force: false,
          reason: "temporary_test",
        });
      }
      if (url.endsWith("/api/releases/jobs/job-prep-failed")) {
        return jsonResponse(
          releaseJob({
            id: "job-prep-failed",
            kind: "pre_release_query",
            status: "failed",
            phase: "finished",
            before_state: {},
            error_message: "Pre-release query timed out",
            preparation_job_id: null,
            raw_output: { before: null, release: null, after: null },
          }),
        );
      }
      return jsonResponse({ detail: "not found" }, 404);
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<App />);
    await screen.findByLabelText("IPv4 address");
    fireEvent.change(screen.getByLabelText("IPv4 address"), {
      target: { value: "10.44.132.254" },
    });
    fireEvent.click(screen.getByRole("button", { name: /prepare/i }));

    const confirmationPanel = await screen
      .findByRole("heading", { name: "Confirmation" })
      .then((heading) => heading.closest(".tool-panel"));
    expect(confirmationPanel).not.toBeNull();
    await waitFor(() =>
      expect(within(confirmationPanel as HTMLElement).getByText("failed")).toBeInTheDocument(),
    );
    expect(within(confirmationPanel as HTMLElement).queryByText("query queued")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /create job/i })).toBeDisabled();
  });

  it("updates full command profile fields through the admin view", async () => {
    localStorage.setItem("bind-plane-token", "token-1");
    let savedPayload: Record<string, unknown> | null = null;
    let profileRows = [commandProfile];
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/api/auth/me")) {
        return jsonResponse(admin);
      }
      if (url.endsWith("/api/admin/switches")) {
        return jsonResponse([]);
      }
      if (url.endsWith("/api/admin/command-profiles") && !init?.method) {
        return jsonResponse(profileRows);
      }
      if (url.endsWith("/api/admin/command-profiles/profile-1") && init?.method === "PATCH") {
        savedPayload = JSON.parse(String(init.body));
        profileRows = [{ ...commandProfile, ...(savedPayload as typeof commandProfile) }];
        return jsonResponse(profileRows[0]);
      }
      return jsonResponse({ detail: "not found" }, 404);
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<App />);
    await screen.findByLabelText("IPv4 address");
    fireEvent.click(screen.getByRole("button", { name: /command profiles/i }));
    await screen.findByText("h3c");
    fireEvent.click(screen.getByRole("button", { name: "Edit" }));

    fireEvent.change(screen.getByPlaceholderText("Profile name"), { target: { value: "h3c-updated" } });
    fireEvent.change(screen.getByPlaceholderText("Description"), { target: { value: "Updated profile" } });
    fireEvent.change(screen.getByLabelText("Login prompt patterns"), {
      target: { value: '{"username_pattern":"login:","password_pattern":"password:"}' },
    });
    fireEvent.change(screen.getByLabelText("Command templates"), {
      target: { value: '{"single_arp_query":"display arp $ip","arp_release":"undo arp static $ip"}' },
    });
    fireEvent.change(screen.getByLabelText("Prompt patterns"), {
      target: {
        value:
          '{"connection_options":{"device_type":"hp_comware_telnet"},"query_expect_string":"<#>"}',
      },
    });
    fireEvent.change(screen.getByLabelText("Pagination rules"), {
      target: { value: '{"disable_paging_command":"screen-length disable"}' },
    });
    fireEvent.change(screen.getByLabelText("Parser rules"), {
      target: { value: '{"arp_entry_regex":"(?P<ip>\\\\S+)","static_type_values":["STATIC"]}' },
    });
    fireEvent.change(screen.getByLabelText("Error patterns"), {
      target: { value: '["Error","Invalid"]' },
    });
    fireEvent.change(screen.getByLabelText("Success patterns"), {
      target: { value: '["Done"]' },
    });
    fireEvent.click(screen.getByLabelText("Active"));
    fireEvent.click(screen.getByRole("button", { name: /save changes/i }));

    await waitFor(() => expect(savedPayload).not.toBeNull());
    expect(savedPayload).toMatchObject({
      name: "h3c-updated",
      description: "Updated profile",
      login_prompt_patterns: { username_pattern: "login:", password_pattern: "password:" },
      command_templates: {
        single_arp_query: "display arp $ip",
        arp_release: "undo arp static $ip",
      },
      prompt_patterns: {
        connection_options: { device_type: "hp_comware_telnet" },
        query_expect_string: "<#>",
      },
      pagination_rules: { disable_paging_command: "screen-length disable" },
      parser_rules: { arp_entry_regex: "(?P<ip>\\S+)", static_type_values: ["STATIC"] },
      error_patterns: ["Error", "Invalid"],
      success_patterns: ["Done"],
      is_active: false,
    });
  });

  it("resets a user password from the admin user view", async () => {
    localStorage.setItem("bind-plane-token", "token-1");
    let resetPayload: Record<string, unknown> | null = null;
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/api/auth/me")) {
        return jsonResponse(admin);
      }
      if (url.endsWith("/api/admin/switches")) {
        return jsonResponse([]);
      }
      if (url.endsWith("/api/admin/users") && !init?.method) {
        return jsonResponse([operator]);
      }
      if (
        url.endsWith("/api/admin/users/operator/reset-password") &&
        init?.method === "POST"
      ) {
        resetPayload = JSON.parse(String(init.body));
        return jsonResponse({ ...operator, must_change_password: true });
      }
      return jsonResponse({ detail: "not found" }, 404);
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<App />);
    await screen.findByLabelText("IPv4 address");
    fireEvent.click(screen.getByRole("button", { name: /user management/i }));
    await screen.findByText("operator");
    fireEvent.change(screen.getByLabelText("New password for operator"), {
      target: { value: "new-password-123" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Reset" }));

    await waitFor(() => expect(resetPayload).not.toBeNull());
    expect(resetPayload).toEqual({ password: "new-password-123" });
  });

  it("does not show retry for a failed pre-release query job", async () => {
    localStorage.setItem("bind-plane-token", "token-1");
    const failedPreQuery = {
      id: "job-1",
      target_ip: "10.44.132.254",
      kind: "pre_release_query",
      reason: "temporary_test",
      ticket_id: null,
      force: false,
      status: "failed",
      phase: "finished",
      before_state: {},
      after_state: {},
      result: {},
      error_message: "Queue enqueue failed",
      operator: { id: "user-1", username: "operator", display_name: "Operator" },
      switch: { id: "switch-1", name: "edge-sw-01", management_ip: "10.0.0.10" },
      retry_of_id: null,
      preparation_job_id: null,
      raw_output: { before: null, release: null, after: null },
      events: [],
      created_at: "2026-05-04T00:00:00Z",
      updated_at: "2026-05-04T00:00:00Z",
      started_at: null,
      finished_at: "2026-05-04T00:01:00Z",
    };
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.endsWith("/api/auth/me")) {
        return jsonResponse(operator);
      }
      if (url.endsWith("/api/releases/jobs/job-1")) {
        return jsonResponse(failedPreQuery);
      }
      if (url.endsWith("/api/releases/jobs")) {
        return jsonResponse([failedPreQuery]);
      }
      return jsonResponse({ detail: "not found" }, 404);
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<App />);
    await screen.findByLabelText("IPv4 address");
    fireEvent.click(screen.getByRole("button", { name: /job history/i }));
    fireEvent.click(await screen.findByText("10.44.132.254"));

    expect(await screen.findAllByRole("heading", { name: "Job detail" })).not.toHaveLength(0);
    expect(screen.queryByRole("button", { name: /retry/i })).not.toBeInTheDocument();
  });

  it("switches polling to the new job after retrying a failed release", async () => {
    localStorage.setItem("bind-plane-token", "token-1");
    const failedRelease = releaseJob();
    const retryRelease = releaseJob({
      id: "job-2",
      status: "queued",
      phase: "queued",
      error_message: null,
      retry_of_id: "job-1",
      raw_output: { before: "10.44.132.254 0011-2233-4455 S", release: null, after: null },
      started_at: null,
      finished_at: null,
    });
    let originalJobLoads = 0;
    let retryJobLoads = 0;
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/api/auth/me")) {
        return jsonResponse(operator);
      }
      if (url.endsWith("/api/releases/jobs") && !init?.method) {
        return jsonResponse([failedRelease]);
      }
      if (url.endsWith("/api/releases/jobs/job-1/retry") && init?.method === "POST") {
        return jsonResponse({ job_id: "job-2" });
      }
      if (url.endsWith("/api/releases/jobs/job-1")) {
        originalJobLoads += 1;
        return jsonResponse(failedRelease);
      }
      if (url.endsWith("/api/releases/jobs/job-2")) {
        retryJobLoads += 1;
        return jsonResponse(retryRelease);
      }
      return jsonResponse({ detail: "not found" }, 404);
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<App />);
    await screen.findByLabelText("IPv4 address");
    fireEvent.click(screen.getByRole("button", { name: /job history/i }));
    fireEvent.click(await screen.findByText("10.44.132.254"));
    expect(await screen.findByRole("button", { name: /retry/i })).toBeInTheDocument();
    const originalLoadsBeforeRetry = originalJobLoads;

    fireEvent.click(screen.getByRole("button", { name: /retry/i }));

    await waitFor(() => expect(retryJobLoads).toBeGreaterThan(0));
    await waitFor(() => expect(screen.getAllByText("queued").length).toBeGreaterThan(0));
    expect(screen.queryByRole("button", { name: /retry/i })).not.toBeInTheDocument();
    expect(originalJobLoads).toBe(originalLoadsBeforeRetry);
  });
});
