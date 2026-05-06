import { useMemo } from "react";
import { useSearchParams } from "react-router-dom";

import type { ListQueryParams, SortOrder } from "@/api/types";

type TableStateOptions = {
  defaultSortBy: string;
  defaultSortOrder?: SortOrder;
  defaultPageSize?: number;
  filterKeys?: string[];
  prefix?: string;
};

function parsePositiveInt(value: string | null, fallback: number) {
  const parsed = Number.parseInt(value ?? "", 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}

function parseSortOrder(value: string | null, fallback: SortOrder): SortOrder {
  return value === "asc" || value === "desc" ? value : fallback;
}

export function useTableState({
  defaultSortBy,
  defaultSortOrder = "asc",
  defaultPageSize = 25,
  filterKeys = [],
  prefix = "",
}: TableStateOptions) {
  const [searchParams, setSearchParams] = useSearchParams();
  const key = (name: string) => `${prefix}${name}`;
  const filters = useMemo(
    () =>
      Object.fromEntries(
        filterKeys.map((filterKey) => [
          filterKey,
          searchParams.get(key(filterKey)) ?? "",
        ]),
      ),
    [filterKeys, key, searchParams],
  );
  const page = parsePositiveInt(searchParams.get(key("page")), 1);
  const pageSize = parsePositiveInt(
    searchParams.get(key("page_size")),
    defaultPageSize,
  );
  const search = searchParams.get(key("search")) ?? "";
  const sortBy = searchParams.get(key("sort_by")) ?? defaultSortBy;
  const sortOrder = parseSortOrder(
    searchParams.get(key("sort_order")),
    defaultSortOrder,
  );

  function update(
    updates: Record<string, string | number | null | undefined>,
    options: { resetPage?: boolean } = {},
  ) {
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      for (const [name, value] of Object.entries(updates)) {
        const paramName = key(name);
        if (value === null || value === undefined || value === "") {
          next.delete(paramName);
        } else {
          next.set(paramName, String(value));
        }
      }
      if (options.resetPage) {
        next.delete(key("page"));
      }
      return next;
    });
  }

  function setPage(nextPage: number) {
    update({ page: Math.max(1, nextPage) }, { resetPage: false });
  }

  function setPageSize(nextPageSize: number) {
    update({ page_size: nextPageSize }, { resetPage: true });
  }

  function setSearch(nextSearch: string) {
    update({ search: nextSearch }, { resetPage: true });
  }

  function setFilter(name: string, value: string) {
    update({ [name]: value }, { resetPage: true });
  }

  function toggleSort(nextSortBy: string) {
    const nextSortOrder =
      sortBy === nextSortBy && sortOrder === "asc" ? "desc" : "asc";
    update(
      { sort_by: nextSortBy, sort_order: nextSortOrder },
      { resetPage: true },
    );
  }

  const apiParams = useMemo<ListQueryParams>(
    () => ({
      page,
      pageSize,
      search,
      sortBy,
      sortOrder,
      filters,
    }),
    [filters, page, pageSize, search, sortBy, sortOrder],
  );

  return {
    page,
    pageSize,
    search,
    sortBy,
    sortOrder,
    filters,
    apiParams,
    setPage,
    setPageSize,
    setSearch,
    setFilter,
    toggleSort,
  };
}
