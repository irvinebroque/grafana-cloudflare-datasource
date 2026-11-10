import { readFile } from "node:fs/promises";
import { datasourceUID, grafanaRequest, grafanaURL } from "./grafana.mjs";

const dashboard = JSON.parse(
  (await readFile("src/dashboards/cloudflare-overview.json", "utf8")).replaceAll(
    "${DS_CLOUDFLARE}",
    datasourceUID,
  ),
);

delete dashboard.__inputs;

await grafanaRequest("/api/dashboards/db", { dashboard, overwrite: true });

console.log(`Imported ${grafanaURL}/d/cloudflare-overview/cloudflare-overview`);
