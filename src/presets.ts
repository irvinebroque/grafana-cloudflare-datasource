import { defaultQuery, type CloudflareQuery } from "./types";

interface Preset {
  label: string;
  query: Partial<CloudflareQuery>;
}

function metric(
  dataset: string,
  field = "",
  aggregation = "count",
  groupBy: string[] = [],
): Partial<CloudflareQuery> {
  return {
    ...defaultQuery,
    dataset,
    metrics: [{ aggregation, field, alias: aggregation === "count" ? "requests" : field }],
    groupBy,
  };
}

function logs(dataset: string): Partial<CloudflareQuery> {
  return {
    ...defaultQuery,
    dataset,
    format: "logs",
    metrics: [],
    groupBy: [],
    limit: 100,
  };
}

function graphMetric(
  dataset: string,
  column: string,
  groupBy: string[] = [],
): Partial<CloudflareQuery> {
  return {
    ...defaultQuery,
    queryType: "graphql-builder",
    dataset,
    scope: "account",
    timeField: "",
    columns: [column],
    metrics: [],
    groupBy,
  };
}

export const presets: Preset[] = [
  {
    label: "Workers · requests",
    query: defaultQuery,
  },
  {
    label: "Workers · p99 CPU time (µs)",
    query: metric("events.workersInvocations", "cpuTimeUs", "p99", ["scriptName"]),
  },
  {
    label: "Workers · logs",
    query: logs("logs.workersLogs"),
  },
  {
    label: "Containers · logs",
    query: logs("logs.containerLogs"),
  },
  {
    label: "Cloudflare · distributed trace",
    query: {
      ...defaultQuery,
      dataset: "logs.traces",
      format: "traces",
      groupBy: [],
      metrics: [],
    },
  },
  {
    label: "HTTP · requests by status",
    query: metric("events.httpRequests", "", "count", ["edgeResponseStatus"]),
  },
  { label: "D1 · query latency (ms)", query: metric("events.d1Queries", "queryDurationMs", "avg") },
  { label: "R2 · operations", query: metric("events.r2CombinedOperations") },
  { label: "KV · requests", query: metric("events.kvRequests") },
  { label: "Queues · messages", query: metric("events.queuesMessages") },
  {
    label: "Workers AI · inference time (ms)",
    query: metric("events.aiInference", "inferenceTimeMs", "avg"),
  },
  {
    label: "Containers · resource metrics",
    query: graphMetric("containersMetricsAdaptiveGroups", "avg.cpuUtilization", [
      "dimensions.applicationId",
    ]),
  },
  {
    label: "AI Gateway · requests",
    query: graphMetric("aiGatewayRequestsAdaptiveGroups", "count", ["dimensions.gateway"]),
  },
  { label: "AI Search · requests", query: graphMetric("aiSearchAPIAdaptiveGroups", "count") },
  {
    label: "Durable Objects · SQL storage",
    query: graphMetric("durableObjectsSqlStorageGroups", "max.storedBytes"),
  },
  {
    label: "DNS · queries",
    query: graphMetric("dnsAnalyticsAdaptiveGroups", "count", ["dimensions.queryType"]),
  },
];
