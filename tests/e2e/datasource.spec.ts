import { test, expect, type Page, type Locator } from "@playwright/test";
import { z } from "zod";

const uid = process.env.GRAFANA_DATASOURCE_UID ?? "cloudflare-live";
const fieldSchema = z.object({ name: z.string(), type: z.string() });

const resultSchema = z.object({
  results: z.record(
    z.string(),
    z.object({
      error: z.string().optional(),
      status: z.number().optional(),
      errorSource: z.string().optional(),
      frames: z
        .array(
          z.object({
            schema: z.object({ fields: z.array(fieldSchema) }),
            data: z.object({
              values: z.array(z.array(z.union([z.string(), z.number(), z.boolean(), z.null()]))),
            }),
          }),
        )
        .optional(),
    }),
  ),
});

const requestSchema = z.object({ queries: z.array(z.object({ refId: z.string() })) });

async function mainQuery(page: Page) {
  const request = await page.waitForRequest((request) => {
    if (!request.url().includes("/api/ds/query") || request.method() !== "POST") return false;
    const parsed = requestSchema.safeParse(request.postDataJSON());

    return parsed.success && parsed.data.queries.some((query) => query.refId === "A");
  });

  const response = await request.response();

  if (!response) throw new Error("Grafana query did not return a response");

  return response;
}

test("keeps the configured token masked and tests the provisioned connection", async ({ page }) => {
  await page.goto(`/connections/datasources/edit/${uid}`);
  await expect(page.getByLabel("Default account ID")).toBeVisible();
  await expect(page.getByLabel("API token", { exact: true })).toHaveValue("configured");
  await expect(page.getByLabel("API token", { exact: true })).toBeDisabled();
  await page.getByRole("button", { name: "Test", exact: true }).click();
  await expect(page.getByText(/Connected to Cloudflare SQL API/)).toBeVisible();
});

async function editCode(input: Locator, value: string) {
  await input.click();
  await input.press("ControlOrMeta+A");
  await input.press("Backspace");
  await input.page().keyboard.insertText(value);
}

function codeEditor(page: Page, label: string) {
  return page.locator(".monaco-editor").filter({ has: page.getByRole("textbox", { name: label }) });
}

async function example(page: Page, name: string) {
  await page.getByLabel("Query examples").click();
  await page.getByText(name, { exact: true }).click();

  const response = mainQuery(page);

  await page.getByRole("button", { name: "Run Cloudflare query" }).click();
  const result = resultSchema.parse(await (await response).json());
  expect(result.results.A?.error).toBeUndefined();

  return result;
}

