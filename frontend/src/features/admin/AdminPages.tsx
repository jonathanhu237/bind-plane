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
import { useTranslation } from "react-i18next";
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
import { activeLabel, booleanLabel, roleLabel, statusLabel } from "@/i18n/labels";
import { formatDate, pretty } from "@/lib/utils";

import {
  createCommandProfileSchema,
  commandProfileToForm,
  defaultCommandProfileForm,
  type CommandProfileFormValues,
} from "./commandProfileForm";

function createUserSchema(t: (key: string) => string) {
  return z.object({
    username: z.string().min(1, t("validation.usernameRequired")),
    displayName: z.string(),
    password: z.string().min(1, t("validation.passwordRequired")),
    role: z.enum(["operator", "admin"]),
  });
}

function createCredentialSchema(t: (key: string) => string) {
  return z.object({
    name: z.string().min(1, t("validation.nameRequired")),
    username: z.string().min(1, t("validation.usernameRequired")),
    password: z.string().min(1, t("validation.passwordRequired")),
    secret: z.string(),
  });
}

function createImportSchema(t: (key: string) => string) {
  return z.object({
    recordsJson: z.string().min(1, t("validation.importJsonRequired")),
  });
}

const userFilterKeys = ["role", "is_active"];
const activeFilterKeys = ["is_active"];
const switchFilterKeys = ["is_enabled", "is_validated"];
const importFilterKeys = ["status"];
const auditFilterKeys = ["action", "target_type"];
const importStatusValues = ["applied", "failed", "draft", "validated"];
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
  const { t } = useTranslation();
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
  const userSchema = useMemo(() => createUserSchema(t), [t]);
  const roleOptions = useMemo(
    () => [
      { value: "operator", label: roleLabel(t, "operator") },
      { value: "admin", label: roleLabel(t, "admin") },
    ],
    [t],
  );
  const activeFilterOptions = useMemo(
    () => [
      { value: "true", label: activeLabel(t, true) },
      { value: "false", label: activeLabel(t, false) },
    ],
    [t],
  );
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
  const resetPasswordSchema = useMemo(
    () =>
      z.object({
        username: z.string().min(1),
        password: z.string().min(8, t("validation.passwordMin8")),
      }),
    [t],
  );
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
        [username]:
          parsed.error.issues[0]?.message ?? t("validation.invalidPassword"),
      }));
      return;
    }
    setResetErrors((current) => ({ ...current, [username]: "" }));
    resetMutation.mutate(parsed.data);
  }

  return (
    <AdminPanel
      title={t("admin.users.title")}
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
            label={t("auth.username")}
            name="username"
            placeholder={t("auth.username")}
          />
          <InputField
            control={form.control}
            label={t("admin.users.displayName")}
            name="displayName"
            placeholder={t("admin.users.displayName")}
          />
          <InputField
            control={form.control}
            label={t("admin.users.initialPassword")}
            name="password"
            placeholder={t("admin.users.initialPassword")}
            type="password"
          />
          <SelectField
            control={form.control}
            label={t("admin.users.role")}
            name="role"
            options={roleOptions}
          />
          <div className="flex items-end">
            <Button disabled={createMutation.isPending} type="submit">
              <UserCog size={16} />
              {t("common.create")}
            </Button>
          </div>
        </form>
      </Form>
      <TableToolbar
        search={table.search}
        searchPlaceholder={t("admin.users.search")}
        onSearchChange={table.setSearch}
      >
        <TableFilterSelect
          label={t("admin.users.role")}
          options={roleOptions}
          value={table.filters.role}
          onValueChange={(value) => table.setFilter("role", value)}
        />
        <TableFilterSelect
          label={t("common.state")}
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
                {t("auth.username")}
              </SortableTableHead>
              <TableHead>{t("admin.users.roles")}</TableHead>
              <SortableTableHead
                field="is_active"
                sortBy={table.sortBy}
                sortOrder={table.sortOrder}
                onSort={table.toggleSort}
              >
                {t("common.state")}
              </SortableTableHead>
              <TableHead>{t("admin.users.resetPassword")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {users.map((item) => (
              <TableRow key={item.id}>
                <TableCell>{item.username}</TableCell>
                <TableCell>
                  {item.roles.map((role) => roleLabel(t, role)).join(", ")}
                </TableCell>
                <TableCell>{activeLabel(t, item.is_active)}</TableCell>
                <TableCell>
                  <div className="flex max-w-md gap-2">
                    <Input
                      aria-label={t("admin.users.newPasswordFor", {
                        username: item.username,
                      })}
                      placeholder={t("admin.users.newPassword")}
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
                      {t("common.reset")}
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
          label={
            usersQuery.isLoading
              ? t("admin.users.loading")
              : t("admin.users.empty")
          }
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
  const { t } = useTranslation();
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
  const credentialSchema = useMemo(() => createCredentialSchema(t), [t]);
  const activeFilterOptions = useMemo(
    () => [
      { value: "true", label: activeLabel(t, true) },
      { value: "false", label: activeLabel(t, false) },
    ],
    [t],
  );
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
      title={t("admin.credentials.title")}
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
            label={t("admin.credentials.name")}
            name="name"
            placeholder={t("admin.credentials.name")}
          />
          <InputField
            control={form.control}
            label={t("admin.credentials.username")}
            name="username"
            placeholder={t("admin.credentials.username")}
          />
          <InputField
            control={form.control}
            label={t("admin.credentials.password")}
            name="password"
            placeholder={t("admin.credentials.password")}
            type="password"
          />
          <InputField
            control={form.control}
            label={t("admin.credentials.secret")}
            name="secret"
            placeholder={t("admin.credentials.secret")}
            type="password"
          />
          <div className="flex items-end">
            <Button disabled={createMutation.isPending} type="submit">
              <KeyRound size={16} />
              {t("common.save")}
            </Button>
          </div>
        </form>
      </Form>
      <TableToolbar
        search={table.search}
        searchPlaceholder={t("admin.credentials.search")}
        onSearchChange={table.setSearch}
      >
        <TableFilterSelect
          label={t("common.state")}
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
                {t("admin.credentials.name")}
              </SortableTableHead>
              <SortableTableHead
                field="username"
                sortBy={table.sortBy}
                sortOrder={table.sortOrder}
                onSort={table.toggleSort}
              >
                {t("admin.credentials.username")}
              </SortableTableHead>
              <SortableTableHead
                field="is_active"
                sortBy={table.sortBy}
                sortOrder={table.sortOrder}
                onSort={table.toggleSort}
              >
                {t("common.state")}
              </SortableTableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {credentials.map((item) => (
              <TableRow key={item.id}>
                <TableCell>{item.name}</TableCell>
                <TableCell>{item.username}</TableCell>
                <TableCell>{activeLabel(t, item.is_active)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      ) : (
        <EmptyState
          label={
            credentialsQuery.isLoading
              ? t("admin.credentials.loading")
              : t("admin.credentials.empty")
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
  const { t } = useTranslation();
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
  const importSchema = useMemo(() => createImportSchema(t), [t]);
  const booleanFilterOptions = useMemo(
    () => [
      { value: "true", label: booleanLabel(t, true) },
      { value: "false", label: booleanLabel(t, false) },
    ],
    [t],
  );
  const importStatusOptions = importStatusValues.map((value) => ({
    value,
    label: statusLabel(t, value),
  }));
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
      title={t("admin.imports.title")}
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
            label={t("admin.imports.importRecordsJson")}
            name="recordsJson"
            placeholder={sample}
          />
          <Button
            className="w-fit"
            disabled={importMutation.isPending}
            type="submit"
          >
            <Database size={16} />
            {t("common.import")}
          </Button>
        </form>
      </Form>
      <div className="grid gap-5 lg:grid-cols-2">
        <div>
          <h3 className="mb-2 text-sm font-semibold">
            {t("admin.imports.switches")}
          </h3>
          <TableToolbar
            search={switchTable.search}
            searchPlaceholder={t("admin.imports.searchSwitches")}
            onSearchChange={switchTable.setSearch}
          >
            <TableFilterSelect
              label={t("admin.imports.enabled")}
              options={booleanFilterOptions}
              value={switchTable.filters.is_enabled}
              onValueChange={(value) =>
                switchTable.setFilter("is_enabled", value)
              }
            />
            <TableFilterSelect
              label={t("admin.imports.validated")}
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
                    {t("admin.credentials.name")}
                  </SortableTableHead>
                  <SortableTableHead
                    field="management_ip"
                    sortBy={switchTable.sortBy}
                    sortOrder={switchTable.sortOrder}
                    onSort={switchTable.toggleSort}
                  >
                    {t("admin.imports.managementIp")}
                  </SortableTableHead>
                  <TableHead>{t("admin.imports.networks")}</TableHead>
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
            <EmptyState label={t("admin.imports.noSwitches")} />
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
          <h3 className="mb-2 text-sm font-semibold">
            {t("admin.imports.batches")}
          </h3>
          <TableToolbar
            search={importTable.search}
            searchPlaceholder={t("admin.imports.searchImports")}
            onSearchChange={importTable.setSearch}
          >
            <TableFilterSelect
              label={t("jobs.status")}
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
                    {t("jobs.status")}
                  </SortableTableHead>
                  <SortableTableHead
                    field="created_at"
                    sortBy={importTable.sortBy}
                    sortOrder={importTable.sortOrder}
                    onSort={importTable.toggleSort}
                  >
                    {t("jobs.created")}
                  </SortableTableHead>
                  <TableHead>{t("admin.imports.summary")}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {imports.map((item) => (
                  <TableRow key={item.id}>
                    <TableCell>{statusLabel(t, item.status)}</TableCell>
                    <TableCell>{formatDate(item.created_at)}</TableCell>
                    <TableCell>
                      <code>{pretty(item.summary)}</code>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <EmptyState label={t("admin.imports.noImports")} />
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
  const { t } = useTranslation();
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
  const commandProfileSchema = useMemo(() => createCommandProfileSchema(t), [t]);
  const activeFilterOptions = useMemo(
    () => [
      { value: "true", label: activeLabel(t, true) },
      { value: "false", label: activeLabel(t, false) },
    ],
    [t],
  );
  const form = useForm<CommandProfileFormValues>({
    defaultValues: defaultCommandProfileForm,
  });
  const saveMutation = useMutation({
    mutationFn: (values: CommandProfileFormValues) => {
      const parsed = commandProfileSchema.safeParse(values);
      if (!parsed.success) {
        throw new Error(
          parsed.error.issues[0]?.message ??
            t("validation.invalidCommandProfile"),
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
      setFormError(
        err instanceof Error ? err.message : t("admin.profiles.saveFailed"),
      ),
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
    ["loginPromptPatterns", t("admin.profiles.loginPromptPatterns")],
    ["commandTemplates", t("admin.profiles.commandTemplates")],
    ["promptPatterns", t("admin.profiles.promptPatterns")],
    ["paginationRules", t("admin.profiles.paginationRules")],
    ["parserRules", t("admin.profiles.parserRules")],
    ["errorPatterns", t("admin.profiles.errorPatterns")],
    ["successPatterns", t("admin.profiles.successPatterns")],
  ];

  return (
    <AdminPanel
      title={t("admin.profiles.title")}
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
              label={t("admin.profiles.profileName")}
              name="name"
              placeholder={t("admin.profiles.profileName")}
            />
            <InputField
              control={form.control}
              label={t("admin.profiles.description")}
              name="description"
              placeholder={t("admin.profiles.description")}
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
                <FormLabel>{t("common.active")}</FormLabel>
              </FormItem>
            )}
          />
          <div className="flex flex-wrap gap-2">
            <Button disabled={saveMutation.isPending} type="submit">
              <TerminalSquare size={16} />
              {editingId ? t("common.saveChanges") : t("common.create")}
            </Button>
            {editingId ? (
              <Button type="button" variant="secondary" onClick={resetForm}>
                {t("common.cancelEdit")}
              </Button>
            ) : null}
          </div>
        </form>
      </Form>
      <TableToolbar
        search={table.search}
        searchPlaceholder={t("admin.profiles.search")}
        onSearchChange={table.setSearch}
      >
        <TableFilterSelect
          label={t("common.state")}
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
                {t("admin.credentials.name")}
              </SortableTableHead>
              <TableHead>{t("admin.profiles.templates")}</TableHead>
              <SortableTableHead
                field="is_active"
                sortBy={table.sortBy}
                sortOrder={table.sortOrder}
                onSort={table.toggleSort}
              >
                {t("common.state")}
              </SortableTableHead>
              <TableHead>{t("common.actions")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {profiles.map((item) => (
              <TableRow key={item.id}>
                <TableCell>{item.name}</TableCell>
                <TableCell>
                  {Object.keys(item.command_templates).join(", ")}
                </TableCell>
                <TableCell>{activeLabel(t, item.is_active)}</TableCell>
                <TableCell>
                  <Button
                    type="button"
                    variant="secondary"
                    onClick={() => startEdit(item)}
                  >
                    {t("common.edit")}
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      ) : (
        <EmptyState
          label={
            profilesQuery.isLoading
              ? t("admin.profiles.loading")
              : t("admin.profiles.empty")
          }
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
  const { t } = useTranslation();
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
  const translatedAuditActionOptions = auditActionOptions.map((option) => ({
    ...option,
    label: statusLabel(t, option.value),
  }));
  const translatedAuditTargetOptions = auditTargetOptions.map((option) => ({
    ...option,
    label: statusLabel(t, option.value),
  }));

  return (
    <AdminPanel
      title={t("admin.audit.title")}
      icon={<ClipboardList size={18} />}
      error={auditQuery.error?.message}
    >
      <TableToolbar
        search={table.search}
        searchPlaceholder={t("admin.audit.search")}
        onSearchChange={table.setSearch}
      >
        <TableFilterSelect
          label={t("admin.audit.action")}
          options={translatedAuditActionOptions}
          value={table.filters.action}
          onValueChange={(value) => table.setFilter("action", value)}
        />
        <TableFilterSelect
          label={t("admin.audit.target")}
          options={translatedAuditTargetOptions}
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
                {t("admin.audit.time")}
              </SortableTableHead>
              <SortableTableHead
                field="action"
                sortBy={table.sortBy}
                sortOrder={table.sortOrder}
                onSort={table.toggleSort}
              >
                {t("admin.audit.action")}
              </SortableTableHead>
              <SortableTableHead
                field="target_type"
                sortBy={table.sortBy}
                sortOrder={table.sortOrder}
                onSort={table.toggleSort}
              >
                {t("admin.audit.target")}
              </SortableTableHead>
              <TableHead>{t("admin.audit.payload")}</TableHead>
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
          label={
            auditQuery.isLoading
              ? t("admin.audit.loading")
              : t("admin.audit.empty")
          }
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
