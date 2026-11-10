import { readFile, readdir } from "node:fs/promises";
import { datasourceUID, grafanaRequest, grafanaURL } from "./grafana.mjs";

for (const file of (await readdir("src/dashboards")).filter((name) => name.endsWith(".json"))) {
  const dashboard = JSON.parse(
    (await readFile(`src/dashboards/${file}`, "utf8")).replaceAll(
      "${DS_CLOUDFLARE}",
      datasourceUID,
    ),
  );

  delete dashboard.__inputs;
  const result = await grafanaRequest("/api/dashboards/db", { dashboard, overwrite: true });
  console.log(`Imported ${grafanaURL}${result.url}`);
}
