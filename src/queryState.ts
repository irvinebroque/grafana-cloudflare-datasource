import {
  defaultAnalyticsSQL,
  defaultGraphQL,
  defaultQuery,
  defaultSQL,
  defaultTelemetry,
  type CloudflareQuery,
  type QueryType,
  type Dataset,
  type Metric,
} from "./types";

export function migrateQuery(query: CloudflareQuery): CloudflareQuery {
  if (query.queryType === "analytics" && query.analyticsSql === undefined)
    return { ...query, analyticsSql: query.sql || defaultAnalyticsSQL, sql: defaultSQL };

  return query;
}

export function queryForMode(stored: CloudflareQuery, type: QueryType): CloudflareQuery {
  const query = migrateQuery(stored);

  const base: CloudflareQuery = {
    ...query,
    queryType: type,
    sql: query.sql || defaultSQL,
    analyticsSql: query.analyticsSql ?? defaultAnalyticsSQL,
    dataset: undefined,
    metrics: [],
    groupBy: [],
    columns: [],
    filters: [],
    traceId: "",
    timeField: "",
    logVolume: false,
    volumeSource: undefined,
  };

  if (type === "builder") return { ...base, ...defaultQuery };

  if (type === "graphql")
    return {
      ...base,
      format: "timeseries",
      graphql: query.graphql || defaultGraphQL,
      dataPath: "viewer.accounts.workersInvocationsAdaptive",
      timeField: "dimensions.datetimeMinute",
    };

  if (type === "telemetry")
    return {
      ...base,
      scope: "account",
      format: "logs",
      telemetry: query.telemetry || defaultTelemetry,
      dataPath: "events.events",
      timeField: "timestamp",
    };

  return { ...base, format: "timeseries", scope: type === "analytics" ? "account" : base.scope };
}

export function defaultMetric(dataset: Dataset | undefined, alias: string): Metric {
  const states = dataset?.kind?.states;

  return {
    aggregation: states?.valid_aggregations[0] ?? "count",
    field: states
      ? (dataset?.columns?.find(
          (column) => /^(U?Int|Float)/.test(column.data_type) && column.name !== "sampleInterval",
        )?.name ?? "")
      : "",
    alias,
  };
}
