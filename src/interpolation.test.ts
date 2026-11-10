import { describe, expect, it } from "vite-plus/test";
import { interpolateQuery } from "./interpolation";
import { catalogSchema, type CloudflareQuery } from "./types";
import { contextRange, contextTarget, volumeTarget } from "./logContext";
import { migrateQuery, queryForMode } from "./queryState";
import { defaultSQL } from "./types";

describe("query boundaries", () => {
  it("uses SQL escaping for SQL and raw values only for parameterized builder filters", () => {
    const calls: Array<[string, string]> = [];

    const result = interpolateQuery(
      {
        refId: "A",
        sql: "SELECT $worker",
        filters: [{ field: "scriptName", operator: "=", value: "$worker" }],
        params: '{"worker":${worker:json}}',
      },
      (value, format) => {
        calls.push([value, format]);

        return value.replace("$worker", format === "sqlstring" ? "'a''b'" : "a'b");
      },
    );

    expect(result.sql).toBe("SELECT 'a''b'");
    expect(result.filters?.[0]?.value).toBe("a'b");
    expect(calls).toContainEqual(["SELECT $worker", "sqlstring"]);
    expect(calls).toContainEqual(['{"worker":${worker:json}}', "json"]);
  });

  it("validates state restrictions and user attributes", () => {
    const catalog = catalogSchema.parse({
      datasets: [
        {
          name: "states.d1Storage",
          title: "D1",
          category: "Storage",
          description: "Storage size",
          kind: { states: { sampling: "adaptive", valid_aggregations: ["max"] } },
          columns: [{ name: "databaseSizeBytes", data_type: "UInt64" }],
          attributes: [{ name: "region.name", data_type: "String" }],
        },
      ],
    });

    expect(catalog.datasets[0]?.kind?.states?.valid_aggregations).toEqual(["max"]);
    expect(catalog.datasets[0]?.attributes?.[0]?.name).toBe("region.name");
    expect(catalogSchema.safeParse({ datasets: [{ name: 3 }] }).success).toBe(false);
  });
});

describe("query mode transitions", () => {
  it("keeps the unified SQL and Analytics Engine dialects separate", () => {
    const unified: CloudflareQuery = { refId: "A", queryType: "sql", sql: "SELECT unified" };
    const legacy = queryForMode(unified, "analytics");
    legacy.analyticsSql = "SELECT legacy";
    const restored = queryForMode(legacy, "sql");

    expect(restored.sql).toBe("SELECT unified");
    expect(queryForMode(restored, "analytics").analyticsSql).toBe("SELECT legacy");
  });

  it("migrates older legacy queries without sending their dialect to the unified endpoint", () => {
    const migrated = queryForMode(
      { refId: "A", queryType: "analytics", sql: "SELECT legacy" },
      "sql",
    );

    expect(migrated.sql).toBe(defaultSQL);
    expect(migrated.analyticsSql).toBe("SELECT legacy");
    const edited = migrateQuery({ refId: "A", queryType: "analytics", sql: "SELECT old" });
    edited.analyticsSql = "SELECT edited";
    expect(queryForMode(edited, "sql").sql).toBe(defaultSQL);
  });

  it("clears stale trace and dataset settings when choosing a different mode", () => {
    const trace: CloudflareQuery = {
      refId: "A",
      queryType: "builder",
      dataset: "logs.traces",
      traceId: "trace",
      format: "traces",
      timeField: "endTime",
      scope: "zone",
    };

    const telemetry = queryForMode(trace, "telemetry");
    expect(telemetry.dataset).toBeUndefined();
    expect(telemetry.traceId).toBe("");
    expect(telemetry.scope).toBe("account");
    const builder = queryForMode(telemetry, "builder");
    expect(builder.dataset).toBe("events.workersInvocations");
    expect(builder.timeField).toBe("timestamp");
    expect(builder.format).toBe("timeseries");
  });

  it("interpolates telemetry JSON and legacy SQL using the appropriate format", () => {
    const calls: Array<[string, string]> = [];
    interpolateQuery(
      { refId: "A", analyticsSql: "SELECT $worker", telemetry: '{"worker":${worker:json}}' },
      (value, format) => {
        calls.push([value, format]);

        return value;
      },
    );
    expect(calls).toContainEqual(["SELECT $worker", "sqlstring"]);
    expect(calls).toContainEqual(['{"worker":${worker:json}}', "json"]);
  });
});

describe("log context", () => {
  it("builds a full-range volume query without inheriting the log row limit", () => {
    const query = {
      refId: "A",
      queryType: "builder",
      format: "logs",
      dataset: "logs.workersLogs",
      limit: 20,
      filters: [{ field: "scriptName", operator: "=", value: "worker" }],
    } satisfies CloudflareQuery;

    const volume = volumeTarget(query);
    expect(volume?.metrics?.[0]?.aggregation).toBe("count");
    expect(volume?.limit).toBe(10000);
    expect(volume?.filters).toEqual(query.filters);
    expect(volume?.volumeSource).toBe(query);
    expect(volumeTarget({ ...query, hide: true })).toBeUndefined();
  });
  it("keeps the source account and scopes context to an invocation", () => {
    const target = contextTarget(
      { timestamp: 10000, requestId: "request", scriptName: "worker" },
      {
        refId: "A",
        accountId: "account",
        filters: [{ field: "level", operator: "=", value: "error" }],
      },
    );

    expect(target.accountId).toBe("account");
    expect(target.filters).toEqual([{ field: "requestId", operator: "=", value: "request" }]);
    expect(target.format).toBe("logs");
    expect(
      contextTarget(
        { timestamp: 10000, accountId: "resolved" },
        { refId: "A", accountId: "$account" },
      ).accountId,
    ).toBe("resolved");
  });

  it("falls back to the worker and excludes the anchor during directional pagination", () => {
    expect(contextTarget({ timestamp: 10000, scriptName: "worker" }).filters).toEqual([
      { field: "scriptName", operator: "=", value: "worker" },
    ]);
    expect(contextRange({ timestamp: 10000 }, 5000, "FORWARD")).toEqual({
      from: 10001,
      to: 15000,
    });
    expect(contextRange({ timestamp: 10000 }, 5000, "BACKWARD")).toEqual({
      from: 5000,
      to: 9999,
    });
  });

  it("keeps container context in its source dataset and scopes it to the container", () => {
    const target = contextTarget(
      { timestamp: 10000, containerId: "container", requestId: "unrelated" },
      { refId: "A", dataset: "logs.containerLogs", scope: "zone" },
    );

    expect(target.dataset).toBe("logs.containerLogs");
    expect(target.scope).toBe("account");
    expect(target.filters).toEqual([{ field: "containerId", operator: "=", value: "container" }]);
    expect(target.columns).toEqual([]);
  });
});