test("loads the plugin, discovers the catalog, and renders grouped real metrics", async ({
  page,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto("/explore");
  await expect(page.getByLabel("Query language")).toBeVisible();
  await page.getByText(/Dataset schema · [1-9]/).waitFor();
  const result = await example(page, "Workers · requests");
  expect(result.results.A?.frames?.length).toBeGreaterThan(0);
  await expect(page.getByText("Graph", { exact: true }).last()).toBeVisible();
  await page.screenshot({ path: ".local/metrics-validated.png", fullPage: true });
  expect(errors).toEqual([]);
});

test("shows real logs and exposes trace correlation", async ({ page }) => {
  await page.goto("/explore");
  const result = await example(page, "Workers · logs");
  const fields = result.results.A?.frames?.[0]?.schema.fields.map((field) => field.name);
  expect(fields).toContain("traceId");
  expect(fields?.slice(0, 2)).toEqual(["timestamp", "message"]);
  const trace = result.results.A?.frames?.[0]?.data.values[fields?.indexOf("traceId") ?? -1]?.[0];
  expect(trace).toBeTruthy();
  await expect(page.getByText("Show log level", { exact: true })).toBeVisible();
  await page.screenshot({ path: ".local/logs-validated.png", fullPage: true });
  await page.getByRole("button", { name: "Log menu", exact: true }).first().click();
  await page.getByText("Show log details", { exact: true }).click();
  await page.getByRole("link", { name: "View trace", exact: true }).first().click();
  await expect(page.getByLabel("Trace ID")).toHaveValue(String(trace));
});

test("fetches invocation log context and a separate full-range volume query", async ({ page }) => {
  await page.goto("/explore");

  const volume = page.waitForResponse((response) => {
    if (!response.url().includes("/api/ds/query")) return false;
    const parsed = requestSchema.safeParse(response.request().postDataJSON());

    return parsed.success && parsed.data.queries.some((query) => query.refId === "log-volume-A");
  });

  await example(page, "Workers · logs");
  const volumeJSON = await (await volume).json();
  const volumeResult = resultSchema.parse(volumeJSON);
  expect(volumeResult.results["log-volume-A"]?.error).toBeUndefined();
  expect(volumeResult.results["log-volume-A"]?.frames?.length).toBeGreaterThan(0);

  const metadata = z
    .object({
      results: z.record(
        z.string(),
        z.object({
          frames: z.array(
            z.object({
              schema: z.object({
                meta: z.object({
                  custom: z.object({
                    sourceQuery: z.object({ refId: z.string() }),
                    logsVolumeType: z.string(),
                  }),
                }),
              }),
            }),
          ),
        }),
      ),
    })
    .parse(volumeJSON);

  expect(metadata.results["log-volume-A"]?.frames[0]?.schema.meta.custom.sourceQuery.refId).toBe(
    "A",
  );
  expect(metadata.results["log-volume-A"]?.frames[0]?.schema.meta.custom.logsVolumeType).toBe(
    "FullRange",
  );
  await page.getByRole("button", { name: "Log menu", exact: true }).first().click();

  const context = page.waitForResponse((response) => {
    if (!response.url().includes("/api/ds/query")) return false;
    const parsed = requestSchema.safeParse(response.request().postDataJSON());

    return parsed.success && parsed.data.queries.some((query) => query.refId === "context");
  });

  await page.getByText("Show context", { exact: true }).click();
  const contextResult = resultSchema.parse(await (await context).json());
  expect(contextResult.results.context?.error).toBeUndefined();
  await expect(page.getByText("Context time window", { exact: true })).toBeVisible();
  await page.screenshot({ path: ".local/context-validated.png", fullPage: true });
});

test("searches traces and renders an entire distributed trace", async ({ page, request }) => {
  const response = await request.post("/api/ds/query", {
    data: {
      from: String(Date.now() - 3600000),
      to: String(Date.now()),
      queries: [
        {
          refId: "A",
          datasource: { uid, type: "cloudflare-cloudflare-datasource" },
          queryType: "sql",
          format: "table",
          sql: "SELECT traceId FROM logs.traces ORDER BY startTime DESC LIMIT 1",
        },
      ],
    },
  });

  const result = resultSchema.parse(await response.json());
  const trace = result.results.A?.frames?.[0]?.data.values[0]?.[0];
  expect(trace).toBeTruthy();

  const pane = {
    test: {
      datasource: uid,
      queries: [
        {
          refId: "A",
          datasource: { uid, type: "cloudflare-cloudflare-datasource" },
          queryType: "builder",
          format: "traces",
          dataset: "logs.traces",
          traceId: trace,
          limit: 10000,
        },
      ],
      range: { from: "now-1h", to: "now" },
    },
  };

  await page.goto(`/explore?schemaVersion=1&panes=${encodeURIComponent(JSON.stringify(pane))}`);
  await expect(page.getByLabel("Trace ID")).toHaveValue(String(trace));
  await expect(page.getByText("Service & Operation", { exact: false }).first()).toBeVisible();
  await expect(page.getByRole("heading", { name: "Node graph", exact: true })).toBeVisible();
  await page.getByRole("heading", { name: "Node graph", exact: true }).getByRole("button").click();
  await expect(page.getByRole("button", { name: "Zoom in", exact: true })).toBeVisible();
  await page.getByRole("heading", { name: "Node graph", exact: true }).getByRole("button").click();
  await page
    .getByRole("switch")
    .filter({ hasText: /GET|POST|fetch/ })
    .first()
    .click();
  await expect(
    page.getByRole("link", { name: "Related Worker logs", exact: true }).first(),
  ).toBeVisible();
  await page.screenshot({ path: ".local/trace-validated.png", fullPage: true });
});

test("edits raw SQL and recovers after an upstream validation error", async ({ page }) => {
  await page.goto("/explore");
  await page.getByLabel("Query language").click();
  await page.getByText("SQL", { exact: true }).click();
  const sql = page.getByLabel("SQL query");
  await editCode(sql, "SELECT missingColumn FROM events.workersInvocations LIMIT 1");
  const failed = mainQuery(page);
  await page.getByRole("button", { name: "Run Cloudflare query" }).click();
  const failure = resultSchema.parse(await (await failed).json()).results.A;
  expect(failure?.error).toContain("Cloudflare API HTTP 422");
  expect(failure?.status).toBe(422);
  expect(failure?.errorSource).toBe("downstream");
  await editCode(
    sql,
    "SELECT $__timeGroup(timestamp) AS time, COUNT(*) AS requests FROM events.workersInvocations GROUP BY time ORDER BY time LIMIT 1000",
  );
  const recovered = mainQuery(page);
  await page.getByRole("button", { name: "Run Cloudflare query" }).click();
  expect(resultSchema.parse(await (await recovered).json()).results.A?.error).toBeUndefined();
});

test("completes SQL from the live catalog and runs with the keyboard", async ({ page }) => {
  await page.goto("/explore");
  await page.getByText(/Dataset schema · [1-9]/).waitFor();
  await page.getByLabel("Query language").click();
  await page.getByText("SQL", { exact: true }).click();
  const sql = page.getByLabel("SQL query");
  await editCode(sql, "SELECT 1 AS value FROM events.workers");
  await sql.press("Control+Space");
  await page.getByRole("option").filter({ hasText: "events.workersInvocations" }).first().click();
  await expect(codeEditor(page, "SQL query")).toContainText("events.workersInvocations");
  await editCode(sql, "SELECT COUNT(*) AS requests FROM events.workersInvocations");
  await page.getByLabel("Result format").click();
  await page.getByText("Table", { exact: true }).click();
  // Editing again must retain settings changed after Monaco mounted.
  await editCode(sql, "SELECT COUNT(*) AS requests FROM events.workersInvocations LIMIT 1");
  const response = mainQuery(page);
  await sql.press("Shift+Enter");
  const result = await response;
  expect(resultSchema.parse(await result.json()).results.A?.error).toBeUndefined();

  const request = z
    .object({ queries: z.array(z.object({ format: z.string(), sql: z.string() })) })
    .parse(result.request().postDataJSON());

  expect(request.queries[0]?.format).toBe("table");
  expect(request.queries[0]?.sql).toContain("LIMIT 1");
  await page.screenshot({ path: ".local/sql-editor-validated.png", fullPage: true });
});

test("filters builder logs from native Grafana log details", async ({ page }) => {
  for (const action of ["for", "out"]) {
    await page.goto("/explore");
    await example(page, "Workers · logs");
    await page.getByRole("button", { name: "Log menu", exact: true }).first().click();
    await page.getByText("Show log details", { exact: true }).click();
    const response = mainQuery(page);
    await page
      .getByRole("button", { name: `Filter ${action} value in query A`, exact: true })
      .first()
      .click();
    const result = await response;
    expect(resultSchema.parse(await result.json()).results.A?.error).toBeUndefined();

    const request = z
      .object({
        queries: z.array(
          z.object({
            filters: z.array(
              z.object({ field: z.string(), operator: z.string(), value: z.string() }),
            ),
          }),
        ),
      })
      .parse(result.request().postDataJSON());

    expect(request.queries[0]?.filters).toEqual([
      { field: "level", operator: action === "for" ? "=" : "!=", value: expect.any(String) },
    ]);
  }
});

test("rejects null JSON without crashing and recovers in both raw API modes", async ({ page }) => {
  await page.goto("/explore");

  for (const mode of ["GraphQL", "Workers telemetry"]) {
    await page.getByLabel("Query language").click();
    await page.getByText(mode, { exact: true }).click();
    const input = page.getByLabel(mode === "GraphQL" ? "GraphQL variables" : "Telemetry query");

    const valid =
      mode === "GraphQL"
        ? "{}"
        : '{"view":"events","limit":100,"parameters":{"datasets":["cloudflare-workers"],"filters":[],"calculations":[]}}';

    await editCode(input, "null");
    const failed = mainQuery(page);
    await page.getByRole("button", { name: "Run Cloudflare query" }).click();
    expect(resultSchema.parse(await (await failed).json()).results.A?.error).toContain(
      "JSON object",
    );
    await editCode(input, valid);
    const recovered = mainQuery(page);
    await page.getByRole("button", { name: "Run Cloudflare query" }).click();
    expect(resultSchema.parse(await (await recovered).json()).results.A?.error).toBeUndefined();
  }
});

test("keeps separate SQL dialects when switching query languages", async ({ page, request }) => {
  const catalog = z
    .object({ datasets: z.array(z.object({ name: z.string() })) })
    .parse(await (await request.get(`/api/datasources/uid/${uid}/resources/datasets`)).json());

  const dataset = catalog.datasets.find((entry) =>
    entry.name.startsWith("events.analyticsEngine."),
  );

  test.skip(!dataset, "The account has no Analytics Engine dataset");

  if (!dataset) return;
  const name = dataset.name.slice("events.analyticsEngine.".length);
  expect(name).toMatch(/^[A-Za-z_][A-Za-z0-9_]*$/);

  const unified =
    "SELECT $__timeGroup(timestamp) AS time, COUNT(*) AS requests FROM events.workersInvocations GROUP BY time ORDER BY time LIMIT 10";

  const legacy = `SELECT $__timeGroup(timestamp) AS time, SUM(_sample_interval) AS events FROM ${name} WHERE timestamp >= toDateTime($__fromTime) AND timestamp <= toDateTime($__toTime) GROUP BY time ORDER BY time LIMIT 10`;
  await page.goto("/explore");
  await page.getByLabel("Query language").click();
  await page.getByText("SQL", { exact: true }).click();
  await editCode(page.getByLabel("SQL query"), unified);
  await page.getByLabel("Query language").click();
  await page.getByText("Analytics Engine SQL", { exact: true }).click();
  await expect(codeEditor(page, "SQL query")).toContainText("your_dataset");
  await editCode(page.getByLabel("SQL query"), legacy);
  await page.getByLabel("Query language").click();
  await page.getByText("SQL", { exact: true }).click();
  await expect(codeEditor(page, "SQL query")).toContainText(unified);
  await page.getByLabel("Query language").click();
  await page.getByText("Analytics Engine SQL", { exact: true }).click();
  await expect(codeEditor(page, "SQL query")).toContainText(legacy);
  const response = mainQuery(page);
  await page.getByRole("button", { name: "Run Cloudflare query" }).click();
  expect(resultSchema.parse(await (await response).json()).results.A?.error).toBeUndefined();
});

test("renders the starter dashboard with its SQL query variable", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto("/d/cloudflare-overview/cloudflare-overview");
  await expect(page.getByText("Worker requests", { exact: true })).toBeVisible();
  await expect(page.getByText("Worker CPU · p99", { exact: true })).toBeVisible();
  await expect(page.getByText("D1 query latency", { exact: true })).toBeVisible();
  await expect(page.getByText("Worker", { exact: true }).first()).toBeVisible();
  await expect(page.getByRole("button", { name: "Cancel", exact: true })).toBeHidden({
    timeout: 30000,
  });
  await expect(page.locator("canvas").first()).toBeVisible();
  await page
    .getByText("Recent traces · click a trace ID to inspect", { exact: true })
    .scrollIntoViewIfNeeded();
  await expect(page.getByRole("button", { name: "Cancel", exact: true })).toBeHidden({
    timeout: 30000,
  });
  await page.screenshot({ path: ".local/dashboard-validated.png", fullPage: true });
  expect(errors).toEqual([]);
});

