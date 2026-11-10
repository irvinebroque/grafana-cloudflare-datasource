import React, { useEffect, useRef, useState } from "react";
import { Alert, Button, Field, Input, Select } from "@grafana/ui";
import type { SelectableValue } from "@grafana/data";
import { graphFormat } from "../queryState";
import type { DataSource } from "../datasource";
import type { CloudflareQuery, GraphDataset } from "../types";

interface Props {
  query: CloudflareQuery;
  datasource: DataSource;
  onChange: (query: Partial<CloudflareQuery>) => void;
}

export function GraphQLBuilder({ query, datasource, onChange }: Props) {
  const [datasets, setDatasets] = useState<GraphDataset[]>([]);
  const [selected, setSelected] = useState<GraphDataset>();
  const [error, setError] = useState("");
  const [refresh, setRefresh] = useState(0);
  const [loading, setLoading] = useState(false);
  const refreshed = useRef(0);
  useEffect(() => {
    let active = true;
    setError("");
    setLoading(true);
    setSelected(undefined);
    const reload = refreshed.current !== refresh;
    refreshed.current = refresh;
    datasource
      .graphDatasets(query, undefined, reload)
      .then((found) => {
        if (active) setDatasets(found);

        return query.dataset ? datasource.graphDatasets(query, query.dataset) : [];
      })
      .then((found) => {
        if (active) setSelected(found[0]);
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
  }, [datasource, query.scope, query.accountId, query.zoneId, query.dataset, refresh]);
  const dataset = selected ?? datasets.find((entry) => entry.name === query.dataset);

  const fields = (dataset?.fields ?? []).map((field) => ({
    label: field.name,
    value: field.name,
    description: field.description,
  }));

  const times = (dataset?.fields ?? []).flatMap((field) =>
    /^(Time|Date)!?$/.test(field.type) ? [{ label: field.name, value: field.name }] : [],
  );

  const filters = (dataset?.filters ?? []).map((field) => ({
    label: field.name,
    value: field.name,
    description: field.type + " · " + field.description,
  }));

  return (
    <div style={{ display: "grid", gap: 12 }}>
      {error && (
        <Alert severity="warning" title="GraphQL discovery">
          {error}
        </Alert>
      )}
      <Field label="Analytics dataset" description={dataset?.description}>
        <Select
          aria-label="GraphQL dataset"
          width={60}
          value={query.dataset}
          isLoading={loading}
          options={datasets.map((entry) => ({
            label: entry.name,
            value: entry.name,
            description: entry.description,
          }))}
          onChange={(value) => {
            const found = datasets.find((entry) => entry.name === value.value);

            if (found)
              onChange({
                dataset: found.name,
                format: graphFormat(found),
                columns: [],
                groupBy: [],
                filters: [],
                timeField: "",
              });
          }}
        />
      </Field>
      {dataset?.settings && (
        <div>
          {dataset.settings.enabled
            ? "Available for this scope and token"
            : "Not enabled for this scope and token"}
          {dataset.settings.maxDuration > 0
            ? ` · maximum query range ${Math.floor(dataset.settings.maxDuration / 3600)} hours`
            : ""}
          {dataset.settings.notOlderThan > 0
            ? ` · retention ${Math.floor(dataset.settings.notOlderThan / 86400)} days`
            : ""}
        </div>
      )}
      <Field
        label="Metrics or fields"
        description="Provider aggregates retain their own sampling and quantile semantics. Empty chooses a default metric."
      >
        <Select
          aria-label="GraphQL fields"
          isMulti
          value={query.columns ?? []}
          options={fields}
          onChange={(values) =>
            onChange({
              columns: values.flatMap((value: SelectableValue<string>) =>
                value.value ? [value.value] : [],
              ),
            })
          }
        />
      </Field>
      <Field
        label="Group by"
        description="Select dimensions such as service, model, rule, instance, or location."
      >
        <Select
          aria-label="GraphQL dimensions"
          isMulti
          value={query.groupBy ?? []}
          options={fields.filter(
            (field) =>
              field.value.startsWith("dimensions.") ||
              !dataset?.fields.find((column) => column.name === field.value)?.numeric,
          )}
          onChange={(values) =>
            onChange({
              groupBy: values.flatMap((value: SelectableValue<string>) =>
                value.value ? [value.value] : [],
              ),
            })
          }
        />
      </Field>
      <Field
        label="Time bucket"
        description="Automatic uses a supported bucket for the panel interval."
      >
        <Select
          aria-label="GraphQL time bucket"
          value={query.timeField || ""}
          options={[{ label: "Automatic", value: "" }, ...times]}
          onChange={(value) => onChange({ timeField: value.value ?? "" })}
        />
      </Field>
      {(query.filters ?? []).map((filter, index) => (
        <div key={index} style={{ display: "flex", gap: 8, alignItems: "end" }}>
          <Field label="Filter">
            <Select
              aria-label={`GraphQL filter ${index + 1}`}
              width={35}
              value={filter.field}
              options={filters}
              onChange={(value) =>
                onChange({
                  filters: query.filters?.map((item, position) =>
                    position === index ? { ...item, field: value.value ?? "" } : item,
                  ),
                })
              }
            />
          </Field>
          <Field label="Value">
            <Input
              aria-label={`GraphQL filter value ${index + 1}`}
              value={filter.value}
              placeholder="Value, $variable, or JSON array"
              onChange={(event) =>
                onChange({
                  filters: query.filters?.map((item, position) =>
                    position === index ? { ...item, value: event.currentTarget.value } : item,
                  ),
                })
              }
            />
          </Field>
          <Button
            variant="secondary"
            aria-label={`Remove GraphQL filter ${index + 1}`}
            icon="trash-alt"
            onClick={() =>
              onChange({ filters: query.filters?.filter((_, position) => position !== index) })
            }
          />
        </div>
      ))}
      <div style={{ display: "flex", gap: 12 }}>
        <Button
          variant="secondary"
          icon="plus"
          onClick={() =>
            onChange({
              filters: [...(query.filters ?? []), { field: "", operator: "=", value: "" }],
            })
          }
        >
          Add GraphQL filter
        </Button>
        <Button variant="secondary" icon="sync" onClick={() => setRefresh((value) => value + 1)}>
          Reload GraphQL schema
        </Button>
      </div>
      <Field label="Row limit">
        <Input
          aria-label="GraphQL row limit"
          type="number"
          min={1}
          max={dataset?.settings?.maxPageSize || 100000}
          value={query.limit ?? 1000}
          onChange={(event) => onChange({ limit: Number(event.currentTarget.value) })}
        />
      </Field>
    </div>
  );
}
