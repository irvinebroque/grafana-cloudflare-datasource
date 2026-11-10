import React, { useEffect, useState } from "react";
import { Alert, Button, Field, Input, Select } from "@grafana/ui";
import type { SelectableValue } from "@grafana/data";
import type { DataSource } from "../datasource";
import type { CloudflareQuery, Operation } from "../types";
import { ResourcePicker } from "./ResourcePicker";

interface Props {
  query: CloudflareQuery;
  datasource: DataSource;
  onChange: (query: Partial<CloudflareQuery>) => void;
}

export function RESTEditor({ query, datasource, onChange }: Props) {
  const [operations, setOperations] = useState<Operation[]>([]);
  const [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    datasource
      .operations()
      .then((result) => {
        if (active) setOperations(result);
      })
      .catch((cause: Error) => {
        if (active) setError(String(cause));
      });

    return () => {
      active = false;
    };
  }, [datasource]);
  const operation = operations.find((item) => item.name === query.operation);

  return (
    <div style={{ display: "grid", gap: 12 }}>
      {error && (
        <Alert severity="warning" title="Operation discovery">
          {error}
        </Alert>
      )}
      <Field
        label="Operation"
        description={
          operation
            ? `Requires ${operation.permission}. Inventory shows current state; history operations use the selected time range.`
            : undefined
        }
      >
        <Select
          aria-label="Read-only operation"
          width={60}
          value={query.operation}
          options={operations.map((item) => ({ label: item.title, value: item.name }))}
          onChange={(item) => {
            const selected = operations.find((entry) => entry.name === item.value);

            if (selected)
              onChange({
                operation: selected.name,
                scope: selected.scope,
                resourceId: "",
                filters: [],
                columns: [],
                groupBy: [],
                format: selected.format,
                timeField: selected.timeField ?? "",
              });
          }}
        />
      </Field>
      {operation?.resource && (
        <ResourcePicker
          key={`${operation.name}:${query.accountId}:${query.zoneId}`}
          datasource={datasource}
          query={query}
          operation={operation.resource === "audit" ? undefined : operation.resource}
          value={query.resourceId ?? ""}
          onChange={(resourceId) => onChange({ resourceId })}
        />
      )}
      {operation?.name === "audit-history" && (
        <div>
          Enter the source audit entry ID and add its action_time parameter, or use Resource history
          from an audit log's details.
        </div>
      )}
      <Field
        label="Fields"
        description="Empty returns the operation's standard fields. Snapshot time records when current state was fetched."
      >
        <Select
          aria-label="Operation fields"
          isMulti
          value={query.columns ?? []}
          options={operation?.columns.map((name) => ({ label: name, value: name })) ?? []}
          onChange={(items) =>
            onChange({
              columns: items.flatMap((item: SelectableValue<string>) =>
                item.value ? [item.value] : [],
              ),
            })
          }
        />
      </Field>
      {(query.filters ?? []).map((filter, index) => (
        <div key={index} style={{ display: "flex", gap: 8, alignItems: "end" }}>
          <Field label="Parameter">
            <Select
              aria-label={`Operation parameter ${index + 1}`}
              width={30}
              value={filter.field}
              options={operation?.parameters?.map((name) => ({ label: name, value: name })) ?? []}
              onChange={(item) =>
                onChange({
                  filters: query.filters?.map((entry, position) =>
                    position === index ? { ...entry, field: item.value ?? "" } : entry,
                  ),
                })
              }
            />
          </Field>
          <Field label="Value">
            <Input
              aria-label={`Operation value ${index + 1}`}
              value={filter.value}
              onChange={(event) =>
                onChange({
                  filters: query.filters?.map((entry, position) =>
                    position === index ? { ...entry, value: event.currentTarget.value } : entry,
                  ),
                })
              }
            />
          </Field>
          <Button
            variant="secondary"
            icon="trash-alt"
            aria-label={`Remove operation parameter ${index + 1}`}
            onClick={() =>
              onChange({ filters: query.filters?.filter((_, position) => position !== index) })
            }
          />
        </div>
      ))}
      {Boolean(operation?.parameters?.length) && (
        <Button
          variant="secondary"
          onClick={() =>
            onChange({
              filters: [...(query.filters ?? []), { field: "", operator: "=", value: "" }],
            })
          }
        >
          Add operation parameter
        </Button>
      )}
      <Field label="Row limit">
        <Input
          aria-label="Operation row limit"
          type="number"
          min={1}
          max={100000}
          value={query.limit ?? 1000}
          onChange={(event) => onChange({ limit: Number(event.currentTarget.value) })}
        />
      </Field>
    </div>
  );
}
