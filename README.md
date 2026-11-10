# Cloudflare data source for Grafana

Query and visualize Cloudflare metrics, logs, traces, and analytics in Grafana.

- **Metrics:** Workers, HTTP traffic, D1, R2, KV, Queues, Workers AI, and more.
- **Logs:** Search Workers and Containers logs, view context, and chart log volume.
- **Traces:** Search traces and open span waterfalls directly from logs.
- **Dashboards:** Use variables, annotations, alerts, and the included Cloudflare overview dashboard.

```mermaid
flowchart LR
    Grafana[Grafana dashboards and Explore] --> Plugin[Cloudflare data source]
    Plugin --> SQL[Cloudflare SQL API · beta]
    Plugin --> APIs[GraphQL, Workers telemetry, Analytics Engine]
```

## Requirements

- Grafana **13.3+**.
- Your Cloudflare account ID and an [API token](https://developers.cloudflare.com/analytics/sql-api/get-started/) with **Account Analytics Read**. Add product and zone permissions as needed.
- To build: Node.js **24+**, pnpm, and Go **1.27.3+**.

## Getting started

### 1. Build the plugin

```sh
git clone https://github.com/irvinebroque/grafana-cloudflare-datasource.git
cd grafana-cloudflare-datasource
pnpm install --frozen-lockfile
BUILD_ALL=1 pnpm exec vp run build
```

The installable plugin is in `dist/`. The build includes Linux, macOS, and Windows executables.

### 2. Install in Grafana

- Copy `dist/` to your Grafana plugins directory as `cloudflare-cloudflare-datasource/`.
- This plugin is **unsigned**. Add this to your Grafana configuration, then restart Grafana:

```ini
[plugins]
allow_loading_unsigned_plugins = cloudflare-cloudflare-datasource
```

### 3. Connect your account

1. Open **Connections → Data sources → Add data source**.
2. Select **Cloudflare**.
3. Enter your **API token** and **Default account ID**. Optionally add a default zone ID.
4. Click **Save & test**.
5. Open the data source's **Dashboards** tab and import **Cloudflare overview**.

Grafana encrypts the saved token. Cloudflare API requests run through the plugin backend.

## Query your data

### Use the query builder

1. Open **Explore** and select your Cloudflare data source.
2. Choose an example from **Start with**, or select a dataset and add metrics and filters.
3. Choose a time range and click **Run Cloudflare query**.

| What you want      | Example                     |
| ------------------ | --------------------------- |
| Requests by Worker | Workers · requests          |
| CPU time           | Workers · p99 CPU time (µs) |
| D1 query latency   | D1 · query latency (ms)     |
| R2 operations      | R2 · operations             |
| Worker logs        | Workers · logs              |
| Container logs     | Containers · logs           |
| Trace search       | Workers · distributed trace |

### Write SQL

Select **SQL** and **Time series**. This query charts requests for one Worker:

```sql
SELECT $__timeGroup(timestamp) AS time,
       COUNT(*) AS requests
FROM events.workersInvocations
WHERE scriptName = $worker
GROUP BY time
ORDER BY time
LIMIT 1000
```

Set **SQL parameters** to:

```json
{ "worker": "my-worker" }
```

- The plugin supplies the account or zone and dashboard time range. Omit those predicates from SQL.
- `$__timeGroup(timestamp)` groups results using Grafana's query interval.
- Press **Ctrl+Space** for dataset, column, macro, and dashboard variable suggestions. Press **Shift+Enter** or **Ctrl/Cmd+Enter** to run.
- Choose **Table** for queries without a time column.
- See the [Cloudflare SQL reference](https://developers.cloudflare.com/analytics/sql-api/sql-reference/) for supported functions.

### Explore logs and traces

- Choose **Workers · logs** or **Containers · logs** to search logs and view surrounding context.
- In builder log details, use the filter buttons to include or exclude a field value. The builder shows the resulting filter.
- In Worker log details, click **View trace** to open the matching waterfall.
- Choose **Workers · distributed trace** to search traces, or paste a **Trace ID**.

```mermaid
flowchart LR
    Logs[Worker logs] --> Details[Log details]
    Details -->|View trace| Trace[Trace waterfall]
    Trace --> Spans[Inspect spans and attributes]
```

### Use other Cloudflare APIs

| Query language           | How to use it                                                                                                                                       |
| ------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| **GraphQL**              | Start with the included Workers query; set the result data path and time column. Set an operation name when the document contains multiple queries. |
| **Workers telemetry**    | Paste a Query Builder request; use `events.events` as the result path for logs.                                                                     |
| **Analytics Engine SQL** | Replace `your_dataset` in the starter query; keep its explicit time filter.                                                                         |

## Run locally with Docker

After building the plugin:

```sh
export CLOUDFLARE_API_TOKEN='your-token'
export CLOUDFLARE_ACCOUNT_ID='your-account-id'
docker compose up
```

Open [localhost:3100](http://localhost:3100) and sign in with `admin` / `admin` on first use.

## Things to know

- Dataset access depends on your token, Cloudflare plan, and data retention.
- Sampling can affect metrics and leave gaps in logs or traces.
- A row-limit warning means results may be incomplete; narrow the time range or increase the limit.
- See [validation and known limitations](docs/VALIDATION.md).
- See the [0.1.1 bug audit](docs/AUDIT.md) for fixes and dependency findings.
- See the [comparison with Grafana's cloud datasources](docs/COMPARISON.md) for the design decisions behind the editor and Explore integrations.

## Development

Uses Vite Plus, strict TypeScript, and [anti-slop](https://github.com/dmmulroy/anti-slop).

```sh
pnpm exec vp check --deny-warnings
pnpm exec vp test run
go test -race ./...
pnpm exec vp run build
```

Use `pnpm exec vp run dev` for incremental frontend builds. Initial and subsequent builds emit Grafana-compatible SystemJS modules. Rebuild the Go executable and restart Grafana after backend changes.

## License

[Apache 2.0](LICENSE).
