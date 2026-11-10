import { readFile } from "node:fs/promises";

const base = process.env.GRAFANA_URL ?? "http://localhost:3100";
const uid = process.env.GRAFANA_DATASOURCE_UID ?? "cloudflare-live";

const dashboard = JSON.parse(
  (await readFile("src/dashboards/cloudflare-overview.json", "utf8")).replaceAll(
    "${DS_CLOUDFLARE}",
    uid,
  ),
);

delete dashboard.__inputs;

const headers = { "Content-Type": "application/json" };

if (process.env.GRAFANA_TOKEN) headers.Authorization = `Bearer ${process.env.GRAFANA_TOKEN}`;

const response = await fetch(base + "/api/dashboards/db", {
  method: "POST",
  headers,
  body: JSON.stringify({ dashboard, overwrite: true }),
});

if (!response.ok)
  throw new Error(`Dashboard import failed: ${response.status} ${await response.text()}`);

console.log(`Imported ${base}/d/cloudflare-overview/cloudflare-overview`);
