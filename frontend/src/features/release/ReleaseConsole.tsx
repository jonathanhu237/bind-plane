import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ClipboardList, Play, RefreshCcw, TerminalSquare } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";

import { apiRequest } from "@/api/client";
import { withListParams } from "@/api/listParams";
import { queryKeys } from "@/api/queryKeys";
import type {
  PaginatedResponse,
  ReleaseJob,
  ReleasePreparation,
  SwitchRecord,
} from "@/api/types";
import { releaseReasonValues, terminalStatuses } from "@/api/types";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { InputField, SelectField } from "@/components/forms/fields";
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
} from "@/components/ui/form";
import { EmptyState, StatusBadge } from "@/features/shared/status";
import { booleanLabel, reasonLabel, statusLabel } from "@/i18n/labels";
import { useCurrentUser, useToken } from "@/api/hooks";
import { cn } from "@/lib/utils";

import {
  createReleasePrepareSchema,
  type ReleasePrepareValues,
} from "./schemas";

const automaticSwitchValue = "__automatic__";

async function fetchEnabledSwitchOptions(token: string | null) {
  const items: SwitchRecord[] = [];
  let page = 1;
  let pageCount = 1;

  do {
    const response = await apiRequest<PaginatedResponse<SwitchRecord>>(
      withListParams("/admin/switches", {
        page,
        pageSize: 200,
        sortBy: "name",
        filters: { is_enabled: true },
      }),
      token,
    );
    items.push(...response.items);
    pageCount = response.page_count;
    page += 1;
  } while (page <= pageCount);

  return items;
}

