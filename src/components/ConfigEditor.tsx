import React from "react";
import type { DataSourcePluginOptionsEditorProps } from "@grafana/data";
import { Alert, Field, Input, SecretInput } from "@grafana/ui";
import type { CloudflareOptions, CloudflareSecrets } from "../types";

type Props = DataSourcePluginOptionsEditorProps<CloudflareOptions, CloudflareSecrets>;

export function ConfigEditor({ options, onOptionsChange }: Props) {
  const update = (change: Partial<CloudflareOptions>) =>
    onOptionsChange({ ...options, jsonData: { ...options.jsonData, ...change } });

  return (
    <div style={{ maxWidth: 800 }}>
      <Alert title="Cloudflare connection" severity="info">
        Use a Cloudflare API token with Account Analytics Read. Workers observability and other APIs
        may require additional read permissions. Your token is encrypted by Grafana and used only by
        the server.
      </Alert>
      <Field
        label="API token"
        description="Create a scoped token in your Cloudflare profile. Save & test after configuring the account."
      >
        <SecretInput
          aria-label="API token"
          value={options.secureJsonData?.apiToken ?? ""}
          isConfigured={Boolean(options.secureJsonFields?.apiToken)}
          onChange={(event) =>
            onOptionsChange({
              ...options,
              secureJsonData: { ...options.secureJsonData, apiToken: event.currentTarget.value },
            })
          }
          onReset={() =>
            onOptionsChange({
              ...options,
              secureJsonFields: { ...options.secureJsonFields, apiToken: false },
              secureJsonData: { ...options.secureJsonData, apiToken: "" },
            })
          }
        />
      </Field>
      <Field
        label="Default account ID"
        description="32-character account identifier from the Cloudflare dashboard. Queries can override this account."
      >
        <Input
          aria-label="Default account ID"
          value={options.jsonData.accountId ?? ""}
          onChange={(event) => update({ accountId: event.currentTarget.value.trim() })}
        />
      </Field>
      <Field label="Default zone ID" description="Optional. Used when a query selects zone scope.">
        <Input
          aria-label="Default zone ID"
          value={options.jsonData.zoneId ?? ""}
          onChange={(event) => update({ zoneId: event.currentTarget.value.trim() })}
        />
      </Field>
      <Field label="Request timeout (seconds)" description="Maximum 300 seconds.">
        <Input
          aria-label="Request timeout"
          type="number"
          min={1}
          max={300}
          value={options.jsonData.timeoutSeconds ?? 60}
          onChange={(event) => update({ timeoutSeconds: Number(event.currentTarget.value) })}
        />
      </Field>
      <Field
        label="Maximum returned rows"
        description="Result safety limit per query, up to 100,000. Queries reaching this limit display a warning."
      >
        <Input
          aria-label="Maximum returned rows"
          type="number"
          min={1}
          max={100000}
          value={options.jsonData.maxRows ?? 10000}
          onChange={(event) => update({ maxRows: Number(event.currentTarget.value) })}
        />
      </Field>
      <a
        href="https://developers.cloudflare.com/analytics/sql-api/get-started/"
        target="_blank"
        rel="noreferrer"
      >
        Token permissions and SQL API setup
      </a>
    </div>
  );
}
