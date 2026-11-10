import { mkdir, writeFile } from "node:fs/promises";

import { datasourceUID, grafanaRequest } from "./grafana.mjs";

const health = await grafanaRequest(`/api/datasources/uid/${datasourceUID}/health`);

if (health.status !== "OK") throw new Error(health.message);

const catalog = await grafanaRequest(`/api/datasources/uid/${datasourceUID}/resources/datasets`);

const zones = await grafanaRequest(`/api/datasources/uid/${datasourceUID}/resources/zones`);

const report = {
  time: new Date().toISOString(),
  grafana: await grafanaRequest("/api/health"),
  plugin: (await grafanaRequest("/api/plugins/cloudflare-cloudflare-datasource/settings")).info
    ?.version,
  catalogSize: catalog.datasets.length,
  checks: [],
  catalog: [],
};

async function query(name, target, destination = report.checks) {
  const result = await grafanaRequest("/api/ds/query", {
    from: String(Date.now() - 3600000),
    to: String(Date.now()),
    queries: [
      {
        ...target,
        refId: "A",
        datasource: { uid: datasourceUID, type: "cloudflare-cloudflare-datasource" },
        intervalMs: 60000,
        maxDataPoints: 1000,
      },
    ],
  });

  const response = result.results?.A;

  if (
    destination === report.catalog &&
    response?.error?.includes("cannot be scoped by account") &&
    target.scope !== "zone" &&
    zones.result?.[0]
  ) {
    return query(name, { ...target, scope: "zone", zoneId: zones.result[0].id }, destination);
  }

  const entry = {
    name,
    ok: Boolean(response && !response.error && response.status === 200 && response.frames?.length),
    error: response?.error,
    frames: response?.frames?.length ?? 0,
    rows: response?.frames?.[0]?.data?.values?.[0]?.length ?? 0,
  };

  destination.push(entry);
  console.log(
    `${entry.ok ? "PASS" : "FAIL"} ${name} (${entry.rows} rows, ${entry.frames} frames)${entry.error ? ": " + entry.error : ""}`,
  );

  return response;
}

await query("Workers grouped requests", {
  queryType: "builder",
  dataset: "events.workersInvocations",
  format: "timeseries",
  metrics: [{ aggregation: "count", alias: "requests" }],
  groupBy: ["scriptName"],
  limit: 1000,
});

await query("Workers p99 CPU", {
  queryType: "builder",
  dataset: "events.workersInvocations",
  format: "timeseries",
  metrics: [{ aggregation: "p99", field: "cpuTimeUs", alias: "cpuTimeUs" }],
  groupBy: ["scriptName"],
  limit: 1000,
});

await query("Workers logs", {
  queryType: "builder",
  dataset: "logs.workersLogs",
  format: "logs",
  limit: 5,
});

const traces = await query("Trace search", {
  queryType: "builder",
  dataset: "logs.traces",
  format: "traces",
  limit: 5,
});

const traceIndex =
  traces.frames?.[0]?.schema.fields.findIndex((field) => field.name === "traceId") ?? -1;

const traceId =
  traceIndex >= 0 ? traces.frames[0].data.values[traceIndex].find(Boolean) : undefined;

if (traceId) {
  const trace = await query("Full distributed trace", {
    queryType: "builder",
    dataset: "logs.traces",
    format: "traces",
    traceId,
    limit: 10000,
  });

  if (!trace.frames?.[0]?.data.values[0]?.length || trace.frames.length !== 3) {
    report.checks[report.checks.length - 1].ok = false;
    report.checks[report.checks.length - 1].error =
      "Retained trace did not produce native waterfall and node graph frames";
  }
} else {
  report.checks.push({
    name: "Full distributed trace",
    ok: false,
    error: "No recent retained trace was returned by trace search",
  });
  console.log("FAIL Full distributed trace (no recent trace ID)");
}

await query("SQL macros and parameters", {
  queryType: "sql",
  format: "timeseries",
  sql: "SELECT $__timeGroup(timestamp) AS time, COUNT(*) AS requests FROM events.workersInvocations WHERE scriptName LIKE $worker GROUP BY time ORDER BY time LIMIT 1000",
  params: '{"worker":"%"}',
});

