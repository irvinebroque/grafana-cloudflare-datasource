import {
  CoreApp,
  SupplementaryQueryType,
  type SupplementaryQueryOptions,
  type DataQueryRequest,
  type DataSourceWithSupplementaryQueriesSupport,
  dateTime,
  LogRowContextQueryDirection,
  type LogRowModel,
  type LogRowContextOptions,
  type DataSourceWithLogsContextSupport,
  DataSourceVariableSupport,
  type ScopedVars,
  type DataSourceInstanceSettings,
} from "@grafana/data";
import { lastValueFrom } from "rxjs";
import { z } from "zod";
import { contextRange, contextTarget, volumeTarget } from "./logContext";
import { DataSourceWithBackend, getTemplateSrv } from "@grafana/runtime";
import {
  catalogSchema,
  defaultQuery,
  resourceSchema,
  type CloudflareOptions,
  type CloudflareQuery,
} from "./types";
import { interpolateQuery } from "./interpolation";

class CloudflareVariables extends DataSourceVariableSupport<DataSource> {
  override getDefaultQuery(): Partial<CloudflareQuery> {
    return {
      queryType: "sql",
      format: "table",
      sql: "SELECT scriptName AS __text, scriptName AS __value FROM events.workersInvocations GROUP BY scriptName LIMIT 1000",
    };
  }
}

export class DataSource
  extends DataSourceWithBackend<CloudflareQuery, CloudflareOptions>
  implements
    DataSourceWithLogsContextSupport<CloudflareQuery>,
    DataSourceWithSupplementaryQueriesSupport<CloudflareQuery>
{
  getSupportedSupplementaryQueryTypes() {
    return [SupplementaryQueryType.LogsVolume];
  }

  getSupplementaryQuery(options: SupplementaryQueryOptions, original: CloudflareQuery) {
    return options.type === SupplementaryQueryType.LogsVolume ? volumeTarget(original) : undefined;
  }

  getSupplementaryRequest(
    type: SupplementaryQueryType,
    request: DataQueryRequest<CloudflareQuery>,
  ) {
    if (type !== SupplementaryQueryType.LogsVolume) return undefined;

    const targets = request.targets.flatMap((target) => {
      const volume = volumeTarget(target);

      return volume ? [volume] : [];
    });

    if (!targets.length) return undefined;

    const intervalMs = Math.max(
      1000,
      Math.ceil((request.range.to.valueOf() - request.range.from.valueOf()) / 100 / 1000) * 1000,
    );

    return { ...request, targets, intervalMs, interval: `${intervalMs}ms`, maxDataPoints: 100 };
  }

  supportsAdjustableWindow = true;

  async getLogRowContextQuery(
    row: LogRowModel,
    _options?: LogRowContextOptions,
    original?: CloudflareQuery,
  ): Promise<CloudflareQuery | null> {
    const request = row.dataFrame.fields.find((field) => field.name === "requestId");
    const script = row.dataFrame.fields.find((field) => field.name === "scriptName");
    const container = row.dataFrame.fields.find((field) => field.name === "containerId");
    const placement = row.dataFrame.fields.find((field) => field.name === "placementId");

    const metadata = z
      .object({ accountId: z.string().optional(), dataset: z.string().optional() })
      .parse(row.dataFrame.meta?.custom ?? {});

    const dataset =
      metadata.dataset ||
      ((original?.queryType ?? "builder") === "builder" ? original?.dataset : undefined) ||
      (container ? "logs.containerLogs" : script ? "logs.workersLogs" : "");

    if (!["logs.workersLogs", "logs.containerLogs"].includes(dataset)) return null;

    return contextTarget(
      {
        timestamp: row.timeEpochMs,
        accountId: metadata.accountId,
        requestId: z.string().optional().catch(undefined).parse(request?.values[row.rowIndex]),
        scriptName: z.string().optional().catch(undefined).parse(script?.values[row.rowIndex]),
        containerId: z.string().optional().catch(undefined).parse(container?.values[row.rowIndex]),
        placementId: z.string().optional().catch(undefined).parse(placement?.values[row.rowIndex]),
      },
      {
        ...original,
        refId: "context",
        accountId: original?.accountId || metadata.accountId,
        dataset,
      },
    );
  }

  async getLogRowContext(
    row: LogRowModel,
    options?: LogRowContextOptions,
    original?: CloudflareQuery,
  ) {
    const target = await this.getLogRowContextQuery(row, options, original);

    if (!target)
      throw new Error("Log context is supported for Workers and Containers log datasets.");

    const bounds = contextRange(
      { timestamp: row.timeEpochMs },
      options?.timeWindowMs ?? 60000,
      options?.direction,
    );

    const from = dateTime(bounds.from);
    const to = dateTime(bounds.to);
    target.limit = options?.limit ?? 100;
    target.logOrder = options?.direction === LogRowContextQueryDirection.Forward ? "asc" : "desc";

    return lastValueFrom(
      this.query({
        requestId: `context-${row.uid}`,
        app: CoreApp.Explore,
        interval: "1s",
        intervalMs: 1000,
        maxDataPoints: 1000,
        range: { from, to, raw: { from, to } },
        scopedVars: options?.scopedVars ?? {},
        targets: [target],
        startTime: Date.now(),
        timezone: "utc",
      }),
    );
  }
  constructor(settings: DataSourceInstanceSettings<CloudflareOptions>) {
    super(settings);
    this.variables = new CloudflareVariables();
    this.annotations = {
      prepareQuery: (annotation) =>
        annotation.target ? { ...annotation.target, format: "table" } : undefined,
      getDefaultQuery: () => ({
        queryType: "sql",
        format: "table",
        sql: "SELECT timestamp AS time, message AS text FROM logs.workersLogs ORDER BY timestamp DESC LIMIT 100",
      }),
    };
  }

  override getDefaultQuery(): Partial<CloudflareQuery> {
    return defaultQuery;
  }

  override filterQuery(query: CloudflareQuery): boolean {
    return !query.hide;
  }

  override applyTemplateVariables(query: CloudflareQuery, scopedVars: ScopedVars): CloudflareQuery {
    return interpolateQuery(query, (value, format) =>
      getTemplateSrv().replace(value, scopedVars, format),
    );
  }

  async datasets(accountId?: string, dataset?: string, refresh = false) {
    const response = await this.getResource("datasets", {
      accountId: getTemplateSrv().replace(accountId ?? ""),
      dataset,
      refresh,
    });

    return catalogSchema.parse(response).datasets;
  }

  async accounts() {
    return resourceSchema.parse(await this.getResource("accounts")).result;
  }

  async zones(accountId?: string) {
    return resourceSchema.parse(
      await this.getResource("zones", {
        accountId: getTemplateSrv().replace(accountId ?? ""),
      }),
    ).result;
  }
}