test("supports server-side Grafana alert evaluation", async ({ request }) => {
  const model = {
    refId: "A",
    queryType: "builder",
    format: "timeseries",
    dataset: "events.workersInvocations",
    metrics: [{ aggregation: "count", alias: "requests" }],
    groupBy: ["scriptName"],
    intervalMs: 60000,
    maxDataPoints: 1000,
  };

  const response = await request.post("/api/v1/eval", {
    data: {
      from: new Date(Date.now() - 3600000).toISOString(),
      to: new Date().toISOString(),
      condition: "B",
      data: [
        { refId: "A", datasourceUid: uid, relativeTimeRange: { from: 3600, to: 0 }, model },
        {
          refId: "B",
          datasourceUid: "__expr__",
          relativeTimeRange: { from: 3600, to: 0 },
          model: {
            refId: "B",
            type: "reduce",
            expression: "A",
            reducer: "last",
            conditions: [],
            datasource: { type: "__expr__", uid: "__expr__" },
          },
        },
      ],
    },
  });

  expect(response.ok()).toBe(true);
  const result = resultSchema.parse(await response.json());
  expect(result.results.B?.error).toBeUndefined();
  expect(result.results.B?.frames?.length).toBeGreaterThan(0);
  expect(result.results.B?.frames?.[0]?.data.values[0]?.length).toBe(1);
});

