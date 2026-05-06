import "@testing-library/jest-dom/vitest";

import { QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { RouterProvider, createMemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { createQueryClient } from "@/lib/query";
import { routes } from "@/routes/router";
import { useAuthStore } from "@/stores/auth";

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
  login_prompt_patterns: {
    username_pattern: "Username:",
    password_pattern: "Password:",
  },
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

function paginated<T>(items: T[]) {
  return {
    items,
    total: items.length,
    page: 1,
    page_size: 25,
    page_count: items.length ? 1 : 0,
  };
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
    started_at: "2026-05-04T00:00:10Z",
    finished_at: "2026-05-04T00:01:00Z",
    ...overrides,
  };
}

function renderRoute(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  const queryClient = createQueryClient();
  return render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

describe("App routes", () => {
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
    vi.stubGlobal(
      "ResizeObserver",
      class ResizeObserver {
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    );
    vi.stubGlobal(
      "matchMedia",
      vi.fn((query: string) => ({
        matches: false,
        media: query,
        onchange: null,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        addListener: vi.fn(),
        removeListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })),
    );
    Object.defineProperty(window.Element.prototype, "scrollIntoView", {
      configurable: true,
      value: vi.fn(),
    });
    Object.defineProperty(window.HTMLElement.prototype, "scrollIntoView", {
      configurable: true,
      value: vi.fn(),
    });
    window.localStorage.clear();
    useAuthStore.setState({ token: null });
  });

  afterEach(() => {
    cleanup();
    window.localStorage.clear();
    useAuthStore.setState({ token: null });
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("redirects protected routes to login", async () => {
    vi.stubGlobal("fetch", vi.fn());

    renderRoute("/release");

    expect(
      await screen.findByRole("button", { name: /sign in/i }),
    ).toBeInTheDocument();
  });

  it("logs in and opens the routed release console", async () => {
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

    renderRoute("/login");
    fireEvent.change(screen.getByLabelText("Username"), {
      target: { value: "operator" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "password123" },
    });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    expect(await screen.findByLabelText("IPv4 address")).toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: /audit logs/i }),
    ).not.toBeInTheDocument();
  });

  it("shows access denied for operator admin routes", async () => {
    useAuthStore.setState({ token: "token-1" });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input);
        if (url.endsWith("/api/auth/me")) {
          return jsonResponse(operator);
        }
        return jsonResponse({ detail: "not found" }, 404);
      }),
    );

    renderRoute("/admin/users");

    expect(await screen.findByText("Access denied")).toBeInTheDocument();
  });

  it("reloads current user when the auth token changes", async () => {
    useAuthStore.setState({ token: "token-1" });
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        const headers = new Headers(init?.headers);
        if (url.endsWith("/api/auth/me")) {
          return jsonResponse(
            headers.get("Authorization") === "Bearer token-2"
              ? admin
              : operator,
          );
        }
        return jsonResponse({ detail: "not found" }, 404);
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    renderRoute("/release");

    expect(await screen.findByLabelText("IPv4 address")).toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: /audit logs/i }),
    ).not.toBeInTheDocument();

    useAuthStore.getState().setToken("token-2");

    expect(
      await screen.findByRole("link", { name: /audit logs/i }),
    ).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining("/api/auth/me"),
      expect.objectContaining({
        headers: expect.any(Headers),
      }),
    );
  });

  it("prepares a release and navigates to the created job detail", async () => {
    useAuthStore.setState({ token: "token-1" });
    let confirmed = false;
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (url.endsWith("/api/auth/me")) {
          return jsonResponse(operator);
        }
        if (url.endsWith("/api/releases/prepare")) {
          expect(JSON.parse(String(init?.body))).toMatchObject({
            target_ip: "10.44.132.254",
            reason: "temporary_test",
          });
          return jsonResponse({
            preparation_job_id: "job-prep",
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
        if (url.endsWith("/api/releases/jobs/job-prep")) {
          return jsonResponse(
            releaseJob({
              id: "job-prep",
              kind: "pre_release_query",
              status: confirmed ? "queued" : "waiting_confirmation",
              error_message: null,
              raw_output: {
                before: "10.44.132.254 0011-2233-4455 S",
                release: null,
                after: null,
              },
            }),
          );
        }
        if (url.endsWith("/api/releases/jobs") && init?.method === "POST") {
          confirmed = true;
          expect(JSON.parse(String(init.body))).toMatchObject({
            preparation_job_id: "job-prep",
          });
          return jsonResponse({ job_id: "job-2" });
        }
        if (url.endsWith("/api/releases/jobs/job-2")) {
          return jsonResponse(
            releaseJob({ id: "job-2", status: "queued", error_message: null }),
          );
        }
        return jsonResponse({ detail: "not found" }, 404);
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    renderRoute("/release");
    fireEvent.change(await screen.findByLabelText("IPv4 address"), {
      target: { value: "10.44.132.254" },
    });
    fireEvent.click(screen.getByRole("button", { name: /prepare/i }));
    const createButton = await screen.findByRole("button", {
      name: /create job/i,
    });
    await waitFor(() => expect(createButton).toBeEnabled());
    fireEvent.click(createButton);

    expect(await screen.findByText("queued")).toBeInTheDocument();
  });

  it("sends explicit switch selection for forced admin preparation", async () => {
    useAuthStore.setState({ token: "token-1" });
    let prepareBody: Record<string, unknown> | null = null;
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (url.endsWith("/api/auth/me")) {
          return jsonResponse(admin);
        }
        if (url.includes("/api/admin/switches")) {
          return jsonResponse(paginated([
            {
              id: "switch-1",
              name: "edge-sw-01",
              management_ip: "10.0.0.10",
              is_enabled: true,
              networks: [],
            },
          ]));
        }
        if (url.endsWith("/api/releases/prepare")) {
          prepareBody = JSON.parse(String(init?.body)) as Record<
            string,
            unknown
          >;
          return jsonResponse({
            preparation_job_id: null,
            status: "stopped_no_record",
            target_ip: "10.44.132.254",
            resolved_switch: null,
            observation: null,
            force: true,
            reason: "temporary_test",
          });
        }
        return jsonResponse({ detail: "not found" }, 404);
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    renderRoute("/release");
    fireEvent.change(await screen.findByLabelText("IPv4 address"), {
      target: { value: "10.44.132.254" },
    });
    fireEvent.click(screen.getByText("Force release"));
    fireEvent.click(await screen.findByLabelText("Forced switch"));
    fireEvent.click(await screen.findByRole("option", { name: /edge-sw-01/i }));
    fireEvent.click(screen.getByRole("button", { name: /prepare/i }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringContaining("/api/releases/prepare"),
        expect.anything(),
      ),
    );
    expect(prepareBody).toMatchObject({
      force: true,
      selected_switch_id: "switch-1",
    });
  });

  it("retries a failed release and navigates to the new retry job", async () => {
    useAuthStore.setState({ token: "token-1" });
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (url.endsWith("/api/auth/me")) {
          return jsonResponse(operator);
        }
        if (url.endsWith("/api/releases/jobs/job-1/retry")) {
          expect(init?.method).toBe("POST");
          return jsonResponse({ job_id: "job-2" });
        }
        if (url.endsWith("/api/releases/jobs/job-2")) {
          return jsonResponse(
            releaseJob({ id: "job-2", status: "queued", error_message: null }),
          );
        }
        if (url.endsWith("/api/releases/jobs/job-1")) {
          return jsonResponse(releaseJob());
        }
        return jsonResponse({ detail: "not found" }, 404);
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    renderRoute("/jobs/job-1");
    fireEvent.click(await screen.findByRole("button", { name: /retry/i }));

    expect(await screen.findByText("queued")).toBeInTheDocument();
    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringContaining("/api/releases/jobs/job-2"),
        expect.anything(),
      ),
    );
  });

  it("uses URL-backed server-side controls on job history", async () => {
    useAuthStore.setState({ token: "token-1" });
    const jobRequests: string[] = [];
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.endsWith("/api/auth/me")) {
        return jsonResponse(operator);
      }
      if (url.includes("/api/releases/jobs?")) {
        jobRequests.push(url);
        const params = new URL(url, "http://bind-plane.test").searchParams;
        return jsonResponse({
          items: [releaseJob({ target_ip: params.get("search") || "10.44.132.254" })],
          total: 2,
          page: Number(params.get("page") ?? "1"),
          page_size: Number(params.get("page_size") ?? "25"),
          page_count: 2,
        });
      }
      return jsonResponse({ detail: "not found" }, 404);
    });
    vi.stubGlobal("fetch", fetchMock);

    renderRoute("/jobs");

    expect(await screen.findByText("10.44.132.254")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Search table"), {
      target: { value: "10.44.132.10" },
    });
    await waitFor(() =>
      expect(
        jobRequests.some((url) =>
          new URL(url, "http://bind-plane.test").searchParams.has("search"),
        ),
      ).toBe(true),
    );

    fireEvent.click(await screen.findByRole("button", { name: /target/i }));
    await waitFor(() =>
      expect(
        jobRequests.some(
          (url) =>
            new URL(url, "http://bind-plane.test").searchParams.get(
              "sort_by",
            ) === "target_ip",
        ),
      ).toBe(true),
    );

    fireEvent.click(await screen.findByRole("button", { name: /next/i }));
    await waitFor(() =>
      expect(
        jobRequests.some(
          (url) =>
            new URL(url, "http://bind-plane.test").searchParams.get("page") ===
            "2",
        ),
      ).toBe(true),
    );
  });

  it("resets a user password from the admin user route", async () => {
    useAuthStore.setState({ token: "token-1" });
    let resetPasswordBody: Record<string, unknown> | null = null;
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (url.endsWith("/api/auth/me")) {
          return jsonResponse(admin);
        }
        if (url.endsWith("/api/admin/users/operator/reset-password")) {
          resetPasswordBody = JSON.parse(String(init?.body)) as Record<
            string,
            unknown
          >;
          return jsonResponse(operator);
        }
        if (url.includes("/api/admin/users")) {
          return jsonResponse(paginated([operator]));
        }
        return jsonResponse({ detail: "not found" }, 404);
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    renderRoute("/admin/users");
    const passwordInput = await screen.findByLabelText(
      "New password for operator",
    );
    fireEvent.click(screen.getByRole("button", { name: "Reset" }));
    expect(
      await screen.findByText("Password must be at least 8 characters"),
    ).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalledWith(
      expect.stringContaining("/api/admin/users/operator/reset-password"),
      expect.anything(),
    );

    fireEvent.change(passwordInput, {
      target: { value: "NewPass123!" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Reset" }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringContaining("/api/admin/users/operator/reset-password"),
        expect.anything(),
      ),
    );
    expect(resetPasswordBody).toEqual({ password: "NewPass123!" });
  });

  it("updates full command profile fields from the admin profile route", async () => {
    useAuthStore.setState({ token: "token-1" });
    let patchBody: Record<string, unknown> | null = null;
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (url.endsWith("/api/auth/me")) {
          return jsonResponse(admin);
        }
        if (url.endsWith("/api/admin/command-profiles/profile-1")) {
          patchBody = JSON.parse(String(init?.body)) as Record<string, unknown>;
          return jsonResponse({ ...commandProfile, success_patterns: ["OK"] });
        }
        if (url.includes("/api/admin/command-profiles")) {
          return jsonResponse(paginated([commandProfile]));
        }
        return jsonResponse({ detail: "not found" }, 404);
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    renderRoute("/admin/profiles");
    fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
    const successInput = screen.getByLabelText("Success patterns");
    fireEvent.change(successInput, {
      target: { value: JSON.stringify(["OK"]) },
    });
    const profileForm = successInput.closest("form") as HTMLElement;
    fireEvent.click(
      within(profileForm).getByRole("button", { name: /save changes/i }),
    );

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringContaining("/api/admin/command-profiles/profile-1"),
        expect.objectContaining({ method: "PATCH" }),
      ),
    );
    expect(patchBody).toMatchObject({
      name: "h3c",
      description: "H3C Telnet profile",
      login_prompt_patterns: {
        username_pattern: "Username:",
        password_pattern: "Password:",
      },
      command_templates: {
        single_arp_query: "display arp $ip",
        arp_release: "undo arp static $ip",
      },
      prompt_patterns: {
        connection_options: { device_type: "hp_comware_telnet" },
        query_expect_string: "[>#]",
      },
      pagination_rules: { disable_paging_command: "screen-length disable" },
      success_patterns: ["OK"],
      error_patterns: ["Error"],
      parser_rules: {
        arp_entry_regex: "(?P<ip>\\S+)\\s+(?P<mac>\\S+)",
        static_type_values: ["S"],
      },
      is_active: true,
    });
  });
});
