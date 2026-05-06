import { ArrowDown, ArrowUp, ChevronsUpDown, Search } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { TableHead } from "@/components/ui/table";
import type { SortOrder } from "@/api/types";

const allValue = "__all__";
const pageSizeOptions = [10, 25, 50, 100];

export function TableToolbar({
  search,
  searchPlaceholder,
  onSearchChange,
  children,
}: {
  search: string;
  searchPlaceholder: string;
  onSearchChange: (value: string) => void;
  children?: React.ReactNode;
}) {
  const { t } = useTranslation();
  return (
    <div className="mb-3 flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
      <div className="relative md:w-72">
        <Search className="pointer-events-none absolute left-2 top-2.5 h-4 w-4 text-muted-foreground" />
        <Input
          aria-label={t("common.searchTable")}
          className="pl-8"
          placeholder={searchPlaceholder}
          value={search}
          onChange={(event) => onSearchChange(event.target.value)}
        />
      </div>
      {children ? (
        <div className="flex flex-wrap items-center gap-2">{children}</div>
      ) : null}
    </div>
  );
}

export function TableFilterSelect({
  label,
  value,
  options,
  onValueChange,
}: {
  label: string;
  value: string;
  options: Array<{ value: string; label: string }>;
  onValueChange: (value: string) => void;
}) {
  const { t } = useTranslation();
  const allLabel = t("common.all", { label: label.toLowerCase() });
  return (
    <Select
      value={value || allValue}
      onValueChange={(nextValue) =>
        onValueChange(nextValue === allValue ? "" : nextValue)
      }
    >
      <SelectTrigger aria-label={label} className="w-[160px]">
        <SelectValue placeholder={label} />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={allValue}>{allLabel}</SelectItem>
        {options.map((option) => (
          <SelectItem key={option.value} value={option.value}>
            {option.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

export function SortableTableHead({
  field,
  sortBy,
  sortOrder,
  onSort,
  children,
}: {
  field: string;
  sortBy: string;
  sortOrder: SortOrder;
  onSort: (field: string) => void;
  children: React.ReactNode;
}) {
  const active = sortBy === field;
  const Icon = active
    ? sortOrder === "asc"
      ? ArrowUp
      : ArrowDown
    : ChevronsUpDown;
  return (
    <TableHead>
      <Button
        className="-ml-3 h-8 px-3"
        type="button"
        variant="ghost"
        onClick={() => onSort(field)}
      >
        {children}
        <Icon className="h-4 w-4" />
      </Button>
    </TableHead>
  );
}

export function TablePagination({
  page,
  pageSize,
  total,
  pageCount,
  onPageChange,
  onPageSizeChange,
}: {
  page: number;
  pageSize: number;
  total: number;
  pageCount: number;
  onPageChange: (page: number) => void;
  onPageSizeChange: (pageSize: number) => void;
}) {
  const { t } = useTranslation();
  const firstItem = total ? (page - 1) * pageSize + 1 : 0;
  const lastItem = Math.min(page * pageSize, total);
  return (
    <div className="mt-3 flex flex-col gap-3 text-sm text-muted-foreground md:flex-row md:items-center md:justify-between">
      <div>
        {total
          ? t("table.resultRange", {
              first: firstItem,
              last: lastItem,
              total,
            })
          : t("table.noResults")}
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <span>{t("table.rows")}</span>
        <Select
          value={String(pageSize)}
          onValueChange={(value) => onPageSizeChange(Number(value))}
        >
          <SelectTrigger
            aria-label={t("table.rowsPerPage")}
            className="w-[88px]"
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {pageSizeOptions.map((option) => (
              <SelectItem key={option} value={String(option)}>
                {option}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button
          disabled={page <= 1}
          size="sm"
          type="button"
          variant="outline"
          onClick={() => onPageChange(page - 1)}
        >
          {t("table.previous")}
        </Button>
        <span className="min-w-20 text-center">
          {t("table.pageOf", { page, pageCount: Math.max(pageCount, 1) })}
        </span>
        <Button
          disabled={pageCount === 0 || page >= pageCount}
          size="sm"
          type="button"
          variant="outline"
          onClick={() => onPageChange(page + 1)}
        >
          {t("table.next")}
        </Button>
      </div>
    </div>
  );
}
