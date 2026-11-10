import { defaultQuery, type CloudflareQuery } from "./types";

export interface LogAnchor {
  accountId?: string;
  requestId?: string;
  scriptName?: string;
  containerId?: string;
  placementId?: string;
}

export function contextTarget(
  anchor: LogAnchor,
  original?: Partial<CloudflareQuery>,
): CloudflareQuery {
  const filters = [];

  const container = original?.dataset === "logs.containerLogs";

  if (container && anchor.containerId)
    filters.push({ field: "containerId", operator: "=", value: anchor.containerId });
  else if (container && anchor.placementId)
    filters.push({ field: "placementId", operator: "=", value: anchor.placementId });
  else if (!container && anchor.requestId)
    filters.push({ field: "requestId", operator: "=", value: anchor.requestId });
  else if (!container && anchor.scriptName)
    filters.push({ field: "scriptName", operator: "=", value: anchor.scriptName });

  return {
    ...original,
    ...defaultQuery,
    accountId: anchor.accountId || original?.accountId,
    refId: "context",
    dataset: container ? "logs.containerLogs" : "logs.workersLogs",
    format: "logs",
    filters,
    metrics: [],
    groupBy: [],
    limit: 100,
  };
}

export function contextRange(
  timestamp: number,
  windowMs: number,
  direction?: "FORWARD" | "BACKWARD",
) {
  return {
    from: direction === "FORWARD" ? timestamp + 1 : timestamp - windowMs,
    to: direction === "BACKWARD" ? timestamp - 1 : timestamp + windowMs,
  };
}

export function volumeTarget(original: CloudflareQuery): CloudflareQuery | undefined {
  if (
    original.hide ||
    (original.queryType ?? "builder") !== "builder" ||
    original.format !== "logs" ||
    !["logs.workersLogs", "logs.containerLogs"].includes(original.dataset ?? "")
  )
    return undefined;

  return {
    ...original,
    refId: `log-volume-${original.refId}`,
    format: "timeseries",
    metrics: [{ aggregation: "count", field: "", alias: "logs" }],
    groupBy: ["level"],
    columns: [],
    timeField: "timestamp",
    limit: 10000,
    logVolume: true,
    volumeSource: original,
  };
}
