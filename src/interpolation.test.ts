import { describe, expect, it } from "vite-plus/test";
import { interpolateQuery } from "./interpolation";
import { catalogSchema, type CloudflareQuery } from "./types";
import { contextRange, contextTarget, volumeTarget } from "./logContext";

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

  it("keeps all catalog kinds, state restrictions, and user attributes", () => {
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
    expect(target.columns).toContain("containerId");
  });
});
