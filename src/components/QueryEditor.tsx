import React, { useEffect, useState } from "react";
import type { QueryEditorProps, SelectableValue } from "@grafana/data";
import { Alert, Button, Field, Input, Select } from "@grafana/ui";
import type { DataSource } from "../datasource";
import {
  defaultAnalyticsSQL,
  defaultGraphQL,
  defaultQuery,
  defaultSQL,
  defaultTelemetry,
  type CloudflareOptions,
  type CloudflareQuery,
  type Dataset,
  type Metric,
  type QueryFilter,
  type QueryType,
  type ResultFormat,
} from "../types";
import { getTemplateSrv } from "@grafana/runtime";
import { QueryCodeEditor } from "./QueryCodeEditor";
import { sqlSuggestions } from "../sqlSuggestions";
import { presets } from "../presets";
import { defaultMetric, isNumericType, migrateQuery, queryForMode } from "../queryState";

type Props = QueryEditorProps<DataSource, CloudflareQuery, CloudflareOptions>;

const queryTypes: Array<SelectableValue<QueryType>> = [
  {
    label: "Builder",
    value: "builder",
    description: "Discover datasets and build parameterized SQL",
  },
  { label: "SQL", value: "sql", description: "Cloudflare SQL API (beta)" },
  { label: "GraphQL", value: "graphql", description: "Cloudflare GraphQL Analytics API" },
  {
    label: "Analytics Engine SQL",
    value: "analytics",
    description: "Existing Analytics Engine SQL endpoint",
  },
  {
    label: "Workers telemetry",
    value: "telemetry",
    description: "Workers Observability query API",
  },
];

const formats: Array<SelectableValue<ResultFormat>> = [
  { label: "Time series", value: "timeseries" },
  { label: "Table", value: "table" },
  { label: "Logs", value: "logs" },
  { label: "Trace", value: "traces" },
];

const aggregations = ["count", "sum", "avg", "min", "max", "p50", "p90", "p95", "p99"].map(
  (value) => ({ label: value, value }),
);

const operators = [
  "=",
  "!=",
  ">",
  ">=",
  "<",
  "<=",
  "LIKE",
  "NOT LIKE",
  "IS NULL",
  "IS NOT NULL",
].map((value) => ({ label: value, value }));

const rowStyle: React.CSSProperties = {
  display: "flex",
  flexWrap: "wrap",
  alignItems: "flex-end",
  gap: 12,
};

