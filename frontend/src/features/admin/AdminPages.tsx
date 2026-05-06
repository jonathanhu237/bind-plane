import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ClipboardList,
  Database,
  KeyRound,
  Network,
  TerminalSquare,
  UserCog,
  Users,
} from "lucide-react";
import { useMemo, useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";

import { apiRequest } from "@/api/client";
import { withListParams } from "@/api/listParams";
import { useToken } from "@/api/hooks";
import { queryKeys } from "@/api/queryKeys";
import type {
  AuditLog,
  CommandProfile,
  Credential,
  ImportBatch,
  PaginatedResponse,
  SwitchRecord,
  UserRead,
} from "@/api/types";
import { Alert } from "@/components/ui/alert";
import {
  InputField,
  SelectField,
  TextareaField,
} from "@/components/forms/fields";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  SortableTableHead,
  TableFilterSelect,
  TablePagination,
  TableToolbar,
} from "@/features/shared/TableControls";
import { EmptyState } from "@/features/shared/status";
import { useTableState } from "@/features/shared/tableState";
import { formatDate, pretty } from "@/lib/utils";

import {
  commandProfileSchema,
  commandProfileToForm,
  defaultCommandProfileForm,
  type CommandProfileFormValues,
} from "./commandProfileForm";

const userSchema = z.object({
  username: z.string().min(1, "Username is required"),
  displayName: z.string(),
  password: z.string().min(1, "Password is required"),
  role: z.enum(["operator", "admin"]),
});

const credentialSchema = z.object({
  name: z.string().min(1, "Name is required"),
  username: z.string().min(1, "Username is required"),
  password: z.string().min(1, "Password is required"),
  secret: z.string(),
});

const importSchema = z.object({
  recordsJson: z.string().min(1, "Import JSON is required"),
});

const roleOptions = [
  { value: "operator", label: "operator" },
  { value: "admin", label: "admin" },
];
const activeFilterOptions = [
  { value: "true", label: "active" },
  { value: "false", label: "inactive" },
];
const booleanFilterOptions = [
  { value: "true", label: "yes" },
  { value: "false", label: "no" },
];
const userFilterKeys = ["role", "is_active"];
const activeFilterKeys = ["is_active"];
const switchFilterKeys = ["is_enabled", "is_validated"];
const importFilterKeys = ["status"];
const auditFilterKeys = ["action", "target_type"];
const importStatusOptions = [
  { value: "applied", label: "applied" },
  { value: "failed", label: "failed" },
  { value: "draft", label: "draft" },
  { value: "validated", label: "validated" },
];
const auditActionOptions = [
  { value: "release_pre_query_queued", label: "pre-query queued" },
  { value: "release_pre_query_completed", label: "pre-query completed" },
  { value: "release_pre_query_confirmed", label: "pre-query confirmed" },
  { value: "release_job_created", label: "job created" },
  { value: "release_job_retried", label: "job retried" },
  { value: "release_job_finished", label: "job finished" },
  { value: "release_job_enqueue_failed", label: "enqueue failed" },
  { value: "release_retry_enqueue_failed", label: "retry enqueue failed" },
];
const auditTargetOptions = [
  { value: "release_job", label: "release job" },
  { value: "user", label: "user" },
  { value: "credential", label: "credential" },
  { value: "command_profile", label: "command profile" },
  { value: "switch", label: "switch" },
  { value: "import_batch", label: "import batch" },
];

function AdminPanel({
  title,
  icon,
  error,
  children,
}: {
  title: string;
  icon: React.ReactNode;
  error?: string | null;
  children: React.ReactNode;
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          {icon}
          {title}
        </CardTitle>
      </CardHeader>
      <CardContent className="grid gap-5">
        {error ? <Alert variant="destructive">{error}</Alert> : null}
        {children}
      </CardContent>
    </Card>
  );
}

