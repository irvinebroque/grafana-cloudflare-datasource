import {
  defaultAnalyticsSQL,
  defaultGraphQL,
  defaultQuery,
  defaultSQL,
  defaultTelemetry,
  type CloudflareQuery,
  type QueryType,
} from "./types";

export function migrateQuery(query: CloudflareQuery): CloudflareQuery {
  if (query.queryType === "analytics" && query.analyticsSql === undefined)
    return { ...query, analyticsSql: query.sql || defaultAnalyticsSQL, sql: defaultSQL };

  return query;
}

export function queryForMode(stored: CloudflareQuery, type: QueryType): CloudflareQuery {
  const query = migrateQuery(stored);

  const analyticsSql =
    query.analyticsSql ??
    (query.queryType === "analytics" ? query.sql : undefined) ??
    defaultAnalyticsSQL;

  const sql = query.sql || defaultSQL;

  const base: CloudflareQuery = {
    ...query,
    queryType: type,
    sql,
    analyticsSql,
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

  if (type === "builder")
    return { ...base, ...defaultQuery, timeField: "timestamp", sampled: true };

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
