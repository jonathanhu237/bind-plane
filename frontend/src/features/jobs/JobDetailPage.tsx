import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Activity, ListRestart } from "lucide-react";
import { useNavigate, useParams } from "react-router-dom";

import { apiRequest } from "@/api/client";
import { useJob, useToken } from "@/api/hooks";
import { queryKeys } from "@/api/queryKeys";
import { reasonLabels } from "@/api/types";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { EmptyState, StatusBadge } from "@/features/shared/status";
import { formatDate, pretty } from "@/lib/utils";

export function JobDetailPage() {
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
    return <EmptyState label="No job selected" />;
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Activity size={18} />
          Job detail
        </CardTitle>
      </CardHeader>
      <CardContent>
        {jobQuery.error ? <Alert>{jobQuery.error.message}</Alert> : null}
        {retryMutation.error ? <Alert>{retryMutation.error.message}</Alert> : null}
        {job ? (
          <div className="grid gap-5">
            <div className="flex flex-wrap gap-2">
              <StatusBadge value={job.status} />
              <StatusBadge value={job.phase} />
              {job.force ? <StatusBadge value="force" /> : null}
            </div>
            <dl className="grid gap-3 text-sm md:grid-cols-3">
              {[
                ["Target", job.target_ip],
                ["Switch", `${job.switch.name} (${job.switch.management_ip})`],
                ["Operator", job.operator.username],
                ["Reason", reasonLabels[job.reason]],
                ["Created", formatDate(job.created_at)],
                ["Finished", formatDate(job.finished_at)],
              ].map(([term, value]) => (
                <div key={term} className="rounded-md border bg-muted/30 p-3">
                  <dt className="text-xs uppercase text-muted-foreground">{term}</dt>
                  <dd className="mt-1 break-words font-medium">{value}</dd>
                </div>
              ))}
            </dl>
            {job.error_message ? <Alert>{job.error_message}</Alert> : null}
            <div className="grid gap-4 lg:grid-cols-3">
              <div>
                <h3 className="mb-2 text-sm font-semibold">Before</h3>
                <pre>{pretty(job.before_state)}</pre>
              </div>
              <div>
                <h3 className="mb-2 text-sm font-semibold">After</h3>
                <pre>{pretty(job.after_state)}</pre>
              </div>
              <div>
                <h3 className="mb-2 text-sm font-semibold">Result</h3>
                <pre>{pretty(job.result)}</pre>
              </div>
            </div>
            {job.raw_output ? (
              <details>
                <summary className="cursor-pointer text-sm font-medium">Raw output</summary>
                <pre className="mt-2">
                  {[job.raw_output.before, job.raw_output.release, job.raw_output.after]
                    .filter(Boolean)
                    .join("\n\n")}
                </pre>
              </details>
            ) : null}
            {job.kind === "release" &&
            ["failed", "timeout", "needs_manual_confirmation"].includes(job.status) ? (
              <Button
                className="w-fit"
                disabled={retryMutation.isPending}
                type="button"
                variant="secondary"
                onClick={() => retryMutation.mutate()}
              >
                <ListRestart size={16} />
                Retry
              </Button>
            ) : null}
          </div>
        ) : (
          <EmptyState label="Loading job" />
        )}
      </CardContent>
    </Card>
  );
}
