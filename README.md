# Cloudflare data source for Grafana

Query Cloudflare metrics, logs, traces, and analytics in Grafana. Includes native Explore views, alerts, and overview and trace dashboards.

## Requirements

- Grafana **13.3+**.
- Your Cloudflare account ID and an [API token](#api-token-permissions).
- To build: Node.js **24+**, pnpm, and Go **1.27.3+**.

### API token permissions

[Create an API token](https://dash.cloudflare.com/profile/api-tokens?permissionGroupKeys=%5B%7B%22key%22%3A%22account_analytics%22%2C%22type%22%3A%22read%22%7D%5D&accountId=%2A&zoneId=all&name=Grafana%20Cloudflare) with **Account → Account Analytics → Read**, scoped to the accounts and zones you want to query. Add product permissions as needed; see the [permission reference](docs/PERMISSIONS.md).

Optional browsing permissions:

| Feature         | Additional permission             |
| --------------- | --------------------------------- |
| Browse accounts | Account → Account Settings → Read |
| Browse zones    | Zone → Zone → Read                |

You can also enter account and zone IDs directly.

## Getting started

### 1. Build

```sh
git clone https://github.com/irvinebroque/grafana-cloudflare-datasource.git
cd grafana-cloudflare-datasource
pnpm install --frozen-lockfile
BUILD_ALL=1 pnpm exec vp run build
```

### 2. Install

Copy `dist/` to your Grafana plugins directory as `cloudflare-cloudflare-datasource/`. The plugin is **unsigned**; add this to your Grafana configuration and restart Grafana:

```ini
[plugins]
allow_loading_unsigned_plugins = cloudflare-cloudflare-datasource
```

### 3. Connect

1. Open **Connections → Data sources → Add data source** and select **Cloudflare**.
2. Enter your **API token** and **Default account ID**. Optionally add a default zone ID.
3. Click **Save & test**.
4. Import **Cloudflare overview** and **Cloudflare traces** from the data source's **Dashboards** tab.

## Query your data

Open **Explore**, select Cloudflare, and choose an example from **Start with**. Set a time range and click **Run Cloudflare query**.

- **Metrics:** Choose **SQL** or **GraphQL**, then **Builder** to select datasets, metrics, and filters. Choose **Code** to write queries directly.
- **Logs:** Select a log dataset to use Grafana's log viewer. Workers and Containers also support surrounding context.
- **Traces:** Choose **Cloudflare · distributed trace**, or use **View trace** from Worker log details. Inspect spans in Grafana's native waterfall and node graph.

See the [product query guide](docs/PLATFORM.md) for examples across Cloudflare.

## Run locally with Docker

After building:

```sh
export CLOUDFLARE_API_TOKEN='your-token'
export CLOUDFLARE_ACCOUNT_ID='your-account-id'
docker compose up
```

Open [localhost:3100](http://localhost:3100) and sign in with `admin` / `admin` on first use.

## Development

```sh
pnpm exec vp check --deny-warnings
pnpm exec vp test run
go test -race ./...
pnpm exec vp run build
```

## License

[Apache 2.0](LICENSE).
