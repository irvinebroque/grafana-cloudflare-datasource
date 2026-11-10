# Cloudflare Grafana datasource

Query Cloudflare metrics, logs, distributed traces, and analytics from Grafana. The plugin uses a Go backend, keeps credentials in Grafana's encrypted secure settings, and works with dashboards, Explore, dashboard variables, annotations, and server-side alerting.

The main query interface is the **Cloudflare SQL API (beta)**. Its live catalog exposes event, state, log, Log Explorer, and custom Analytics Engine datasets across Cloudflare products. The plugin also supports raw **GraphQL Analytics**, **Workers Observability telemetry**, and the original **Analytics Engine SQL** endpoint.

## Features

- Catalog-driven SQL builder with field descriptions, custom-attribute discovery, account and zone scope, grouping, multiple metrics, filters, and row limits.
- Count, sum, average, min, max, and weighted p50/p90/p95/p99. State dataset aggregation options follow catalog restrictions.
- Grafana time series with dimension labels, null values, UTC timestamps, and automatic CPU, latency, and byte units.
- Workers and Containers logs with context and full-range log-volume histograms. Workers logs expose invocation, trace, and span IDs.
- Trace search, clickable trace IDs, and native distributed-trace waterfalls including parent relationships, span attributes, durations, and error status.
- Raw SQL with named JSON parameters and dashboard interval macros. Raw GraphQL and telemetry requests cover additional API features.
- Query variables using SQL (`__text` and `__value` columns), dashboard-variable interpolation, and annotations (`time` and `text` columns).
- Backend alert evaluation without a browser or frontend dependency.
- A bundled Cloudflare overview dashboard with a Worker variable, requests, p99 CPU, errors, D1 latency, R2 operations, logs, and trace search.
- Request cancellation, bounded retries for transient errors, response and row caps, schema caching, and per-query errors.

Product coverage includes Workers, Durable Objects, Workflows, Containers logs, D1, R2, KV, Queues, Hyperdrive, Vectorize, Analytics Engine, HTTP traffic, Access, security, network analytics, Web Analytics, Workers AI, browser rendering, Pipelines, Stream, images, and other datasets returned by Cloudflare. Catalog visibility is not a guarantee of entitlement or retained data. New datasets appear without a plugin release.

## Build

