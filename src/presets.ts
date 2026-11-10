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
  column: string[],
  groupBy: string[] = [],
  scope: "account" | "zone" = "account",
): Partial<CloudflareQuery> {
  return {
    ...defaultQuery,
    queryType: "graphql-builder",
    dataset,
    scope,
    timeField: "",
    columns: column,
    metrics: [],
    groupBy,
  };
}

const resourcePresets: Array<[string, string, "account" | "zone"]> = [
  ["TLS · certificate expiry", "certificates", "zone"],
  ["Tunnel · current status", "tunnels", "account"],
  ["Load Balancing · pools", "pools", "account"],
  ["Zero Trust · posture checks", "posture-rules", "account"],
  ["CASB · findings", "casb-findings", "account"],
  ["DEX · tests and results", "dex-tests", "account"],
  ["DLP · profiles", "dlp-profiles", "account"],
  ["Audit Logs v2 · events", "audit", "account"],
];

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
    query: graphMetric(
      "containersMetricsAdaptiveGroups",
      ["avg.cpuUtilization"],
      ["dimensions.applicationId"],
    ),
  },
  {
    label: "AI Gateway · requests",
    query: graphMetric("aiGatewayRequestsAdaptiveGroups", ["count"], ["dimensions.gateway"]),
  },
  { label: "AI Search · requests", query: graphMetric("aiSearchAPIAdaptiveGroups", ["count"]) },
  {
    label: "Durable Objects · SQL storage",
    query: graphMetric("durableObjectsSqlStorageGroups", ["max.storedBytes"]),
  },
  {
    label: "DNS · queries",
    query: graphMetric("dnsAnalyticsAdaptiveGroups", ["count"], ["dimensions.queryType"]),
  },
  {
    label: "DNS Firewall · responses",
    query: graphMetric(
      "dnsFirewallAnalyticsAdaptiveGroups",
      ["count"],
      ["dimensions.clusterName", "dimensions.responseCode"],
    ),
  },
  {
    label: "Load Balancing · requests and pool health",
    query: graphMetric(
      "loadBalancingRequestsAdaptiveGroups",
      ["count"],
      ["dimensions.lbName", "dimensions.selectedPoolHealthy"],
      "zone",
    ),
  },
  {
    label: "Health Checks · latency and failures",
    query: graphMetric(
      "healthCheckEventsAdaptiveGroups",
      ["avg.rttMs"],
      ["dimensions.healthCheckName", "dimensions.healthStatus"],
      "zone",
    ),
  },
  {
    label: "WAF · actions by rule",
    query: graphMetric(
      "firewallEventsAdaptiveGroups",
      ["count"],
      ["dimensions.ruleId", "dimensions.action", "dimensions.source"],
      "zone",
    ),
  },
  {
    label: "WAF · sampled firewall events",
    query: { ...graphMetric("firewallEventsAdaptive", [], [], "zone"), format: "logs" },
  },
  {
    label: "DDoS · packets by attack",
    query: graphMetric(
      "dosdNetworkAnalyticsAdaptiveGroups",
      ["sum.packets"],
      ["dimensions.attackId"],
    ),
  },
  {
    label: "Network Firewall · packets by rule",
    query: graphMetric(
      "magicFirewallNetworkAnalyticsAdaptiveGroups",
      ["sum.packets"],
      ["dimensions.ruleId"],
    ),
  },
  {
    label: "Network IDS · packets",
    query: graphMetric(
      "magicIDPSNetworkAnalyticsAdaptiveGroups",
      ["sum.packets"],
      ["dimensions.verdict"],
    ),
  },
  {
    label: "Spectrum · traffic",
    query: graphMetric(
      "spectrumNetworkAnalyticsAdaptiveGroups",
      ["sum.bits", "sum.packets"],
      ["dimensions.applicationTag"],
    ),
  },
  {
    label: "Gateway · DNS decisions",
    query: graphMetric(
      "gatewayResolverQueriesAdaptiveGroups",
      ["count"],
      ["dimensions.resolverDecision"],
    ),
  },
  {
    label: "Gateway · HTTP actions and DLP",
    query: graphMetric(
      "cf1GatewayHttp1hGroups",
      ["sum.requests", "sum.dlpMatchedRequestsTotal", "sum.dlpScannedRequestsTotal"],
      ["dimensions.action"],
    ),
  },
  {
    label: "Gateway · network sessions",
    query: graphMetric(
      "cf1GatewayNetworkSession1hGroups",
      ["sum.sessions", "sum.bytesTotal"],
      ["dimensions.protocol"],
    ),
  },
  {
    label: "Containers · CPU and memory",
    query: graphMetric(
      "containersMetricsAdaptiveGroups",
      ["avg.cpuUtilization", "avg.memory"],
      ["dimensions.applicationId"],
    ),
  },
  {
    label: "Containers · disk",
    query: graphMetric(
      "containersMetricsAdaptiveGroups",
      ["max.diskUsage", "max.diskAvailable"],
      ["dimensions.applicationId"],
    ),
  },
  {
    label: "Containers · network",
    query: graphMetric(
      "containersMetricsAdaptiveGroups",
      ["avg.rxBandwidthBps", "avg.txBandwidthBps"],
      ["dimensions.applicationId"],
    ),
  },
  {
    label: "Containers · uptime",
    query: graphMetric(
      "containersMetricsAdaptiveGroups",
      ["max.containerUptime"],
      ["dimensions.instanceId"],
    ),
  },
  {
    label: "Containers · usage",
    query: graphMetric(
      "containersUsageAdaptiveGroups",
      ["sum.cpuTimeSec", "sum.allocatedMemory", "sum.allocatedDisk", "sum.txBytes"],
      ["dimensions.applicationId"],
    ),
  },
  {
    label: "AI Gateway · tokens and cost",
    query: graphMetric(
      "aiGatewayRequestsAdaptiveGroups",
      ["sum.tokensIn", "sum.tokensOut", "sum.cost"],
      ["dimensions.gateway", "dimensions.model"],
    ),
  },
  {
    label: "AI Gateway · latency, errors, and cache",
    query: graphMetric(
      "aiGatewayRequestsAdaptiveGroups",
      ["quantiles.durationMsP95", "sum.erroredRequests", "sum.cachedRequests"],
      ["dimensions.gateway"],
    ),
  },
  {
    label: "AI Search · search and retrieval",
    query: graphMetric(
      "aiSearchAPIAdaptiveGroups",
      ["sum.searchCount", "sum.aiSearchCount"],
      ["dimensions.instanceId"],
    ),
  },
  {
    label: "AI Search · ingestion",
    query: graphMetric(
      "aiSearchIngestedItemsAdaptiveGroups",
      ["count", "avg.totalDurationMs", "sum.totalTokens"],
      ["dimensions.instanceId", "dimensions.error"],
    ),
  },
  {
    label: "AI Search · storage",
    query: graphMetric(
      "aiSearchStorageAdaptiveGroups",
      ["max.bytes", "sum.byteHours"],
      ["dimensions.instanceId"],
    ),
  },
  {
    label: "Durable Objects · runtime",
    query: graphMetric(
      "durableObjectsPeriodicGroups",
      ["sum.cpuTime", "sum.duration", "max.memoryUsageBytes"],
      ["dimensions.namespaceId"],
    ),
  },
  {
    label: "Durable Objects · storage",
    query: graphMetric("durableObjectsStorageGroups", ["max.storedBytes"]),
  },
  {
    label: "Pages Functions · requests and errors",
    query: graphMetric(
      "pagesFunctionsInvocationsAdaptiveGroups",
      ["sum.requests", "sum.errors"],
      ["dimensions.scriptName"],
    ),
  },
  {
    label: "Workers Assets · requests",
    query: graphMetric(
      "workersAssetsRequestsAdaptiveGroups",
      ["sum.requests"],
      ["dimensions.cacheStatus"],
    ),
  },
  {
    label: "Workers Builds · build minutes",
    query: graphMetric("workersBuildsBuildMinutesAdaptiveGroups", ["sum.buildMinutes"]),
  },
  {
    label: "Cron Triggers · scheduled execution events",
    query: { ...graphMetric("workersInvocationsScheduled", []), format: "logs" },
  },
  ...resourcePresets.map(([label, operation, scope]): Preset => ({
    label,
    query: {
      ...defaultQuery,
      queryType: "rest",
      operation,
      scope,
      format: operation === "audit" ? "logs" : "table",
      metrics: [],
      groupBy: [],
      columns: [],
      filters: [],
      resourceId: "",
      timeField: "",
    },
  })),
];
