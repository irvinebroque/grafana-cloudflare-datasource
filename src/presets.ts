import type { CloudflareQuery } from "./types";

export interface Preset {
  label: string;
  query: Partial<CloudflareQuery>;
}

function metric(
  dataset: string,
  field: string,
  aggregation = "count",
  groupBy: string[] = [],
): Partial<CloudflareQuery> {
  return {
    queryType: "builder",
    scope: "account",
    timeField: "timestamp",
    sampled: true,
    dataset,
    format: "timeseries",
    metrics: [{ aggregation, field, alias: aggregation === "count" ? "requests" : field }],
    groupBy,
    filters: [],
    columns: [],
    traceId: "",
    limit: 1000,
  };
}

export const presets: Preset[] = [
  {
    label: "Workers · requests",
    query: metric("events.workersInvocations", "", "count", ["scriptName"]),
  },
  {
    label: "Workers · p99 CPU time (µs)",
    query: metric("events.workersInvocations", "cpuTimeUs", "p99", ["scriptName"]),
  },
  {
    label: "Workers · logs",
    query: {
      queryType: "builder",
      scope: "account",
      timeField: "timestamp",
      sampled: true,
      dataset: "logs.workersLogs",
      format: "logs",
      columns: [],
      filters: [],
      groupBy: [],
      metrics: [],
      traceId: "",
      limit: 100,
    },
  },
  {
    label: "Containers · logs",
    query: {
      queryType: "builder",
      scope: "account",
      timeField: "timestamp",
      sampled: true,
      dataset: "logs.containerLogs",
      format: "logs",
      columns: ["timestamp", "message", "level", "containerId", "placementId", "id"],
      filters: [],
      groupBy: [],
      metrics: [],
      traceId: "",
      limit: 100,
    },
  },
  {
    label: "Workers · distributed trace",
    query: {
      queryType: "builder",
      scope: "account",
      timeField: "timestamp",
      sampled: true,
      dataset: "logs.traces",
      format: "traces",
      columns: [],
      filters: [],
      groupBy: [],
      metrics: [],
      traceId: "",
      limit: 1000,
    },
  },
  {
    label: "HTTP · requests by status",
    query: metric("events.httpRequests", "", "count", ["edgeResponseStatus"]),
  },
  { label: "D1 · query latency (ms)", query: metric("events.d1Queries", "queryDurationMs", "avg") },
  { label: "R2 · operations", query: metric("events.r2CombinedOperations", "") },
  { label: "KV · requests", query: metric("events.kvRequests", "") },
  { label: "Queues · messages", query: metric("events.queuesMessages", "") },
  {
    label: "Workers AI · inference time (ms)",
    query: metric("events.aiInference", "inferenceTimeMs", "avg"),
  },
];