Requirements: Node.js 24+, pnpm, [Vite Plus](https://viteplus.dev/guide/), Go 1.27.2+, and Grafana 13.3+. The frontend is built with Vite Plus and converted to Grafana's SystemJS module format. All TypeScript uses strict mode. All generic [anti-slop](https://github.com/dmmulroy/anti-slop) rules are enabled; their source and provenance are vendored under `tools/oxlint/anti-slop/`.

```sh
pnpm install --frozen-lockfile
pnpm exec vp check --deny-warnings
pnpm exec vp test run
go test -race ./...
go vet ./...
BUILD_ALL=1 pnpm exec vp run build
node scripts/package.mjs
```

`dist/` contains the installable plugin. `artifacts/` contains the ZIP and SHA-256 checksum. `BUILD_ALL=1` builds Linux amd64/arm64, macOS amd64/arm64, and Windows amd64 executables. Without it, the build targets the host platform.

## Install in Grafana

1. Extract the release ZIP into Grafana's plugins directory, retaining the `cloudflare-cloudflare-datasource` folder.
2. This development release is **unsigned**. Set `GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS=cloudflare-cloudflare-datasource` (or the corresponding `allow_loading_unsigned_plugins` configuration) and restart Grafana.
3. Add the **Cloudflare** datasource under Connections.
4. Set an API token and default account ID. Optionally set a default zone ID. Select **Save & test**.
5. Import the bundled **Cloudflare overview** dashboard from the datasource's Dashboards page, or import `src/dashboards/cloudflare-overview.json` and choose your datasource.

The plugin has been validated on Grafana **13.3.0**. Older Grafana versions are not claimed as supported. A production catalog release requires Grafana plugin signing; no signing credential is included.

## Permissions

Use a Cloudflare API token with **Account Analytics Read** for the accounts you query. Zone-scoped queries require Zone Analytics Read or Account Analytics Read on the owning account. Resource selectors may require Account Settings Read and Zone Read. Workers telemetry requires the applicable Workers Observability read permissions. The original Analytics Engine endpoint requires Account Analytics Read.

Cloudflare applies authorization, plan, retention, sampling, dataset, and field restrictions to every query. Save & test checks the SQL catalog using the default account; it does not claim that every dataset or API is authorized.

See [SQL API setup](https://developers.cloudflare.com/analytics/sql-api/get-started/), [GraphQL authentication](https://developers.cloudflare.com/analytics/graphql-api/getting-started/authentication/), and [Workers observability](https://developers.cloudflare.com/workers/observability/).

## Local Grafana

With Docker:

```sh
export CLOUDFLARE_API_TOKEN='your-token'
export CLOUDFLARE_ACCOUNT_ID='your-account-id'
BUILD_ALL=1 pnpm exec vp run build
docker compose up
```

Open [local Grafana](http://localhost:3100). Docker uses Grafana's normal login (`admin` / `admin` initially). The provided provisioning file reads credentials from environment variables.

Without Docker, download and extract [Grafana](https://grafana.com/grafana/download) into `.local/grafana/`, or set `GRAFANA_HOME` to an existing installation:

```sh
export CLOUDFLARE_API_TOKEN='your-token'
export CLOUDFLARE_ACCOUNT_ID='your-account-id'
pnpm exec vp run build
node scripts/start-grafana.mjs
```

The native development runner binds to `127.0.0.1:3100`, enables anonymous local administration, and stores all runtime data under ignored `.local/`. For development with an existing `cf` login, use `--cf-auth` instead of `CLOUDFLARE_API_TOKEN`, with `CLOUDFLARE_ACCOUNT_ID` set explicitly. Run `cf auth whoami` first to refresh/verify the session. OAuth credentials expire; provision a scoped API token for a persistent installation.

To install the sample dashboard in this local instance:

```sh
node scripts/install-dashboard.mjs
```

Restart Grafana after changing the backend executable. Rebuild and reload the browser after frontend changes. The `dev` script watches the Vite build only; `vp run build` also finalizes SystemJS and builds Go.

## Querying

**Builder:** choose a dataset, result format, metrics, dimensions, and filters. The catalog shows allowed aggregates for state datasets and supported field types. Use **Browse accounts** or **Browse zones** to select an override. IDs may contain dashboard variables. Filters are bound as SQL parameters rather than concatenated into SQL. Filters currently combine with AND; use raw SQL for OR, IN, expressions, and conditional aggregates.

**SQL API:** the plugin supplies the selected account/zone and Grafana's time range in the request body. **Omit tenancy and time predicates from SQL**, because Cloudflare rejects duplicate request-level and SQL scope/time constraints.

```sql
SELECT $__timeGroup(timestamp) AS time,
       scriptName,
       COUNT(*) AS requests,
       AVG(cpuTimeUs) AS averageCpuUs
FROM events.workersInvocations
WHERE scriptName = $worker
GROUP BY time, scriptName
ORDER BY time
LIMIT 10000
```

Bind `worker` using the parameters field, for example `{"worker":"my-worker"}`. With a dashboard variable, use `{"worker":${worker:json}}`. Time series require a time column and a numeric metric. Choose Table for aggregates without a time column. Use Series dimensions to label numeric grouping columns in raw SQL or GraphQL.

| Macro                             | Expansion                                                                    |
| --------------------------------- | ---------------------------------------------------------------------------- |
| `$__timeGroup(timestamp)`         | A Cloudflare `toStartOfInterval` expression using the Grafana query interval |
| `$__interval_s`, `$__interval_ms` | Query interval in seconds or milliseconds                                    |
| `$__fromTime`, `$__toTime`        | Quoted UTC `YYYY-MM-DD HH:MM:SS` timestamps                                  |
| `$__from_s`, `$__to_s`            | Unix time in seconds                                                         |
| `$__accountId`, `$__zoneId`       | Quoted selected scope IDs                                                    |

Grafana built-in variables such as `${__from}` and `${__to}` are interpolated by Grafana. SQL interpolation uses Grafana's `sqlstring` formatter by default; prefer JSON parameters for user-controlled values. Builder filter values use raw interpolation and are then safely parameterized. Dashboard variables require concrete values when creating backend alerts.

**Logs:** the Workers and Containers logs presets select their canonical log columns. Other log datasets may have different schemas; choose their timestamp and message columns explicitly. Individual event/log rows represent retained samples, and a finite row limit is not a complete export. Full-range histogram queries are supported for Builder queries against Workers and Containers logs. Workers context uses invocation ID when present and otherwise the Worker name; Containers context uses the container or placement ID.

**Traces:** use the distributed-trace preset to search recent root spans, then click a trace ID. Alternatively paste an ID into the Trace ID field. Logs expose a **View trace** link. Full-trace queries return native Grafana trace frames. Retention and sampling can result in partial or missing traces. Increase the span limit or narrow the query when a row-limit warning appears.

**GraphQL:** paste a read-only query and choose the result data path, for example `viewer.accounts.workersInvocationsAdaptive`. Arrays are traversed automatically and nested scalar fields become dotted columns. Variables `accountTag`, `zoneTag`, `from`, and `to` are supplied automatically. Set a Time column such as `dimensions.datetimeMinute` for time series. The Workers example is included in the editor.

**Workers telemetry:** paste the JSON parameters for the Workers Observability query API, including `view`, `parameters`, `limit`, and optional cursor/aggregation settings. Grafana supplies the timeframe. The result path is relative to `result`; use `events.events` for individual logs. Use SQL trace mode for native waterfalls. This mode exposes API request and result structures and requires familiarity with the [Workers Query Builder](https://developers.cloudflare.com/workers/observability/query-builder/).

**Legacy Analytics Engine SQL:** choose Analytics Engine SQL and replace `your_dataset` in the starter query. This original endpoint uses its own SQL dialect and sampling semantics. Include the time filter explicitly:

```sql
SELECT $__timeGroup(timestamp) AS time,
       SUM(_sample_interval) AS events
FROM your_dataset
WHERE timestamp >= toDateTime($__fromTime)
  AND timestamp <= toDateTime($__toTime)
GROUP BY time
ORDER BY time
LIMIT 1000
```

The unified SQL API also discovers custom Analytics Engine datasets as `events.analyticsEngine.<name>`. Its supported aggregates apply sample weights automatically, so do not multiply by `_sample_interval` there.

## Sampling, limits, and API errors

The unified SQL API applies sampling weights to supported count, sum, and average operations. Builder percentiles use `quantileWeighted` with the dataset's sample interval, or weight 1 for unsampled data. State datasets permit only their catalog-declared aggregates. Log Explorer may support fewer SQL functions than other datasets; consult [Cloudflare's SQL reference](https://developers.cloudflare.com/analytics/sql-api/sql-reference/).

The backend limits concurrent queries per request to four, retries selected transient failures at most twice, caps response bodies at 32 MiB, and caps returned rows at the configured maximum (default 10,000, maximum 100,000). Row-limit warnings indicate potentially incomplete results. Narrow high-cardinality queries rather than assuming unlimited pagination. SQL queries control their own upstream LIMIT; the configured maximum also caps decoded output. The five-minute catalog cache can be bypassed with Reload catalog.

Cloudflare errors include the upstream HTTP status and ray ID for troubleshooting. An empty result means the API returned no matching retained rows. A permission or API error remains an error, not an empty success.

## Validation

```sh
pnpm exec vp check --deny-warnings
pnpm exec vp test run
go test -race ./...
go vet ./...
node scripts/live-validate.mjs --catalog
pnpm exec vp exec playwright test
```

Live tests require a running provisioned Grafana and Cloudflare account with recent Workers logs/traces. Browser tests use installed Google Chrome. Set `GRAFANA_URL`, `GRAFANA_DATASOURCE_UID`, and optionally `GRAFANA_TOKEN` for a different test instance. `live-validate.mjs` writes a metadata-only report under `.local/`; browser screenshots and local runtime data are ignored by Git.

See [the recorded validation results](docs/VALIDATION.md) for the tested versions, coverage, and external API limitations. The Docker path is provided for development; this session validated the native Grafana path because Docker was unavailable.
