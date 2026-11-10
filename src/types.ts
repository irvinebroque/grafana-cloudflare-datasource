import type { DataQuery, DataSourceJsonData } from "@grafana/data";
import { z } from "zod";

export type QueryType =
  | "builder"
  | "sql"
  | "graphql"
  | "graphql-builder"
  | "analytics"
  | "telemetry";

export type ResultFormat = "timeseries" | "table" | "logs" | "traces";

export interface Metric {
  aggregation: string;
  field: string;
  alias: string;
}

export interface QueryFilter {
  field: string;
  operator: string;
  value: string;
  literal?: boolean;
}

export interface CloudflareQuery extends DataQuery {
  queryType?: QueryType;
  format?: ResultFormat;
  accountId?: string;
  zoneId?: string;
  scope?: "account" | "zone";
  dataset?: string;
  metrics?: Metric[];
  groupBy?: string[];
  filters?: QueryFilter[];
  columns?: string[];
  timeField?: string;
  sampled?: boolean;
  limit?: number;
  sql?: string;
  analyticsSql?: string;
  params?: string;
  graphql?: string;
  operationName?: string;
  variables?: string;
  dataPath?: string;
  telemetry?: string;
  traceId?: string;
  logOrder?: "asc" | "desc";
  logVolume?: boolean;
  volumeSource?: CloudflareQuery;
}

export interface CloudflareOptions extends DataSourceJsonData {
  accountId?: string;
  zoneId?: string;
  timeoutSeconds?: number;
  maxRows?: number;
}

export interface CloudflareSecrets {
  apiToken?: string;
}

const columnSchema = z.object({
  name: z.string(),
  description: z.string().optional(),
  data_type: z.string(),
});

const datasetSchema = z.object({
  name: z.string(),
  title: z.string(),
  category: z.string(),
  description: z.string(),
  hidden: z.boolean().optional(),
  kind: z
    .object({
      states: z.object({ valid_aggregations: z.array(z.string()) }).optional(),
    })
    .optional(),
  columns: z.array(columnSchema).optional(),
  attributes: z.array(columnSchema).optional(),
});

export const catalogSchema = z.object({ datasets: z.array(datasetSchema) });

export type Dataset = z.infer<typeof datasetSchema>;

export const resourceSchema = z.object({
  result: z.array(z.object({ id: z.string(), name: z.string() })),
});

export const defaultQuery: Partial<CloudflareQuery> = {
  queryType: "builder",
  format: "timeseries",
  scope: "account",
  dataset: "events.workersInvocations",
  metrics: [{ aggregation: "count", field: "", alias: "requests" }],
  groupBy: ["scriptName"],
  filters: [],
  columns: [],
  timeField: "timestamp",
  sampled: true,
  traceId: "",
  logVolume: false,
  volumeSource: undefined,
  limit: 1000,
};

export const defaultSQL =
  "SELECT $__timeGroup(timestamp) AS time, COUNT(*) AS requests\nFROM events.workersInvocations\nGROUP BY time\nORDER BY time\nLIMIT 1000";

export const defaultAnalyticsSQL =
  "SELECT $__timeGroup(timestamp) AS time, SUM(_sample_interval) AS events\nFROM your_dataset\nWHERE timestamp >= toDateTime($__fromTime) AND timestamp <= toDateTime($__toTime)\nGROUP BY time ORDER BY time LIMIT 1000";

export const defaultGraphQL = `query Workers($accountTag: string!, $from: Time!, $to: Time!) {
  viewer {
    accounts(filter: {accountTag: $accountTag}) {
      workersInvocationsAdaptive(limit: 1000, filter: {datetime_geq: $from, datetime_leq: $to}, orderBy: [datetimeMinute_ASC]) {
        dimensions { datetimeMinute scriptName }
        sum { requests errors subrequests }
      }
    }
  }
}`;

export const defaultTelemetry = JSON.stringify(
  {
    view: "events",
    limit: 100,
    parameters: { datasets: ["cloudflare-workers"], filters: [], calculations: [] },
  },
  null,
  2,
);

const graphColumnSchema = z.object({
  name: z.string(),
  type: z.string(),
  description: z.string(),
  numeric: z.boolean(),
});

export const graphCatalogSchema = z.object({
  datasets: z.array(
    z.object({
      name: z.string(),
      description: z.string(),
      fields: z.array(graphColumnSchema),
      filters: z.array(graphColumnSchema),
      orderBy: z.array(z.string()),
      settings: z
        .object({
          enabled: z.boolean(),
          availableFields: z.array(z.string()),
          maxDuration: z.number(),
          maxPageSize: z.number(),
          notOlderThan: z.number(),
        })
        .optional(),
    }),
  ),
});

export type GraphDataset = z.infer<typeof graphCatalogSchema>["datasets"][number];