export function UsersAdminPage() {
  const token = useToken();
  const queryClient = useQueryClient();
  const table = useTableState({
    defaultSortBy: "username",
    filterKeys: userFilterKeys,
  });
  const [resetPasswords, setResetPasswords] = useState<Record<string, string>>(
    {},
  );
  const [resetErrors, setResetErrors] = useState<Record<string, string>>({});
  const usersQuery = useQuery({
    queryKey: queryKeys.usersList(table.apiParams),
    queryFn: () =>
      apiRequest<PaginatedResponse<UserRead>>(
        withListParams("/admin/users", table.apiParams),
        token,
      ),
  });
  const users = usersQuery.data?.items ?? [];
  const form = useForm<z.infer<typeof userSchema>>({
    resolver: zodResolver(userSchema),
    defaultValues: {
      username: "",
      displayName: "",
      password: "",
      role: "operator",
    },
  });
  const createMutation = useMutation({
    mutationFn: (values: z.infer<typeof userSchema>) =>
      apiRequest<UserRead>("/admin/users", token, {
        method: "POST",
        body: JSON.stringify({
          username: values.username,
          display_name: values.displayName || null,
          password: values.password,
          roles: [values.role],
        }),
      }),
    onSuccess: async () => {
      form.reset();
      await queryClient.invalidateQueries({ queryKey: queryKeys.users });
    },
  });
  const resetPasswordSchema = z.object({
    username: z.string().min(1),
    password: z.string().min(8, "Password must be at least 8 characters"),
  });
  const resetMutation = useMutation({
    mutationFn: ({
      username,
      password,
    }: {
      username: string;
      password: string;
    }) =>
      apiRequest<UserRead>(`/admin/users/${username}/reset-password`, token, {
        method: "POST",
        body: JSON.stringify({ password }),
      }),
    onSuccess: async (_, variables) => {
      setResetPasswords((current) => ({
        ...current,
        [variables.username]: "",
      }));
      setResetErrors((current) => ({ ...current, [variables.username]: "" }));
      await queryClient.invalidateQueries({ queryKey: queryKeys.users });
    },
  });

  function resetPassword(username: string) {
    const parsed = resetPasswordSchema.safeParse({
      username,
      password: resetPasswords[username] ?? "",
    });
    if (!parsed.success) {
      setResetErrors((current) => ({
        ...current,
        [username]: parsed.error.issues[0]?.message ?? "Invalid password",
      }));
      return;
    }
    setResetErrors((current) => ({ ...current, [username]: "" }));
    resetMutation.mutate(parsed.data);
  }

  return (
    <AdminPanel
      title="User management"
      icon={<Users size={18} />}
      error={
        usersQuery.error?.message ??
        createMutation.error?.message ??
        resetMutation.error?.message
      }
    >
      <Form {...form}>
        <form
          className="grid gap-3 md:grid-cols-5"
          onSubmit={form.handleSubmit((values) =>
            createMutation.mutate(values),
          )}
        >
          <InputField
            control={form.control}
            label="Username"
            name="username"
            placeholder="Username"
          />
          <InputField
            control={form.control}
            label="Display name"
            name="displayName"
            placeholder="Display name"
          />
          <InputField
            control={form.control}
            label="Initial password"
            name="password"
            placeholder="Initial password"
            type="password"
          />
          <SelectField
            control={form.control}
            label="Role"
            name="role"
            options={roleOptions}
          />
          <div className="flex items-end">
            <Button disabled={createMutation.isPending} type="submit">
              <UserCog size={16} />
              Create
            </Button>
          </div>
        </form>
      </Form>
      <TableToolbar
        search={table.search}
        searchPlaceholder="Search users"
        onSearchChange={table.setSearch}
      >
        <TableFilterSelect
          label="Role"
          options={roleOptions}
          value={table.filters.role}
          onValueChange={(value) => table.setFilter("role", value)}
        />
        <TableFilterSelect
          label="State"
          options={activeFilterOptions}
          value={table.filters.is_active}
          onValueChange={(value) => table.setFilter("is_active", value)}
        />
      </TableToolbar>
      {users.length ? (
        <Table>
          <TableHeader>
            <TableRow>
              <SortableTableHead
                field="username"
                sortBy={table.sortBy}
                sortOrder={table.sortOrder}
                onSort={table.toggleSort}
              >
                Username
              </SortableTableHead>
              <TableHead>Roles</TableHead>
              <SortableTableHead
                field="is_active"
                sortBy={table.sortBy}
                sortOrder={table.sortOrder}
                onSort={table.toggleSort}
              >
                State
              </SortableTableHead>
              <TableHead>Reset password</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {users.map((item) => (
              <TableRow key={item.id}>
                <TableCell>{item.username}</TableCell>
                <TableCell>{item.roles.join(", ")}</TableCell>
                <TableCell>{item.is_active ? "active" : "inactive"}</TableCell>
                <TableCell>
                  <div className="flex max-w-md gap-2">
                    <Input
                      aria-label={`New password for ${item.username}`}
                      placeholder="New password"
                      type="password"
                      value={resetPasswords[item.username] ?? ""}
                      onChange={(event) =>
                        setResetPasswords((current) => ({
                          ...current,
                          [item.username]: event.target.value,
                        }))
                      }
                    />
                    <Button
                      disabled={resetMutation.isPending}
                      type="button"
                      variant="secondary"
                      onClick={() => resetPassword(item.username)}
                    >
                      Reset
                    </Button>
                  </div>
                  {resetErrors[item.username] ? (
                    <p className="mt-1 text-xs text-destructive">
                      {resetErrors[item.username]}
                    </p>
                  ) : null}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      ) : (
        <EmptyState
          label={usersQuery.isLoading ? "Loading users" : "No users"}
        />
      )}
      {usersQuery.data ? (
        <TablePagination
          page={usersQuery.data.page}
          pageCount={usersQuery.data.page_count}
          pageSize={usersQuery.data.page_size}
          total={usersQuery.data.total}
          onPageChange={table.setPage}
          onPageSizeChange={table.setPageSize}
        />
      ) : null}
    </AdminPanel>
  );
}

export function CredentialsAdminPage() {
  const token = useToken();
  const queryClient = useQueryClient();
  const table = useTableState({
    defaultSortBy: "name",
    filterKeys: activeFilterKeys,
  });
  const credentialsQuery = useQuery({
    queryKey: queryKeys.credentialsList(table.apiParams),
    queryFn: () =>
      apiRequest<PaginatedResponse<Credential>>(
        withListParams("/admin/credentials", table.apiParams),
        token,
      ),
  });
  const credentials = credentialsQuery.data?.items ?? [];
  const form = useForm<z.infer<typeof credentialSchema>>({
    resolver: zodResolver(credentialSchema),
    defaultValues: { name: "", username: "", password: "", secret: "" },
  });
  const createMutation = useMutation({
    mutationFn: (values: z.infer<typeof credentialSchema>) =>
      apiRequest<Credential>("/admin/credentials", token, {
        method: "POST",
        body: JSON.stringify({ ...values, secret: values.secret || null }),
      }),
    onSuccess: async () => {
      form.reset();
      await queryClient.invalidateQueries({ queryKey: queryKeys.credentials });
    },
  });

  return (
    <AdminPanel
      title="Credential management"
      icon={<KeyRound size={18} />}
      error={credentialsQuery.error?.message ?? createMutation.error?.message}
    >
      <Form {...form}>
        <form
          className="grid gap-3 md:grid-cols-5"
          onSubmit={form.handleSubmit((values) =>
            createMutation.mutate(values),
          )}
        >
          <InputField
            control={form.control}
            label="Name"
            name="name"
            placeholder="Name"
          />
          <InputField
            control={form.control}
            label="Username"
            name="username"
            placeholder="Username"
          />
          <InputField
            control={form.control}
            label="Password"
            name="password"
            placeholder="Password"
            type="password"
          />
          <InputField
            control={form.control}
            label="Enable secret"
            name="secret"
            placeholder="Enable secret"
            type="password"
          />
          <div className="flex items-end">
            <Button disabled={createMutation.isPending} type="submit">
              <KeyRound size={16} />
              Save
            </Button>
          </div>
        </form>
      </Form>
      <TableToolbar
        search={table.search}
        searchPlaceholder="Search credentials"
        onSearchChange={table.setSearch}
      >
        <TableFilterSelect
          label="State"
          options={activeFilterOptions}
          value={table.filters.is_active}
          onValueChange={(value) => table.setFilter("is_active", value)}
        />
      </TableToolbar>
      {credentials.length ? (
        <Table>
          <TableHeader>
            <TableRow>
              <SortableTableHead
                field="name"
                sortBy={table.sortBy}
                sortOrder={table.sortOrder}
                onSort={table.toggleSort}
              >
                Name
              </SortableTableHead>
              <SortableTableHead
                field="username"
                sortBy={table.sortBy}
                sortOrder={table.sortOrder}
                onSort={table.toggleSort}
              >
                Username
              </SortableTableHead>
              <SortableTableHead
                field="is_active"
                sortBy={table.sortBy}
                sortOrder={table.sortOrder}
                onSort={table.toggleSort}
              >
                State
              </SortableTableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {credentials.map((item) => (
              <TableRow key={item.id}>
                <TableCell>{item.name}</TableCell>
                <TableCell>{item.username}</TableCell>
                <TableCell>{item.is_active ? "active" : "inactive"}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      ) : (
        <EmptyState
          label={
            credentialsQuery.isLoading
              ? "Loading credentials"
              : "No credentials"
          }
        />
      )}
      {credentialsQuery.data ? (
        <TablePagination
          page={credentialsQuery.data.page}
          pageCount={credentialsQuery.data.page_count}
          pageSize={credentialsQuery.data.page_size}
          total={credentialsQuery.data.total}
          onPageChange={table.setPage}
          onPageSizeChange={table.setPageSize}
        />
      ) : null}
    </AdminPanel>
  );
}

export function ImportsAdminPage() {
  const token = useToken();
  const queryClient = useQueryClient();
  const switchTable = useTableState({
    defaultSortBy: "name",
    filterKeys: switchFilterKeys,
    prefix: "switch_",
  });
  const importTable = useTableState({
    defaultSortBy: "created_at",
    defaultSortOrder: "desc",
    filterKeys: importFilterKeys,
    prefix: "import_",
  });
  const credentialsQuery = useQuery({
    queryKey: queryKeys.credentialsList({ pageSize: 200, sortBy: "name" }),
    queryFn: () =>
      apiRequest<PaginatedResponse<Credential>>(
        withListParams("/admin/credentials", { pageSize: 200, sortBy: "name" }),
        token,
      ),
  });
  const profilesQuery = useQuery({
    queryKey: queryKeys.commandProfilesList({ pageSize: 200, sortBy: "name" }),
    queryFn: () =>
      apiRequest<PaginatedResponse<CommandProfile>>(
        withListParams("/admin/command-profiles", {
          pageSize: 200,
          sortBy: "name",
        }),
        token,
      ),
  });
  const switchesQuery = useQuery({
    queryKey: queryKeys.switchesList(switchTable.apiParams),
    queryFn: () =>
      apiRequest<PaginatedResponse<SwitchRecord>>(
        withListParams("/admin/switches", switchTable.apiParams),
        token,
      ),
  });
  const importsQuery = useQuery({
    queryKey: queryKeys.importsList(importTable.apiParams),
    queryFn: () =>
      apiRequest<PaginatedResponse<ImportBatch>>(
        withListParams("/admin/imports", importTable.apiParams),
        token,
      ),
  });
  const switches = switchesQuery.data?.items ?? [];
  const imports = importsQuery.data?.items ?? [];
  const form = useForm<z.infer<typeof importSchema>>({
    resolver: zodResolver(importSchema),
    defaultValues: { recordsJson: "[]" },
  });
  const importMutation = useMutation({
    mutationFn: (values: z.infer<typeof importSchema>) =>
      apiRequest<ImportBatch>("/admin/imports/switch-networks", token, {
        method: "POST",
        body: JSON.stringify({
          source_filename: "manual-json",
          records: JSON.parse(values.recordsJson),
        }),
      }),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.imports }),
        queryClient.invalidateQueries({ queryKey: queryKeys.switches }),
      ]);
    },
  });
  const sample = useMemo(
    () =>
      JSON.stringify(
        [
          {
            switch_name: "edge-sw-01",
            management_ip: "10.0.0.10",
            cidr: "10.44.132.0/24",
            credential_id:
              credentialsQuery.data?.items[0]?.id ?? "credential-uuid",
            command_profile_id:
              profilesQuery.data?.items[0]?.id ?? "profile-uuid",
            network_validated: true,
          },
        ],
        null,
        2,
      ),
    [credentialsQuery.data, profilesQuery.data],
  );

  return (
    <AdminPanel
      title="Switch and network imports"
      icon={<Network size={18} />}
      error={
        credentialsQuery.error?.message ??
        profilesQuery.error?.message ??
        switchesQuery.error?.message ??
        importsQuery.error?.message ??
        importMutation.error?.message
      }
    >
      <Form {...form}>
        <form
          className="grid gap-3"
          onSubmit={form.handleSubmit((values) =>
            importMutation.mutate(values),
          )}
        >
          <TextareaField
            controlClassName="font-mono text-xs"
            control={form.control}
            label="Import records JSON"
            name="recordsJson"
            placeholder={sample}
          />
          <Button
            className="w-fit"
            disabled={importMutation.isPending}
            type="submit"
          >
            <Database size={16} />
            Import
          </Button>
        </form>
      </Form>
      <div className="grid gap-5 lg:grid-cols-2">
        <div>
          <h3 className="mb-2 text-sm font-semibold">Switches</h3>
          <TableToolbar
            search={switchTable.search}
            searchPlaceholder="Search switches"
            onSearchChange={switchTable.setSearch}
          >
            <TableFilterSelect
              label="Enabled"
              options={booleanFilterOptions}
              value={switchTable.filters.is_enabled}
              onValueChange={(value) =>
                switchTable.setFilter("is_enabled", value)
              }
            />
            <TableFilterSelect
              label="Validated"
              options={booleanFilterOptions}
              value={switchTable.filters.is_validated}
              onValueChange={(value) =>
                switchTable.setFilter("is_validated", value)
              }
            />
          </TableToolbar>
          {switches.length ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <SortableTableHead
                    field="name"
                    sortBy={switchTable.sortBy}
                    sortOrder={switchTable.sortOrder}
                    onSort={switchTable.toggleSort}
                  >
                    Name
                  </SortableTableHead>
                  <SortableTableHead
                    field="management_ip"
                    sortBy={switchTable.sortBy}
                    sortOrder={switchTable.sortOrder}
                    onSort={switchTable.toggleSort}
                  >
                    Management IP
                  </SortableTableHead>
                  <TableHead>Networks</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {switches.map((item) => (
                  <TableRow key={item.id}>
                    <TableCell>{item.name}</TableCell>
                    <TableCell>{item.management_ip}</TableCell>
                    <TableCell>
                      {item.networks.map((network) => network.cidr).join(", ")}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <EmptyState label="No switches" />
          )}
          {switchesQuery.data ? (
            <TablePagination
              page={switchesQuery.data.page}
              pageCount={switchesQuery.data.page_count}
              pageSize={switchesQuery.data.page_size}
              total={switchesQuery.data.total}
              onPageChange={switchTable.setPage}
              onPageSizeChange={switchTable.setPageSize}
            />
          ) : null}
        </div>
        <div>
          <h3 className="mb-2 text-sm font-semibold">Import batches</h3>
          <TableToolbar
            search={importTable.search}
            searchPlaceholder="Search imports"
            onSearchChange={importTable.setSearch}
          >
            <TableFilterSelect
              label="Status"
              options={importStatusOptions}
              value={importTable.filters.status}
              onValueChange={(value) => importTable.setFilter("status", value)}
            />
          </TableToolbar>
          {imports.length ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <SortableTableHead
                    field="status"
                    sortBy={importTable.sortBy}
                    sortOrder={importTable.sortOrder}
                    onSort={importTable.toggleSort}
                  >
                    Status
                  </SortableTableHead>
                  <SortableTableHead
                    field="created_at"
                    sortBy={importTable.sortBy}
                    sortOrder={importTable.sortOrder}
                    onSort={importTable.toggleSort}
                  >
                    Created
                  </SortableTableHead>
                  <TableHead>Summary</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {imports.map((item) => (
                  <TableRow key={item.id}>
                    <TableCell>{item.status}</TableCell>
                    <TableCell>{formatDate(item.created_at)}</TableCell>
                    <TableCell>
                      <code>{pretty(item.summary)}</code>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <EmptyState label="No imports" />
          )}
          {importsQuery.data ? (
            <TablePagination
              page={importsQuery.data.page}
              pageCount={importsQuery.data.page_count}
              pageSize={importsQuery.data.page_size}
              total={importsQuery.data.total}
              onPageChange={importTable.setPage}
              onPageSizeChange={importTable.setPageSize}
            />
          ) : null}
        </div>
      </div>
    </AdminPanel>
  );
}

export function ProfilesAdminPage() {
  const token = useToken();
  const queryClient = useQueryClient();
  const [editingId, setEditingId] = useState<string | null>(null);
  const [formError, setFormError] = useState<string | null>(null);
  const table = useTableState({
    defaultSortBy: "name",
    filterKeys: activeFilterKeys,
  });
  const profilesQuery = useQuery({
    queryKey: queryKeys.commandProfilesList(table.apiParams),
    queryFn: () =>
      apiRequest<PaginatedResponse<CommandProfile>>(
        withListParams("/admin/command-profiles", table.apiParams),
        token,
      ),
  });
  const profiles = profilesQuery.data?.items ?? [];
  const form = useForm<CommandProfileFormValues>({
    defaultValues: defaultCommandProfileForm,
  });
  const saveMutation = useMutation({
    mutationFn: (values: CommandProfileFormValues) => {
      const parsed = commandProfileSchema.safeParse(values);
      if (!parsed.success) {
        throw new Error(
          parsed.error.issues[0]?.message ?? "Invalid command profile",
        );
      }
      return apiRequest<CommandProfile>(
        editingId
          ? `/admin/command-profiles/${editingId}`
          : "/admin/command-profiles",
        token,
        {
          method: editingId ? "PATCH" : "POST",
          body: JSON.stringify(parsed.data),
        },
      );
    },
    onSuccess: async () => {
      form.reset(defaultCommandProfileForm);
      setEditingId(null);
      setFormError(null);
      await queryClient.invalidateQueries({
        queryKey: queryKeys.commandProfiles,
      });
    },
    onError: (err) =>
      setFormError(err instanceof Error ? err.message : "Save profile failed"),
  });

  function startEdit(profile: CommandProfile) {
    form.reset(commandProfileToForm(profile));
    setEditingId(profile.id);
    setFormError(null);
  }

  function resetForm() {
    form.reset(defaultCommandProfileForm);
    setEditingId(null);
    setFormError(null);
  }

  const textFields: Array<[keyof CommandProfileFormValues, string]> = [
    ["loginPromptPatterns", "Login prompt patterns"],
    ["commandTemplates", "Command templates"],
    ["promptPatterns", "Prompt patterns"],
    ["paginationRules", "Pagination rules"],
    ["parserRules", "Parser rules"],
    ["errorPatterns", "Error patterns"],
    ["successPatterns", "Success patterns"],
  ];

  return (
    <AdminPanel
      title="Command profiles"
      icon={<TerminalSquare size={18} />}
      error={profilesQuery.error?.message ?? formError}
    >
      <Form {...form}>
        <form
          className="grid gap-4"
          onSubmit={form.handleSubmit((values) => saveMutation.mutate(values))}
        >
          <div className="grid gap-3 md:grid-cols-2">
            <InputField
              control={form.control}
              label="Profile name"
              name="name"
              placeholder="Profile name"
            />
            <InputField
              control={form.control}
              label="Description"
              name="description"
              placeholder="Description"
            />
          </div>
          <div className="grid gap-3 lg:grid-cols-2">
            {textFields.map(([field, label]) => (
              <TextareaField
                key={field}
                control={form.control}
                controlClassName="min-h-36 font-mono text-xs"
                label={label}
                name={field}
              />
            ))}
          </div>
          <FormField
            control={form.control}
            name="isActive"
            render={({ field }) => (
              <FormItem className="flex flex-row items-center gap-2 space-y-0">
                <FormControl>
                  <Checkbox
                    checked={field.value}
                    onCheckedChange={(checked) =>
                      field.onChange(Boolean(checked))
                    }
                  />
                </FormControl>
                <FormLabel>Active</FormLabel>
              </FormItem>
            )}
          />
          <div className="flex flex-wrap gap-2">
            <Button disabled={saveMutation.isPending} type="submit">
              <TerminalSquare size={16} />
              {editingId ? "Save changes" : "Create"}
            </Button>
            {editingId ? (
              <Button type="button" variant="secondary" onClick={resetForm}>
                Cancel edit
              </Button>
            ) : null}
          </div>
        </form>
      </Form>
      <TableToolbar
        search={table.search}
        searchPlaceholder="Search profiles"
        onSearchChange={table.setSearch}
      >
        <TableFilterSelect
          label="State"
          options={activeFilterOptions}
          value={table.filters.is_active}
          onValueChange={(value) => table.setFilter("is_active", value)}
        />
      </TableToolbar>
      {profiles.length ? (
        <Table>
          <TableHeader>
            <TableRow>
              <SortableTableHead
                field="name"
                sortBy={table.sortBy}
                sortOrder={table.sortOrder}
                onSort={table.toggleSort}
              >
                Name
              </SortableTableHead>
              <TableHead>Templates</TableHead>
              <SortableTableHead
                field="is_active"
                sortBy={table.sortBy}
                sortOrder={table.sortOrder}
                onSort={table.toggleSort}
              >
                State
              </SortableTableHead>
              <TableHead>Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {profiles.map((item) => (
              <TableRow key={item.id}>
                <TableCell>{item.name}</TableCell>
                <TableCell>
                  {Object.keys(item.command_templates).join(", ")}
                </TableCell>
                <TableCell>{item.is_active ? "active" : "inactive"}</TableCell>
                <TableCell>
                  <Button
                    type="button"
                    variant="secondary"
                    onClick={() => startEdit(item)}
                  >
                    Edit
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      ) : (
        <EmptyState
          label={profilesQuery.isLoading ? "Loading profiles" : "No profiles"}
        />
      )}
      {profilesQuery.data ? (
        <TablePagination
          page={profilesQuery.data.page}
          pageCount={profilesQuery.data.page_count}
          pageSize={profilesQuery.data.page_size}
          total={profilesQuery.data.total}
          onPageChange={table.setPage}
          onPageSizeChange={table.setPageSize}
        />
      ) : null}
    </AdminPanel>
  );
}

export function AuditLogsPage() {
  const token = useToken();
  const table = useTableState({
    defaultSortBy: "created_at",
    defaultSortOrder: "desc",
    filterKeys: auditFilterKeys,
  });
  const auditQuery = useQuery({
    queryKey: queryKeys.auditList(table.apiParams),
    queryFn: () =>
      apiRequest<PaginatedResponse<AuditLog>>(
        withListParams("/audit", table.apiParams),
        token,
      ),
  });
  const logs = auditQuery.data?.items ?? [];

  return (
    <AdminPanel
      title="Audit logs"
      icon={<ClipboardList size={18} />}
      error={auditQuery.error?.message}
    >
      <TableToolbar
        search={table.search}
        searchPlaceholder="Search audit logs"
        onSearchChange={table.setSearch}
      >
        <TableFilterSelect
          label="Action"
          options={auditActionOptions}
          value={table.filters.action}
          onValueChange={(value) => table.setFilter("action", value)}
        />
        <TableFilterSelect
          label="Target"
          options={auditTargetOptions}
          value={table.filters.target_type}
          onValueChange={(value) => table.setFilter("target_type", value)}
        />
      </TableToolbar>
      {logs.length ? (
        <Table>
          <TableHeader>
            <TableRow>
              <SortableTableHead
                field="created_at"
                sortBy={table.sortBy}
                sortOrder={table.sortOrder}
                onSort={table.toggleSort}
              >
                Time
              </SortableTableHead>
              <SortableTableHead
                field="action"
                sortBy={table.sortBy}
                sortOrder={table.sortOrder}
                onSort={table.toggleSort}
              >
                Action
              </SortableTableHead>
              <SortableTableHead
                field="target_type"
                sortBy={table.sortBy}
                sortOrder={table.sortOrder}
                onSort={table.toggleSort}
              >
                Target
              </SortableTableHead>
              <TableHead>Payload</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {logs.map((log) => (
              <TableRow key={log.id}>
                <TableCell>{formatDate(log.created_at)}</TableCell>
                <TableCell>{log.action}</TableCell>
                <TableCell>{log.target_type}</TableCell>
                <TableCell>
                  <code>{pretty(log.payload)}</code>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      ) : (
        <EmptyState
          label={auditQuery.isLoading ? "Loading audit logs" : "No audit logs"}
        />
      )}
      {auditQuery.data ? (
        <TablePagination
          page={auditQuery.data.page}
          pageCount={auditQuery.data.page_count}
          pageSize={auditQuery.data.page_size}
          total={auditQuery.data.total}
          onPageChange={table.setPage}
          onPageSizeChange={table.setPageSize}
        />
      ) : null}
    </AdminPanel>
  );
}
