import type { CloudflareQuery } from "./types";

export function interpolateQuery(
  query: CloudflareQuery,
  replace: (value: string, format: string) => string,
): CloudflareQuery {
  return {
    ...query,
    accountId: replace(query.accountId ?? "", "raw"),
    zoneId: replace(query.zoneId ?? "", "raw"),
    dataset: replace(query.dataset ?? "", "raw"),
    traceId: replace(query.traceId ?? "", "raw"),
    sql: replace(query.sql ?? "", "sqlstring"),
    params: replace(query.params ?? "", "json"),
    variables: replace(query.variables ?? "", "json"),
    filters: query.filters?.map((filter) => ({ ...filter, value: replace(filter.value, "raw") })),
  };
}