function preparationFromJob(
  job: ReleaseJob,
  seed: ReleasePreparation,
): ReleasePreparation {
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
        : job.status === "cancelled" &&
            preparationStatus === "stopped_no_record"
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

export function ReleaseConsole() {
  const { t } = useTranslation();
  const token = useToken();
  const userQuery = useCurrentUser();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [preparation, setPreparation] = useState<ReleasePreparation | null>(
    null,
  );
  const [preparationSeed, setPreparationSeed] =
    useState<ReleasePreparation | null>(null);
  const [preparationJobId, setPreparationJobId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const user = userQuery.data;
  const isAdmin = user?.roles.includes("admin") ?? false;
  const releasePrepareSchema = useMemo(() => createReleasePrepareSchema(t), [t]);
  const reasonOptions = useMemo(
    () =>
      releaseReasonValues.map((value) => ({
        value,
        label: reasonLabel(t, value),
      })),
    [t],
  );

  const form = useForm<ReleasePrepareValues>({
    resolver: zodResolver(releasePrepareSchema),
    defaultValues: {
      targetIp: "",
      reason: "temporary_test",
      ticketId: "",
      force: false,
      selectedSwitchId: automaticSwitchValue,
    },
  });

  const switchesQuery = useQuery({
    queryKey: queryKeys.switchOptions,
    queryFn: () => fetchEnabledSwitchOptions(token),
    enabled: Boolean(token && isAdmin),
  });

  const preparationJobQuery = useQuery({
    queryKey: queryKeys.job(preparationJobId),
    queryFn: () =>
      apiRequest<ReleaseJob>(`/releases/jobs/${preparationJobId}`, token),
    enabled: Boolean(token && preparationJobId && preparationSeed),
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status &&
        !terminalStatuses.has(status) &&
        status !== "waiting_confirmation"
        ? 2000
        : false;
    },
  });

  useEffect(() => {
    const job = preparationJobQuery.data;
    if (!job || !preparationSeed) {
      return;
    }
    const nextPreparation = preparationFromJob(job, preparationSeed);
    setPreparation(nextPreparation);
    form.setValue("force", nextPreparation.force);
    if (
      job.status === "waiting_confirmation" ||
      terminalStatuses.has(job.status)
    ) {
      setPreparationJobId(null);
    }
    if (job.status === "failed" || job.status === "timeout") {
      setError(job.error_message || t("release.preQueryFailed"));
    }
  }, [form, preparationJobQuery.data, preparationSeed, t]);

  const prepareMutation = useMutation({
    mutationFn: (values: ReleasePrepareValues) =>
      apiRequest<ReleasePreparation>("/releases/prepare", token, {
        method: "POST",
        body: JSON.stringify({
          target_ip: values.targetIp,
          reason: values.reason,
          force: values.force,
          selected_switch_id:
            values.force && values.selectedSwitchId !== automaticSwitchValue
              ? values.selectedSwitchId
              : undefined,
        }),
      }),
    onSuccess: (response) => {
      setPreparation(response);
      setPreparationSeed(response);
      setPreparationJobId(response.preparation_job_id);
      form.setValue("force", response.force);
      setError(null);
    },
    onError: (err) =>
      setError(err instanceof Error ? err.message : t("release.preparationFailed")),
  });

  const createJobMutation = useMutation({
    mutationFn: ({ forceOverride }: { forceOverride?: boolean }) => {
      const values = form.getValues();
      if (!preparation?.preparation_job_id) {
        throw new Error(t("release.preparationRequired"));
      }
      return apiRequest<{ job_id: string }>("/releases/jobs", token, {
        method: "POST",
        body: JSON.stringify({
          preparation_job_id: preparation.preparation_job_id,
          target_ip: preparation.target_ip,
          reason: preparation.reason ?? values.reason,
          ticket_id: values.ticketId || null,
          force: forceOverride ?? preparation.force,
          confirmed: true,
        }),
      });
    },
    onSuccess: async (response) => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.jobs });
      navigate(`/jobs/${response.job_id}`);
    },
    onError: (err) =>
      setError(err instanceof Error ? err.message : t("release.jobCreationFailed")),
  });

  const hasPreparedJob = Boolean(preparation?.preparation_job_id);
  const canForceStoppedNoRecord =
    isAdmin &&
    preparation?.status === "stopped_no_record" &&
    preparation.observation?.entry_type === "missing" &&
    Boolean(preparation.preparation_job_id);
  const displayedForce = preparation?.force || canForceStoppedNoRecord;
  const forceValue = form.watch("force");
  const showForcedSwitchSelector =
    isAdmin &&
    forceValue &&
    !hasPreparedJob &&
    (preparation?.status ?? null) !== "ready";
  const enabledSwitches = useMemo(
    () => switchesQuery.data ?? [],
    [switchesQuery.data],
  );
  const loading = prepareMutation.isPending || createJobMutation.isPending;

  function submit(values: ReleasePrepareValues) {
    setPreparation(null);
    setPreparationSeed(null);
    setPreparationJobId(null);
    prepareMutation.mutate(values);
  }

  return (
    <section className="grid gap-5 xl:grid-cols-[420px_1fr]">
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <TerminalSquare size={18} />
            {t("release.title")}
          </CardTitle>
        </CardHeader>
        <CardContent>
          <Form {...form}>
            <form className="grid gap-4" onSubmit={form.handleSubmit(submit)}>
              <InputField
                control={form.control}
                label={t("release.ipv4Address")}
                name="targetIp"
                placeholder="10.44.132.254"
              />
              <SelectField
                control={form.control}
                label={t("release.reason")}
                name="reason"
                options={reasonOptions}
              />
              <InputField
                control={form.control}
                label={t("release.ticketId")}
                name="ticketId"
              />
              {isAdmin ? (
                <FormField
                  control={form.control}
                  name="force"
                  render={({ field }) => (
                    <FormItem className="flex flex-row items-center gap-2 space-y-0">
                      <FormControl>
                        <Checkbox
                          checked={preparation?.force ?? field.value}
                          disabled={hasPreparedJob || loading}
                          onCheckedChange={(checked) =>
                            field.onChange(Boolean(checked))
                          }
                        />
                      </FormControl>
                      <FormLabel>{t("release.forceRelease")}</FormLabel>
                    </FormItem>
                  )}
                />
              ) : null}
              {showForcedSwitchSelector ? (
                <SelectField
                  control={form.control}
                  label={t("release.forcedSwitch")}
                  name="selectedSwitchId"
                  options={[
                    {
                      value: automaticSwitchValue,
                      label: t("release.automaticResolution"),
                    },
                    ...enabledSwitches.map((item) => ({
                      value: item.id,
                      label: `${item.name} (${item.management_ip})`,
                    })),
                  ]}
                />
              ) : null}
              {error ? <Alert variant="destructive">{error}</Alert> : null}
              <Button disabled={loading} type="submit">
                <RefreshCcw size={16} />
                {prepareMutation.isPending
                  ? t("release.preparing")
                  : t("release.prepare")}
              </Button>
            </form>
          </Form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <ClipboardList size={18} />
            {t("release.confirmation")}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {preparation ? (
            <div className="grid gap-4">
              <StatusBadge value={preparation.status} />
              <dl className="grid gap-3 text-sm md:grid-cols-2">
                {[
                  [t("release.target"), preparation.target_ip],
                  [
                    t("release.switch"),
                    preparation.resolved_switch
                      ? `${preparation.resolved_switch.name} (${preparation.resolved_switch.management_ip})`
                      : t("common.notResolved"),
                  ],
                  [
                    t("release.network"),
                    preparation.resolved_switch?.cidr ?? t("common.none"),
                  ],
                  [
                    t("release.currentState"),
                    preparation.observation
                      ? `${statusLabel(t, preparation.observation.entry_type)}${
                          preparation.observation.mac
                            ? `, ${preparation.observation.mac}`
                            : ""
                        }`
                      : t("common.unknown"),
                  ],
                  [
                    t("release.reason"),
                    reasonLabel(t, preparation.reason ?? form.getValues("reason")),
                  ],
                  [t("release.force"), booleanLabel(t, displayedForce)],
                ].map(([term, value], index) => (
                  <div key={term} className="rounded-md border bg-muted/30 p-3">
                    <dt className="text-xs uppercase text-muted-foreground">
                      {term}
                    </dt>
                    <dd
                      className={cn(
                        "mt-1 break-words font-medium",
                        index === 5 && displayedForce && "text-destructive",
                      )}
                    >
                      {value}
                    </dd>
                  </div>
                ))}
              </dl>
              {preparation.observation ? (
                <details>
                  <summary className="cursor-pointer text-sm font-medium">
                    {t("release.rawPreQueryOutput")}
                  </summary>
                  <pre className="mt-2">
                    {preparation.observation.raw_output}
                  </pre>
                </details>
              ) : null}
              <div className="flex flex-wrap gap-2">
                <Button
                  disabled={
                    loading ||
                    preparation.status !== "ready" ||
                    !preparation.preparation_job_id
                  }
                  type="button"
                  variant="destructive"
                  onClick={() => createJobMutation.mutate({})}
                >
                  <Play size={16} />
                  {t("release.createJob")}
                </Button>
                {canForceStoppedNoRecord ? (
                  <Button
                    disabled={loading}
                    type="button"
                    variant="destructive"
                    onClick={() =>
                      createJobMutation.mutate({ forceOverride: true })
                    }
                  >
                    <Play size={16} />
                    {t("release.forceReleaseJob")}
                  </Button>
                ) : null}
              </div>
            </div>
          ) : (
            <EmptyState label={t("release.noPreparation")} />
          )}
        </CardContent>
      </Card>
    </section>
  );
}