test("discovers GraphQL analytics and renders real metrics through the shared builder", async ({
  page,
}) => {
  await page.goto("/explore");
  await page.getByLabel("Query language").click();
  await page.getByText("GraphQL builder", { exact: true }).click();
  await expect(
    page.getByText("Available for this scope and token", { exact: false }),
  ).toBeVisible();
  await expect(page.getByLabel("GraphQL dataset")).toBeVisible();
  const response = mainQuery(page);
  await page.getByRole("button", { name: "Run Cloudflare query" }).click();
  const result = resultSchema.parse(await (await response).json());
  expect(result.results.A?.error).toBeUndefined();
  expect(result.results.A?.frames?.some((frame) => frame.data.values[0]?.length)).toBe(true);
  await expect(page.getByText("Graph", { exact: true }).last()).toBeVisible();
});

test("renders the simple trace dashboard with Grafana's native viewer", async ({ page }) => {
  await page.goto("/d/cloudflare-traces/cloudflare-traces?from=now-6h&to=now");
  await expect(page.getByText("Service & Operation", { exact: false }).first()).toBeVisible();
  await expect(page.getByText("Recent traces", { exact: true })).toBeVisible();
  const secondTrace = page.getByRole("grid").getByRole("row").nth(2).getByRole("gridcell").first();
  const traceId = (await secondTrace.innerText()).trim();
  await secondTrace.click();
  await page.getByRole("link", { name: "Inspect trace", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`var-traceId=${traceId}`));
  await expect(
    page.getByRole("heading", { name: `Trace · ${traceId}`, exact: true }),
  ).toBeVisible();
  await expect(page.getByText("Service & Operation", { exact: false }).first()).toBeVisible();
  await page.screenshot({ path: ".local/native-trace-dashboard.png", fullPage: true });
});
