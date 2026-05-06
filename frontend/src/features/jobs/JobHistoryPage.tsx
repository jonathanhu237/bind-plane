import { useQuery } from "@tanstack/react-query";
import { FileClock, RefreshCcw } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";

import { apiRequest } from "@/api/client";
import { withListParams } from "@/api/listParams";
import { useToken } from "@/api/hooks";
import { queryKeys } from "@/api/queryKeys";
import type { PaginatedResponse, ReleaseJob } from "@/api/types";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
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
import { EmptyState, StatusBadge } from "@/features/shared/status";
import { useTableState } from "@/features/shared/tableState";
import { statusLabel } from "@/i18n/labels";
import { formatDate } from "@/lib/utils";

const jobFilterKeys = ["status", "kind", "force"];
const jobStatusValues = [
  "queued",
  "running",
  "waiting_confirmation",
  "succeeded",
  "failed",
  "timeout",
  "needs_manual_confirmation",
  "cancelled",
];

export function JobHistoryPage() {
  const { t } = useTranslation();
  const token = useToken();
  const navigate = useNavigate();
  const table = useTableState({
    defaultSortBy: "created_at",
    defaultSortOrder: "desc",
    filterKeys: jobFilterKeys,
  });
  const jobsQuery = useQuery({
    queryKey: queryKeys.jobsList(table.apiParams),
    queryFn: () =>
      apiRequest<PaginatedResponse<ReleaseJob>>(
        withListParams("/releases/jobs", table.apiParams),
        token,
      ),
  });
  const jobsPage = jobsQuery.data;
  const jobs = jobsPage?.items ?? [];
  const jobStatusOptions = jobStatusValues.map((value) => ({
    value,
    label: statusLabel(t, value),
  }));
  const jobKindOptions = [
    { value: "pre_release_query", label: statusLabel(t, "pre_release_query") },
    { value: "release", label: statusLabel(t, "release") },
  ];
  const forceOptions = [
    { value: "true", label: t("jobs.forced") },
    { value: "false", label: t("jobs.normal") },
  ];

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between">
        <CardTitle className="flex items-center gap-2">
          <FileClock size={18} />
          {t("jobs.historyTitle")}
        </CardTitle>
        <Button
          size="icon"
          type="button"
          variant="outline"
          onClick={() => void jobsQuery.refetch()}
        >
          <RefreshCcw size={16} />
        </Button>
      </CardHeader>
      <CardContent>
        {jobsQuery.error ? (
          <Alert variant="destructive">{jobsQuery.error.message}</Alert>
        ) : null}
        <TableToolbar
          search={table.search}
          searchPlaceholder={t("jobs.search")}
          onSearchChange={table.setSearch}
        >
          <TableFilterSelect
            label={t("jobs.status")}
            options={jobStatusOptions}
            value={table.filters.status}
            onValueChange={(value) => table.setFilter("status", value)}
          />
          <TableFilterSelect
            label={t("jobs.kind")}
            options={jobKindOptions}
            value={table.filters.kind}
            onValueChange={(value) => table.setFilter("kind", value)}
          />
          <TableFilterSelect
            label={t("jobs.force")}
            options={forceOptions}
            value={table.filters.force}
            onValueChange={(value) => table.setFilter("force", value)}
          />
        </TableToolbar>
        {jobs.length ? (
          <Table>
            <TableHeader>
              <TableRow>
                <SortableTableHead
                  field="target_ip"
                  sortBy={table.sortBy}
                  sortOrder={table.sortOrder}
                  onSort={table.toggleSort}
                >
                  {t("jobs.target")}
                </SortableTableHead>
                <TableHead>{t("jobs.switch")}</TableHead>
                <SortableTableHead
                  field="status"
                  sortBy={table.sortBy}
                  sortOrder={table.sortOrder}
                  onSort={table.toggleSort}
                >
                  {t("jobs.status")}
                </SortableTableHead>
                <SortableTableHead
                  field="created_at"
                  sortBy={table.sortBy}
                  sortOrder={table.sortOrder}
                  onSort={table.toggleSort}
                >
                  {t("jobs.created")}
                </SortableTableHead>
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
          <EmptyState
            label={
              jobsQuery.isLoading ? t("jobs.loadingJobs") : t("jobs.noJobs")
            }
          />
        )}
        {jobsPage ? (
          <TablePagination
            page={jobsPage.page}
            pageCount={jobsPage.page_count}
            pageSize={jobsPage.page_size}
            total={jobsPage.total}
            onPageChange={table.setPage}
            onPageSizeChange={table.setPageSize}
          />
        ) : null}
      </CardContent>
    </Card>
  );
}
