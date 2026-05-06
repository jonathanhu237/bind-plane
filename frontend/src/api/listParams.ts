import type { ListQueryParams } from "@/api/types";

export function buildListSearch(params: ListQueryParams) {
  const search = new URLSearchParams();
  search.set("page", String(params.page ?? 1));
  search.set("page_size", String(params.pageSize ?? 25));
  if (params.search) {
    search.set("search", params.search);
  }
  if (params.sortBy) {
    search.set("sort_by", params.sortBy);
  }
  if (params.sortOrder) {
    search.set("sort_order", params.sortOrder);
  }
  for (const [key, value] of Object.entries(params.filters ?? {})) {
    if (value !== undefined && value !== null && value !== "") {
      search.set(key, String(value));
    }
  }
  return search.toString();
}

export function withListParams(path: string, params: ListQueryParams) {
  return `${path}?${buildListSearch(params)}`;
}
