import { describe, expect, it } from "vite-plus/test";
import { queryHasFilter, toggleQueryFilter } from "./queryFilters";
import { queryDisplayText } from "./queryDisplay";
import { defaultMetric, isNumericType } from "./queryState";
import { sqlSuggestions } from "./sqlSuggestions";
import type { CloudflareQuery, Dataset } from "./types";

const query: CloudflareQuery = {
  refId: "A",
  queryType: "builder",
  dataset: "logs.workersLogs",
  format: "logs",
  filters: [{ field: "scriptName", operator: "=", value: "worker" }],
};

describe("Explore filters", () => {
  it("toggles literal include/exclude values without altering other filters or the source query", () => {
    const options = { key: "message", value: "a' OR 1=1 --" };
    const included = toggleQueryFilter(query, { type: "FILTER_FOR", options });

    expect(queryHasFilter(included, options)).toBe(true);
    expect(included.filters).toEqual([
      ...query.filters!,
      { field: "message", operator: "=", value: options.value },
    ]);
    expect(toggleQueryFilter(included, { type: "FILTER_FOR", options }).filters).toEqual(
      query.filters,
    );

    const excluded = toggleQueryFilter(included, { type: "FILTER_OUT", options });

    expect(queryHasFilter(excluded, options)).toBe(false);
    expect(excluded.filters).toEqual([
      ...query.filters!,
      { field: "message", operator: "!=", value: options.value },
    ]);
    expect(toggleQueryFilter(excluded, { type: "FILTER_OUT", options }).filters).toEqual(
      query.filters,
    );
    expect(query.filters).toHaveLength(1);
  });

  it("leaves raw queries and unsupported fields intact", () => {
    for (const key of ["timestamp", "attributes", "bad;field", "source.message"]) {
      expect(
        toggleQueryFilter(query, { type: "FILTER_FOR", options: { key, value: "value" } }),
      ).toBe(query);
    }

    const sql = { ...query, queryType: "sql" } satisfies CloudflareQuery;

    expect(
      toggleQueryFilter(sql, { type: "FILTER_FOR", options: { key: "message", value: "value" } }),
    ).toBe(sql);
    expect(queryHasFilter(sql, { key: "scriptName", value: "worker" })).toBe(false);
  });
});

describe("schema-aware editor", () => {
  const dataset: Dataset = {
    name: "states.d1Storage",
    title: "D1 storage",
    category: "Storage",
    description: "Storage size",
    kind: { states: { valid_aggregations: ["max"] } },
    columns: [
      { name: "databaseId", data_type: "String" },
      { name: "sampleInterval", data_type: "UInt64" },
      { name: "sizeBytes", data_type: "Nullable(LowCardinality(UInt64))" },
    ],
    attributes: [{ name: "region.name", data_type: "String" }],
  };

  it("recognizes wrapped numeric types without suggesting string, date, or array fields", () => {
    for (const type of [
      "UInt64",
      "Float32",
      "Nullable(Int32)",
      "LowCardinality(Nullable(Decimal(10, 2)))",
    ]) {
      expect(isNumericType(type)).toBe(true);
    }

    for (const type of ["String", "Nullable(String)", "DateTime64(3)", "Array(UInt64)", "Bool"]) {
      expect(isNumericType(type)).toBe(false);
    }

    expect(defaultMetric(dataset, "storage")).toEqual({
      aggregation: "max",
      field: "sizeBytes",
      alias: "storage",
    });
  });

  it("completes datasets, referenced columns, attributes, macros and dashboard variables", () => {
    const unrelated = {
      ...dataset,
      name: "events.other",
      columns: [{ name: "unrelated", data_type: "String" }],
    };

    const suggestions = sqlSuggestions(
      { refId: "A", sql: 'SELECT * FROM "states.d1Storage"' },
      [dataset, unrelated],
      ["worker"],
    );

    const labels = suggestions.map((suggestion) => suggestion.label);

    expect(labels).toEqual(
      expect.arrayContaining([
        dataset.name,
        "events.other",
        "sizeBytes",
        "attributes['region.name']",
        "$worker",
        "$__timeGroup(timestamp)",
      ]),
    );
    expect(labels).not.toContain("unrelated");
    expect(suggestions.find((suggestion) => suggestion.label === "sizeBytes")?.detail).toContain(
      "UInt64",
    );

    const legacy = sqlSuggestions(
      { refId: "A", queryType: "analytics", analyticsSql: "SELECT * FROM custom" },
      [dataset, { ...unrelated, name: "events.analyticsEngine.custom" }],
      [],
    );

    expect(legacy.map((suggestion) => suggestion.label)).toContain("custom");
    expect(legacy.map((suggestion) => suggestion.label)).not.toContain(dataset.name);

    const quoted = sqlSuggestions(
      { refId: "A", sql: 'SELECT * FROM events.analyticsEngine."custom-events"' },
      [{ ...unrelated, name: "events.analyticsEngine.custom-events" }],
      [],
    );

    expect(
      quoted.find((suggestion) => suggestion.label === "events.analyticsEngine.custom-events")
        ?.insertText,
    ).toBe('events.analyticsEngine."custom-events"');
    expect(quoted.some((suggestion) => suggestion.label === "unrelated")).toBe(true);
  });
});

it("uses readable history summaries and preserves the legacy SQL dialect", () => {
  expect(queryDisplayText(query)).toBe('logs.workersLogs · scriptName = "worker"');
  expect(
    queryDisplayText({ refId: "A", queryType: "analytics", sql: "old", analyticsSql: "new" }),
  ).toBe("new");
  expect(queryDisplayText({ refId: "A", queryType: "sql", sql: "SELECT 1" })).toBe("SELECT 1");
});
