import React, { useState } from "react";
import { Alert, Button, Field, Input, Select } from "@grafana/ui";
import type { DataSource } from "../datasource";
import type { CloudflareQuery } from "../types";

interface Props {
  datasource: DataSource;
  query: CloudflareQuery;
  operation?: string;
  value: string;
  onChange: (value: string) => void;
}

export function ResourcePicker({ datasource, query, operation, value, onChange }: Props) {
  const [resources, setResources] = useState<Array<{ id: string; name: string }>>([]);
  const [error, setError] = useState("");
  const [limited, setLimited] = useState(false);
  const [loading, setLoading] = useState(false);

  return (
    <div style={{ display: "grid", gap: 8 }}>
      <div style={{ display: "flex", gap: 8, alignItems: "end" }}>
        <Field
          label="Resource ID or name"
          description="Enter an ID, name, or $variable. Browsing requires the product's read permission."
        >
          <Input
            aria-label="Resource ID"
            width={40}
            value={value}
            onChange={(event) => onChange(event.currentTarget.value)}
          />
        </Field>
        {operation && (
          <Button
            variant="secondary"
            disabled={loading}
            onClick={() => {
              setError("");
              setResources([]);
              setLoading(true);
              datasource
                .resources(operation, query)
                .then((result) => {
                  setResources(result.result);
                  setLimited(result.limited);
                })
                .catch((cause: Error) => setError(String(cause)))
                .finally(() => setLoading(false));
            }}
          >
            Browse resources
          </Button>
        )}
        {resources.length > 0 && (
          <Field label="Available resources">
            <Select
              aria-label="Available resources"
              width={40}
              value={value}
              options={resources.map((resource) => ({
                label: resource.name,
                value: resource.id,
                description: resource.id,
              }))}
              onChange={(item) => {
                if (item.value) onChange(item.value);
              }}
            />
          </Field>
        )}
      </div>
      {limited && <div>Showing the first 1,000 resources. Enter an ID directly if needed.</div>}
      {error && (
        <Alert title="Resource discovery" severity="warning">
          {error}
        </Alert>
      )}
    </div>
  );
}