await query("Annotation query", {
  queryType: "sql",
  format: "table",
  sql: "SELECT timestamp AS time, message AS text FROM logs.workersLogs LIMIT 5",
});

await query("Variable query", {
  queryType: "sql",
  format: "table",
  sql: "SELECT scriptName AS __text, scriptName AS __value FROM events.workersInvocations GROUP BY scriptName LIMIT 1000",
});

await query("GraphQL builder", {
  queryType: "graphql-builder",
  dataset: "workersInvocationsAdaptive",
  format: "timeseries",
  columns: ["sum.requests"],
  groupBy: ["dimensions.scriptName"],
});

await query("GraphQL", {
  queryType: "graphql",
  graphql:
    "query($accountTag: string!, $from: Time!, $to: Time!) { viewer { accounts(filter:{accountTag:$accountTag}) { workersInvocationsAdaptive(limit:10,filter:{datetime_geq:$from,datetime_leq:$to},orderBy:[datetimeMinute_ASC]) { dimensions { datetimeMinute scriptName } sum { requests errors } } } } }",
  dataPath: "viewer.accounts.workersInvocationsAdaptive",
  timeField: "dimensions.datetimeMinute",
  format: "timeseries",
});

const wae = catalog.datasets.find((dataset) => dataset.name.startsWith("events.analyticsEngine."));

if (wae)
  await query("Legacy Analytics Engine SQL", {
    queryType: "analytics",
    sql: `SELECT timestamp, blob1 FROM ${wae.name.slice("events.analyticsEngine.".length)} WHERE timestamp >= toDateTime($__fromTime) LIMIT 5`,
    format: "table",
  });

if (wae)
  await query("Legacy grouped SQL macros", {
    queryType: "analytics",
    analyticsSql: `SELECT $__timeGroup(timestamp) AS time, SUM(_sample_interval) AS events FROM ${wae.name.slice("events.analyticsEngine.".length)} WHERE timestamp >= toDateTime($__fromTime) AND timestamp <= toDateTime($__toTime) GROUP BY time ORDER BY time LIMIT 10`,
    format: "timeseries",
  });

await query("Container log defaults", {
  queryType: "builder",
  dataset: "logs.containerLogs",
  format: "logs",
  limit: 2,
});

await query("Custom log timestamp", {
  queryType: "builder",
  dataset: "logs.traces",
  format: "logs",
  columns: ["startTime", "spanName"],
  timeField: "startTime",
  limit: 2,
});

await query("Numeric builder filters", {
  queryType: "builder",
  dataset: "events.workersInvocations",
  format: "table",
  metrics: [{ aggregation: "count", alias: "requests" }],
  filters: [{ field: "cpuTimeUs", operator: ">", value: "1000" }],
  limit: 2,
});

await query("Workers telemetry", {
  queryType: "telemetry",
  telemetry: JSON.stringify({
    view: "events",
    limit: 2,
    parameters: { datasets: ["cloudflare-workers"], filters: [], calculations: [] },
  }),
  dataPath: "events.events",
  format: "logs",
});

if (process.argv.includes("--catalog")) {
  let cursor = 0;

  async function worker() {
    while (cursor < catalog.datasets.length) {
      const dataset = catalog.datasets[cursor++];

      const column =
        dataset.columns?.find((field) => field.data_type.startsWith("Date")) ??
        dataset.columns?.find((field) => field.name !== "accountTag");

      if (!column) continue;
      await query(
        dataset.name,
        {
          queryType: "builder",
          dataset: dataset.name,
          format: "table",
          columns: [column.name],
          metrics: [],
          limit: 1,
        },
        report.catalog,
      );
    }
  }

  await Promise.all([worker(), worker(), worker()]);
}

await mkdir(".local", { recursive: true });

await writeFile(".local/live-validation.json", JSON.stringify(report, null, 2));

console.log(
  `Summary: ${report.checks.filter((check) => check.ok).length}/${report.checks.length} core checks; ${report.catalog.filter((check) => check.ok).length}/${report.catalog.length} catalog datasets.`,
);

if (report.checks.some((check) => !check.ok)) process.exitCode = 1;
