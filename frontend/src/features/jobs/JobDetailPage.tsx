import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Activity, ListRestart } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";

import { apiRequest } from "@/api/client";
import { useJob, useToken } from "@/api/hooks";
import { queryKeys } from "@/api/queryKeys";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { EmptyState, StatusBadge } from "@/features/shared/status";
import { reasonLabel } from "@/i18n/labels";
import { formatDate, pretty } from "@/lib/utils";

export function JobDetailPage() {
  const { t } = useTranslation();
  const { jobId } = useParams();
  const token = useToken();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const jobQuery = useJob(jobId);
  const job = jobQuery.data;

  const retryMutation = useMutation({
    mutationFn: () =>
      apiRequest<{ job_id: string }>(`/releases/jobs/${job?.id}/retry`, token, {
        method: "POST",
        body: JSON.stringify({}),
      }),
    onSuccess: async (response) => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.jobs });
      navigate(`/jobs/${response.job_id}`);
    },
  });

  if (!jobId) {
    return <EmptyState label={t("jobs.noJobSelected")} />;
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Activity size={18} />
          {t("jobs.detailTitle")}
        </CardTitle>
      </CardHeader>
      <CardContent>
        {jobQuery.error ? (
          <Alert variant="destructive">{jobQuery.error.message}</Alert>
        ) : null}
        {retryMutation.error ? (
          <Alert variant="destructive">{retryMutation.error.message}</Alert>
        ) : null}
        {job ? (
          <div className="grid gap-5">
            <div className="flex flex-wrap gap-2">
              <StatusBadge value={job.status} />
              <StatusBadge value={job.phase} />
              {job.force ? <StatusBadge value="force" /> : null}
            </div>
            <dl className="grid gap-3 text-sm md:grid-cols-3">
              {[
                [t("jobs.target"), job.target_ip],
                [t("jobs.switch"), `${job.switch.name} (${job.switch.management_ip})`],
                [t("jobs.operator"), job.operator.username],
                [t("jobs.reason"), reasonLabel(t, job.reason)],
                [t("jobs.created"), formatDate(job.created_at)],
                [t("jobs.finished"), formatDate(job.finished_at)],
              ].map(([term, value]) => (
                <div key={term} className="rounded-md border bg-muted/30 p-3">
                  <dt className="text-xs uppercase text-muted-foreground">
                    {term}
                  </dt>
                  <dd className="mt-1 break-words font-medium">{value}</dd>
                </div>
              ))}
            </dl>
            {job.error_message ? (
              <Alert variant="destructive">{job.error_message}</Alert>
            ) : null}
            <div className="grid gap-4 lg:grid-cols-3">
              <div>
                <h3 className="mb-2 text-sm font-semibold">{t("jobs.before")}</h3>
                <pre>{pretty(job.before_state)}</pre>
              </div>
              <div>
                <h3 className="mb-2 text-sm font-semibold">{t("jobs.after")}</h3>
                <pre>{pretty(job.after_state)}</pre>
              </div>
              <div>
                <h3 className="mb-2 text-sm font-semibold">{t("jobs.result")}</h3>
                <pre>{pretty(job.result)}</pre>
              </div>
            </div>
            {job.raw_output ? (
              <details>
                <summary className="cursor-pointer text-sm font-medium">
                  {t("jobs.rawOutput")}
                </summary>
                <pre className="mt-2">
                  {[
                    job.raw_output.before,
                    job.raw_output.release,
                    job.raw_output.after,
                  ]
                    .filter(Boolean)
                    .join("\n\n")}
                </pre>
              </details>
            ) : null}
            {job.kind === "release" &&
            ["failed", "timeout", "needs_manual_confirmation"].includes(
              job.status,
            ) ? (
              <Button
                className="w-fit"
                disabled={retryMutation.isPending}
                type="button"
                variant="secondary"
                onClick={() => retryMutation.mutate()}
              >
                <ListRestart size={16} />
                {t("common.retry")}
              </Button>
            ) : null}
          </div>
        ) : (
          <EmptyState label={t("jobs.loadingJob")} />
        )}
      </CardContent>
    </Card>
  );
}
