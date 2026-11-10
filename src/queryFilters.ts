import type { QueryFilterOptions, ToggleFilterAction } from "@grafana/data";
import type { CloudflareQuery, QueryFilter } from "./types";

function canFilter(query: CloudflareQuery, key: string | undefined) {
  return (
    (query.queryType ?? "builder") === "builder" &&
    query.format === "logs" &&
    !!key &&
    /^[A-Za-z_][A-Za-z0-9_]*$/.test(key) &&
    key !== (query.timeField || "timestamp") &&
    key !== "attributes"
  );
}

function matches(filter: QueryFilter, options: QueryFilterOptions): boolean {
  return (
    filter.field === options.key &&
    filter.value === options.value &&
    (filter.literal === true || !filter.value.includes("$"))
  );
}

export function queryHasFilter(query: CloudflareQuery, options: QueryFilterOptions): boolean {
  return (
    canFilter(query, options.key) &&
    (query.filters ?? []).some((filter) => matches(filter, options) && filter.operator === "=")
  );
}

export function toggleQueryFilter(
  query: CloudflareQuery,
  action: ToggleFilterAction,
): CloudflareQuery {
  const { key, value } = action.options;

  if (!canFilter(query, key) || key === undefined || value === undefined) return query;

  const operator = action.type === "FILTER_FOR" ? "=" : "!=";
  const filters = query.filters ?? [];

  const exists = filters.some(
    (filter) => matches(filter, action.options) && filter.operator === operator,
  );

  const remaining = filters.filter(
    (filter) =>
      !matches(filter, action.options) || (filter.operator !== "=" && filter.operator !== "!="),
  );

  return {
    ...query,
    filters: exists ? remaining : [...remaining, { field: key, operator, value, literal: true }],
  };
}
