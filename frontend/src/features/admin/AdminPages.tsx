import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ClipboardList, Database, KeyRound, Network, TerminalSquare, UserCog, Users } from "lucide-react";
import { useMemo, useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { z } from "zod";

import { apiRequest } from "@/api/client";
import { useToken } from "@/api/hooks";
import { queryKeys } from "@/api/queryKeys";
import type { AuditLog, CommandProfile, Credential, ImportBatch, RoleName, SwitchRecord, UserRead } from "@/api/types";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { FormField } from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { EmptyState } from "@/features/shared/status";
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
        {error ? <Alert>{error}</Alert> : null}
        {children}
      </CardContent>
    </Card>
  );
}

export function UsersAdminPage() {
  const token = useToken();
  const queryClient = useQueryClient();
  const [resetPasswords, setResetPasswords] = useState<Record<string, string>>({});
  const [resetErrors, setResetErrors] = useState<Record<string, string>>({});
  const usersQuery = useQuery({
    queryKey: queryKeys.users,
    queryFn: () => apiRequest<UserRead[]>("/admin/users", token),
  });
  const form = useForm<z.infer<typeof userSchema>>({
    resolver: zodResolver(userSchema),
    defaultValues: { username: "", displayName: "", password: "", role: "operator" },
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
    mutationFn: ({ username, password }: { username: string; password: string }) =>
      apiRequest<UserRead>(`/admin/users/${username}/reset-password`, token, {
        method: "POST",
        body: JSON.stringify({ password }),
      }),
    onSuccess: async (_, variables) => {
      setResetPasswords((current) => ({ ...current, [variables.username]: "" }));
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
      error={usersQuery.error?.message ?? createMutation.error?.message ?? resetMutation.error?.message}
    >
      <form className="grid gap-3 md:grid-cols-5" onSubmit={form.handleSubmit((values) => createMutation.mutate(values))}>
        <FormField label="Username" error={form.formState.errors.username}>
          <Input placeholder="Username" {...form.register("username")} />
        </FormField>
        <FormField label="Display name" error={form.formState.errors.displayName}>
          <Input placeholder="Display name" {...form.register("displayName")} />
        </FormField>
        <FormField label="Initial password" error={form.formState.errors.password}>
          <Input placeholder="Initial password" type="password" {...form.register("password")} />
        </FormField>
        <FormField label="Role" error={form.formState.errors.role}>
          <Select {...form.register("role")}>
            <option value="operator">operator</option>
            <option value="admin">admin</option>
          </Select>
        </FormField>
        <div className="flex items-end">
          <Button disabled={createMutation.isPending} type="submit">
            <UserCog size={16} />
            Create
          </Button>
        </div>
      </form>
      {(usersQuery.data ?? []).length ? (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Username</TableHead>
              <TableHead>Roles</TableHead>
              <TableHead>State</TableHead>
              <TableHead>Reset password</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {(usersQuery.data ?? []).map((item) => (
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
                    <p className="mt-1 text-xs text-destructive">{resetErrors[item.username]}</p>
                  ) : null}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      ) : (
        <EmptyState label={usersQuery.isLoading ? "Loading users" : "No users"} />
      )}
    </AdminPanel>
  );
}

export function CredentialsAdminPage() {
  const token = useToken();
  const queryClient = useQueryClient();
  const credentialsQuery = useQuery({
    queryKey: queryKeys.credentials,
    queryFn: () => apiRequest<Credential[]>("/admin/credentials", token),
  });
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
      <form className="grid gap-3 md:grid-cols-5" onSubmit={form.handleSubmit((values) => createMutation.mutate(values))}>
        <FormField label="Name" error={form.formState.errors.name}>
          <Input placeholder="Name" {...form.register("name")} />
        </FormField>
        <FormField label="Username" error={form.formState.errors.username}>
          <Input placeholder="Username" {...form.register("username")} />
        </FormField>
        <FormField label="Password" error={form.formState.errors.password}>
          <Input placeholder="Password" type="password" {...form.register("password")} />
        </FormField>
        <FormField label="Enable secret" error={form.formState.errors.secret}>
          <Input placeholder="Enable secret" type="password" {...form.register("secret")} />
        </FormField>
        <div className="flex items-end">
          <Button disabled={createMutation.isPending} type="submit">
            <KeyRound size={16} />
            Save
          </Button>
        </div>
      </form>
      {(credentialsQuery.data ?? []).length ? (
        <Table>
          <TableBody>
            {(credentialsQuery.data ?? []).map((item) => (
              <TableRow key={item.id}>
                <TableCell>{item.name}</TableCell>
                <TableCell>{item.username}</TableCell>
                <TableCell>{item.is_active ? "active" : "inactive"}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      ) : (
        <EmptyState label={credentialsQuery.isLoading ? "Loading credentials" : "No credentials"} />
      )}
    </AdminPanel>
  );
}

export function ImportsAdminPage() {
  const token = useToken();
  const queryClient = useQueryClient();
  const credentialsQuery = useQuery({
    queryKey: queryKeys.credentials,
    queryFn: () => apiRequest<Credential[]>("/admin/credentials", token),
  });
  const profilesQuery = useQuery({
    queryKey: queryKeys.commandProfiles,
    queryFn: () => apiRequest<CommandProfile[]>("/admin/command-profiles", token),
  });
  const switchesQuery = useQuery({
    queryKey: queryKeys.switches,
    queryFn: () => apiRequest<SwitchRecord[]>("/admin/switches", token),
  });
  const importsQuery = useQuery({
    queryKey: queryKeys.imports,
    queryFn: () => apiRequest<ImportBatch[]>("/admin/imports", token),
  });
  const form = useForm<z.infer<typeof importSchema>>({
    resolver: zodResolver(importSchema),
    defaultValues: { recordsJson: "[]" },
  });
  const importMutation = useMutation({
    mutationFn: (values: z.infer<typeof importSchema>) =>
      apiRequest<ImportBatch>("/admin/imports/switch-networks", token, {
        method: "POST",
        body: JSON.stringify({ source_filename: "manual-json", records: JSON.parse(values.recordsJson) }),
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
            credential_id: credentialsQuery.data?.[0]?.id ?? "credential-uuid",
            command_profile_id: profilesQuery.data?.[0]?.id ?? "profile-uuid",
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
      <form className="grid gap-3" onSubmit={form.handleSubmit((values) => importMutation.mutate(values))}>
        <FormField label="Import records JSON" error={form.formState.errors.recordsJson}>
          <Textarea className="font-mono text-xs" placeholder={sample} {...form.register("recordsJson")} />
        </FormField>
        <Button className="w-fit" disabled={importMutation.isPending} type="submit">
          <Database size={16} />
          Import
        </Button>
      </form>
      <div className="grid gap-5 lg:grid-cols-2">
        <div>
          <h3 className="mb-2 text-sm font-semibold">Switches</h3>
          {(switchesQuery.data ?? []).length ? (
            <Table>
              <TableBody>
                {(switchesQuery.data ?? []).map((item) => (
                  <TableRow key={item.id}>
                    <TableCell>{item.name}</TableCell>
                    <TableCell>{item.management_ip}</TableCell>
                    <TableCell>{item.networks.map((network) => network.cidr).join(", ")}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <EmptyState label="No switches" />
          )}
        </div>
        <div>
          <h3 className="mb-2 text-sm font-semibold">Import batches</h3>
          {(importsQuery.data ?? []).length ? (
            <Table>
              <TableBody>
                {(importsQuery.data ?? []).map((item) => (
                  <TableRow key={item.id}>
                    <TableCell>{item.status}</TableCell>
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
  const profilesQuery = useQuery({
    queryKey: queryKeys.commandProfiles,
    queryFn: () => apiRequest<CommandProfile[]>("/admin/command-profiles", token),
  });
  const form = useForm<CommandProfileFormValues>({ defaultValues: defaultCommandProfileForm });
  const saveMutation = useMutation({
    mutationFn: (values: CommandProfileFormValues) => {
      const parsed = commandProfileSchema.safeParse(values);
      if (!parsed.success) {
        throw new Error(parsed.error.issues[0]?.message ?? "Invalid command profile");
      }
      return apiRequest<CommandProfile>(
        editingId ? `/admin/command-profiles/${editingId}` : "/admin/command-profiles",
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
      await queryClient.invalidateQueries({ queryKey: queryKeys.commandProfiles });
    },
    onError: (err) => setFormError(err instanceof Error ? err.message : "Save profile failed"),
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
      <form className="grid gap-4" onSubmit={form.handleSubmit((values) => saveMutation.mutate(values))}>
        <div className="grid gap-3 md:grid-cols-2">
          <FormField label="Profile name">
            <Input placeholder="Profile name" {...form.register("name")} />
          </FormField>
          <FormField label="Description">
            <Input placeholder="Description" {...form.register("description")} />
          </FormField>
        </div>
        <div className="grid gap-3 lg:grid-cols-2">
          {textFields.map(([field, label]) => (
            <FormField key={field} label={label}>
              <Textarea className="min-h-36 font-mono text-xs" {...form.register(field)} />
            </FormField>
          ))}
        </div>
        <Controller
          control={form.control}
          name="isActive"
          render={({ field }) => (
            <label className="flex items-center gap-2 text-sm">
              <Checkbox checked={field.value} onCheckedChange={(checked) => field.onChange(Boolean(checked))} />
              Active
            </label>
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
      {(profilesQuery.data ?? []).length ? (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Templates</TableHead>
              <TableHead>State</TableHead>
              <TableHead>Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {(profilesQuery.data ?? []).map((item) => (
              <TableRow key={item.id}>
                <TableCell>{item.name}</TableCell>
                <TableCell>{Object.keys(item.command_templates).join(", ")}</TableCell>
                <TableCell>{item.is_active ? "active" : "inactive"}</TableCell>
                <TableCell>
                  <Button type="button" variant="secondary" onClick={() => startEdit(item)}>
                    Edit
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      ) : (
        <EmptyState label={profilesQuery.isLoading ? "Loading profiles" : "No profiles"} />
      )}
    </AdminPanel>
  );
}

export function AuditLogsPage() {
  const token = useToken();
  const auditQuery = useQuery({
    queryKey: queryKeys.audit,
    queryFn: () => apiRequest<AuditLog[]>("/audit", token),
  });

  return (
    <AdminPanel title="Audit logs" icon={<ClipboardList size={18} />} error={auditQuery.error?.message}>
      {(auditQuery.data ?? []).length ? (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Time</TableHead>
              <TableHead>Action</TableHead>
              <TableHead>Target</TableHead>
              <TableHead>Payload</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {(auditQuery.data ?? []).map((log) => (
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
        <EmptyState label={auditQuery.isLoading ? "Loading audit logs" : "No audit logs"} />
      )}
    </AdminPanel>
  );
}
