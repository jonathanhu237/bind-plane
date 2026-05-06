import { useQuery } from "@tanstack/react-query";
import { FileClock, RefreshCcw } from "lucide-react";
import { useNavigate } from "react-router-dom";

import { apiRequest } from "@/api/client";
import { useToken } from "@/api/hooks";
import { queryKeys } from "@/api/queryKeys";
import type { ReleaseJob } from "@/api/types";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { EmptyState, StatusBadge } from "@/features/shared/status";
import { formatDate } from "@/lib/utils";

export function JobHistoryPage() {
  const token = useToken();
  const navigate = useNavigate();
  const jobsQuery = useQuery({
    queryKey: queryKeys.jobs,
    queryFn: () => apiRequest<ReleaseJob[]>("/releases/jobs", token),
  });
  const jobs = jobsQuery.data ?? [];

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between">
        <CardTitle className="flex items-center gap-2">
          <FileClock size={18} />
          Job history
        </CardTitle>
        <Button size="icon" type="button" variant="outline" onClick={() => void jobsQuery.refetch()}>
          <RefreshCcw size={16} />
        </Button>
      </CardHeader>
      <CardContent>
        {jobsQuery.error ? <Alert>{jobsQuery.error.message}</Alert> : null}
        {jobs.length ? (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Target</TableHead>
                <TableHead>Switch</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Created</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {jobs.map((job) => (
                <TableRow
                  key={job.id}
                  className="cursor-pointer"
                  onClick={() => navigate(`/jobs/${job.id}`)}
                >
                  <TableCell>{job.target_ip}</TableCell>
                  <TableCell>{job.switch.name}</TableCell>
                  <TableCell>
                    <StatusBadge value={job.status} />
                  </TableCell>
                  <TableCell>{formatDate(job.created_at)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        ) : (
          <EmptyState label={jobsQuery.isLoading ? "Loading jobs" : "No jobs"} />
        )}
      </CardContent>
    </Card>
  );
}