export function QueryEditor({ query: stored, datasource, onChange, onRunQuery }: Props) {
  const query = migrateQuery({ ...defaultQuery, ...stored });
  const [datasets, setDatasets] = useState<Dataset[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const [zones, setZones] = useState<Array<SelectableValue<string>>>([]);
  const [accounts, setAccounts] = useState<Array<SelectableValue<string>>>([]);
  const update = (change: Partial<CloudflareQuery>) => onChange({ ...query, ...change });

  useEffect(() => {
    let active = true;
    setLoading(true);
    setError("");
    datasource
      .datasets(query.accountId, undefined, refresh > 0)
      .then((found) => {
        if (active) setDatasets(found);
      })
      .catch((cause: Error) => {
        if (active) setError(String(cause));
      })
      .finally(() => {
        if (active) setLoading(false);
      });

    return () => {
      active = false;
    };
  }, [datasource, query.accountId, refresh]);

  const dataset = datasets.find((entry) => entry.name === query.dataset);

  const supportedAggregations = dataset?.kind?.states?.valid_aggregations;

  const aggregationOptions = supportedAggregations
    ? aggregations.filter((item) => supportedAggregations.includes(item.value))
    : aggregations;

  const fields = (dataset?.columns ?? []).map((column) => ({
    label: column.name,
    value: column.name,
    description: `${column.data_type} · ${column.description ?? ""}`,
    numeric: isNumericType(column.data_type),
  }));

  for (const attribute of dataset?.attributes ?? []) {
    fields.push({
      label: `attributes['${attribute.name}']`,
      value: `attributes['${attribute.name}']`,
      description: attribute.data_type,
      numeric: isNumericType(attribute.data_type),
    });
  }

  const selectDataset = (name: string) => {
    const entry = datasets.find((item) => item.name === name);
    const isLog = name.startsWith("logs.");
    const format: ResultFormat = name === "logs.traces" ? "traces" : isLog ? "logs" : "timeseries";
    update({
      dataset: name,
      scope: entry?.columns?.some((column) => column.name === "accountTag") ? "account" : "zone",
      format,
      metrics: isLog ? [] : [defaultMetric(entry, entry?.kind?.states ? "value" : "count")],
      timeField:
        entry?.columns?.find((column) => column.data_type.startsWith("Date"))?.name ?? "timestamp",
      sampled: entry?.columns?.some((column) => column.name === "sampleInterval"),
      groupBy: [],
      filters: [],
      traceId: "",
      columns:
        isLog && name !== "logs.workersLogs"
          ? entry?.columns?.flatMap((column) =>
              [
                "timestamp",
                "message",
                "level",
                "serviceName",
                "traceId",
                "spanId",
                "containerId",
                "placementId",
                "id",
              ].includes(column.name)
                ? [column.name]
                : [],
            )
          : [],
    });
  };

  const changeMetric = (index: number, change: Partial<Metric>) =>
    update({
      metrics: query.metrics?.map((item, position) =>
        position === index ? { ...item, ...change } : item,
      ),
    });

  const changeFilter = (index: number, change: Partial<QueryFilter>) =>
    update({
      filters: query.filters?.map((item, position) =>
        position === index ? { ...item, ...change } : item,
      ),
    });

  return (
    <div style={{ display: "grid", gap: 12 }}>
      <div style={rowStyle}>
        <Field label="Query language">
          <Select
            aria-label="Query language"
            width={28}
            value={query.queryType}
            options={queryTypes}
            onChange={(value) => {
              if (value.value) onChange(queryForMode(query, value.value));
            }}
          />
        </Field>
        <Field label="Result format">
          <Select
            aria-label="Result format"
            width={22}
            value={query.format}
            options={formats}
            onChange={(value) => {
              if (value.value) update({ format: value.value });
            }}
          />
        </Field>
        <Field label="Start with">
          <Select
            aria-label="Query examples"
            width={36}
            placeholder="Choose an example"
            options={presets.map((preset, index) => ({ label: preset.label, value: index }))}
            onChange={(value) => {
              const preset = presets[value.value ?? -1];

              if (preset) update(preset.query);
            }}
          />
        </Field>
      </div>
      <div style={rowStyle}>
        <Field label="Scope">
          <Select
            aria-label="Scope"
            width={18}
            value={query.scope}
            options={[
              { label: "Account", value: "account" },
              { label: "Zone", value: "zone" },
            ]}
            onChange={(value) => update({ scope: value.value === "zone" ? "zone" : "account" })}
          />
        </Field>
        <Field label="Account" description="Uses the configured default when empty.">
          <Input
            aria-label="Query account ID"
            width={40}
            placeholder="Default account or $variable"
            value={query.accountId ?? ""}
            onChange={(event) => update({ accountId: event.currentTarget.value })}
          />
        </Field>
        <Button
          variant="secondary"
          onClick={() => {
            datasource
              .accounts()
              .then((found) =>
                setAccounts(found.map((account) => ({ label: account.name, value: account.id }))),
              )
              .catch((cause: Error) => setError(String(cause)));
          }}
        >
          Browse accounts
        </Button>
        {accounts.length > 0 && (
          <Field label="Available accounts">
            <Select
              aria-label="Available accounts"
              width={30}
              options={accounts}
              value={query.accountId}
              onChange={(value) => update({ accountId: value.value })}
            />
          </Field>
        )}
        {query.scope === "zone" && (
          <>
            <Button
              variant="secondary"
              onClick={() => {
                datasource
                  .zones(query.accountId)
                  .then((found) =>
                    setZones(found.map((zone) => ({ label: zone.name, value: zone.id }))),
                  )
                  .catch((cause: Error) => setError(String(cause)));
              }}
            >
              Browse zones
            </Button>
            {zones.length > 0 && (
              <Field label="Available zones">
                <Select
                  aria-label="Available zones"
                  width={30}
                  options={zones}
                  value={query.zoneId}
                  onChange={(value) => update({ zoneId: value.value })}
                />
              </Field>
            )}
          </>
        )}
        {query.scope === "zone" && (
          <Field label="Zone ID">
            <Input
              aria-label="Query zone ID"
              width={40}
              placeholder="Default zone or $variable"
              value={query.zoneId ?? ""}
              onChange={(event) => update({ zoneId: event.currentTarget.value })}
            />
          </Field>
        )}
      </div>
      {error && (
        <Alert severity="warning" title="Dataset discovery">
          {error}. Check datasource configuration and token permissions. Raw queries remain
          available.
        </Alert>
      )}
      {query.queryType === "builder" && (
        <>
          <div style={rowStyle}>
            <Field
              label="Dataset"
              description={
                dataset?.description ??
                "Dataset names are case-sensitive. Availability depends on the account, plan, and permissions."
              }
            >
              <Select
                aria-label="Dataset"
                width={64}
                value={query.dataset}
                isLoading={loading}
                allowCustomValue
                options={datasets.map((entry) => ({
                  label: `${entry.title} · ${entry.name}`,
                  value: entry.name,
                  description: `${entry.category}${entry.hidden ? " · advanced" : ""}`,
                }))}
                onChange={(value) => {
                  if (value.value) selectDataset(value.value);
                }}
              />
            </Field>
            <Button variant="secondary" icon="sync" onClick={() => setRefresh(refresh + 1)}>
              Reload catalog
            </Button>
          </div>
          {(query.format === "timeseries" || query.format === "table") && (
            <>
              {(query.metrics ?? []).map((metric, index) => (
                <div key={index} style={rowStyle}>
                  <Field label="Aggregation">
                    <Select
                      aria-label={`Aggregation ${index + 1}`}
                      width={16}
                      value={metric.aggregation}
                      options={aggregationOptions}
                      onChange={(value) =>
                        changeMetric(index, { aggregation: value.value ?? "count" })
                      }
                    />
                  </Field>
                  {metric.aggregation !== "count" && (
                    <Field label="Metric field">
                      <Select
                        aria-label={`Metric field ${index + 1}`}
                        width={32}
                        value={metric.field}
                        options={fields.filter((field) => field.numeric)}
                        allowCustomValue
                        onChange={(value) => changeMetric(index, { field: value.value ?? "" })}
                      />
                    </Field>
                  )}
                  <Field label="Alias">
                    <Input
                      aria-label={`Metric alias ${index + 1}`}
                      width={24}
                      value={metric.alias}
                      onChange={(event) =>
                        changeMetric(index, { alias: event.currentTarget.value })
                      }
                    />
                  </Field>
                  <Button
                    variant="secondary"
                    aria-label={`Remove metric ${index + 1}`}
                    icon="trash-alt"
                    onClick={() =>
                      update({
                        metrics: query.metrics?.filter((_item, position) => position !== index),
                      })
                    }
                  />
                </div>
              ))}
              <div>
                <Button
                  variant="secondary"
                  size="sm"
                  icon="plus"
                  onClick={() =>
                    update({
                      metrics: [
                        ...(query.metrics ?? []),
                        defaultMetric(dataset, `value_${query.metrics?.length ?? 0}`),
                      ],
                    })
                  }
                >
                  Add metric
                </Button>
              </div>
              <Field
                label="Group by"
                description="Each unique dimension combination becomes a labeled series."
              >
                <Select
                  aria-label="Group by"
                  width={64}
                  isMulti
                  allowCustomValue
                  options={fields}
                  value={query.groupBy}
                  onChange={(values) =>
                    update({
                      groupBy: values.flatMap((value: SelectableValue<string>) =>
                        value.value ? [value.value] : [],
                      ),
                    })
                  }
                />
              </Field>
            </>
          )}
          {query.format === "logs" && (
            <Field
              label="Columns"
              description="Empty uses the dataset's default log columns. Select the timestamp and message columns for other log datasets."
            >
              <Select
                aria-label="Log columns"
                width={64}
                isMulti
                allowCustomValue
                options={fields}
                value={query.columns}
                onChange={(values) =>
                  update({
                    columns: values.flatMap((value: SelectableValue<string>) =>
                      value.value ? [value.value] : [],
                    ),
                  })
                }
              />
            </Field>
          )}
          {query.format === "traces" && (
            <Field label="Trace ID" description="Paste a trace ID to load its full waterfall.">
              <Input
                aria-label="Trace ID"
                width={56}
                value={query.traceId ?? ""}
                onChange={(event) => update({ traceId: event.currentTarget.value })}
              />
            </Field>
          )}
          {(query.filters ?? []).map((filter, index) => (
            <div key={index} style={rowStyle}>
              <Field label="Filter field">
                <Select
                  aria-label={`Filter field ${index + 1}`}
                  width={32}
                  allowCustomValue
                  options={fields}
                  value={filter.field}
                  onChange={(value) => changeFilter(index, { field: value.value ?? "" })}
                />
              </Field>
              <Field label="Operator">
                <Select
                  aria-label={`Filter operator ${index + 1}`}
                  width={18}
                  options={operators}
                  value={filter.operator}
                  onChange={(value) => changeFilter(index, { operator: value.value ?? "=" })}
                />
              </Field>
              {!filter.operator.includes("NULL") && (
                <Field label="Value">
                  <Input
                    aria-label={`Filter value ${index + 1}`}
                    width={32}
                    value={filter.value}
                    onChange={(event) =>
                      changeFilter(index, { value: event.currentTarget.value, literal: undefined })
                    }
                  />
                </Field>
              )}
              <Button
                variant="secondary"
                aria-label={`Remove filter ${index + 1}`}
                icon="trash-alt"
                onClick={() =>
                  update({
                    filters: query.filters?.filter((_item, position) => position !== index),
                  })
                }
              />
            </div>
          ))}
          <div>
            <Button
              variant="secondary"
              size="sm"
              icon="plus"
              onClick={() =>
                update({
                  filters: [...(query.filters ?? []), { field: "", operator: "=", value: "" }],
                })
              }
            >
              Add filter
            </Button>
          </div>
          <Field label="Row limit">
            <Input
              aria-label="Row limit"
              width={18}
              type="number"
              min={1}
              max={100000}
              value={query.limit ?? 1000}
              onChange={(event) => update({ limit: Number(event.currentTarget.value) })}
            />
          </Field>
          <details>
            <summary>Dataset schema · {dataset?.columns?.length ?? 0} columns</summary>
            <p>
              Adaptive sampling weights are applied automatically to count, sum, and average.
              Percentile metrics use explicit sample weights.
            </p>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => {
                datasource
                  .datasets(query.accountId, query.dataset)
                  .then((found) =>
                    setDatasets(
                      datasets.map(
                        (entry) => found.find((item) => item.name === entry.name) ?? entry,
                      ),
                    ),
                  )
                  .catch((cause: Error) => setError(String(cause)));
              }}
            >
              Discover custom attributes
            </Button>
            <table style={{ width: "100%", marginTop: 12 }}>
              <thead>
                <tr>
                  <th align="left">Field</th>
                  <th align="left">Type</th>
                  <th align="left">Description</th>
                </tr>
              </thead>
              <tbody>
                {dataset?.columns?.map((column) => (
                  <tr key={column.name}>
                    <td>
                      <code>{column.name}</code>
                    </td>
                    <td>{column.data_type}</td>
                    <td>{column.description}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </details>
        </>
      )}
      {(query.queryType === "sql" || query.queryType === "analytics") && (
        <>
          <Field
            label="SQL query"
            description={
              query.queryType === "sql"
                ? "Scope and dashboard time range are supplied automatically. Omit accountTag, zoneTag, and time predicates from SQL."
                : "Include a timestamp predicate with $__fromTime and $__toTime. Analytics Engine uses its original sampling semantics."
            }
          >
            <QueryCodeEditor
              key={query.queryType}
              label="SQL query"
              language="sql"
              height={180}
              onRunQuery={onRunQuery}
              getSuggestions={() =>
                sqlSuggestions(
                  query,
                  datasets,
                  getTemplateSrv()
                    .getVariables()
                    .map((variable) => variable.name),
                )
              }
              value={
                query.queryType === "analytics"
                  ? (query.analyticsSql ?? query.sql ?? defaultAnalyticsSQL)
                  : (query.sql ?? defaultSQL)
              }
              onChange={(value) =>
                update(query.queryType === "analytics" ? { analyticsSql: value } : { sql: value })
              }
            />
          </Field>
          {query.queryType === "sql" && (
            <Field
              label="SQL parameters (JSON)"
              description={
                'Bind values with $name placeholders. For dashboard variables use {"worker": ${worker:json}}.'
              }
            >
              <QueryCodeEditor
                label="SQL parameters"
                language="json"
                height={80}
                onRunQuery={onRunQuery}
                value={query.params ?? ""}
                onChange={(value) => update({ params: value })}
              />
            </Field>
          )}
          <div>
            <code>$__timeGroup(timestamp)</code> · <code>$__interval_s</code> ·{" "}
            <code>$__interval_ms</code> · <code>$__fromTime</code> · <code>$__toTime</code>
          </div>
        </>
      )}
      {query.queryType === "graphql" && (
        <>
          <Field
            label="GraphQL query"
            description="Variables accountTag, zoneTag, from, and to are provided automatically."
          >
            <QueryCodeEditor
              label="GraphQL query"
              language="graphql"
              height={260}
              onRunQuery={onRunQuery}
              value={query.graphql ?? defaultGraphQL}
              onChange={(value) => update({ graphql: value })}
            />
          </Field>
          <Field label="Additional variables (JSON)">
            <QueryCodeEditor
              label="GraphQL variables"
              language="json"
              height={80}
              onRunQuery={onRunQuery}
              value={query.variables ?? ""}
              onChange={(value) => update({ variables: value })}
            />
          </Field>
          <Field
            label="Operation name"
            description="Required when the document contains multiple queries."
          >
            <Input
              aria-label="GraphQL operation name"
              value={query.operationName ?? ""}
              onChange={(event) => update({ operationName: event.currentTarget.value })}
            />
          </Field>
        </>
      )}
      {query.queryType === "telemetry" && (
        <Field
          label="Workers telemetry request (JSON)"
          description="Paste query parameters from Workers Query Builder. Grafana supplies timeframe and an ad-hoc query ID. Use SQL for native trace waterfalls."
        >
          <QueryCodeEditor
            label="Telemetry query"
            language="json"
            height={220}
            onRunQuery={onRunQuery}
            value={query.telemetry ?? defaultTelemetry}
            onChange={(value) => update({ telemetry: value })}
          />
        </Field>
      )}
      {(query.queryType === "graphql" || query.queryType === "telemetry") && (
        <Field
          label="Result data path"
          description="Dot-separated path, relative to GraphQL data or telemetry result. Arrays are traversed automatically."
        >
          <Input
            aria-label="Result data path"
            value={query.dataPath ?? ""}
            onChange={(event) => update({ dataPath: event.currentTarget.value })}
          />
        </Field>
      )}
      {query.queryType !== "builder" && (
        <Field
          label="Series dimensions"
          description="Comma-separated result columns to use as labels, including numeric dimensions."
        >
          <Input
            aria-label="Series dimensions"
            value={query.groupBy?.join(", ") ?? ""}
            onChange={(event) =>
              update({
                groupBy: event.currentTarget.value
                  .split(",")
                  .flatMap((value) => (value.trim() ? [value.trim()] : [])),
              })
            }
          />
        </Field>
      )}
      <Field
        label="Time column"
        description="Timestamp column for time series and logs. Nested GraphQL fields use dot notation."
      >
        <Input
          aria-label="Time column"
          value={query.timeField ?? ""}
          onChange={(event) => update({ timeField: event.currentTarget.value })}
        />
      </Field>
      <div style={rowStyle}>
        <Button icon="play" onClick={onRunQuery}>
          Run Cloudflare query
        </Button>
        <a
          href="https://developers.cloudflare.com/analytics/sql-api/sql-reference/"
          target="_blank"
          rel="noreferrer"
        >
          SQL reference
        </a>
      </div>
    </div>
  );
}
