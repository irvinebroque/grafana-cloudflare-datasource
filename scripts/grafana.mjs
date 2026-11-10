export const grafanaURL = process.env.GRAFANA_URL ?? "http://localhost:3100";
export const datasourceUID = process.env.GRAFANA_DATASOURCE_UID ?? "cloudflare-live";

const headers = { "Content-Type": "application/json" };

if (process.env.GRAFANA_TOKEN) headers.Authorization = `Bearer ${process.env.GRAFANA_TOKEN}`;

export async function grafanaRequest(path, body) {
  const response = await fetch(grafanaURL + path, {
    method: body ? "POST" : "GET",
    headers,
    body: body ? JSON.stringify(body) : undefined,
  });

  const result = await response.json();

  if (!response.ok && !(path === "/api/ds/query" && result.results))
    throw new Error(`Grafana HTTP ${response.status}: ${JSON.stringify(result)}`);

  return result;
}
