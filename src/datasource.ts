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
  type DataSourceWithToggleableQueryFiltersSupport,
} from "@grafana/data";
import { lastValueFrom } from "rxjs";
import { z } from "zod";
import { contextRange, contextTarget, volumeTarget } from "./logContext";
import { DataSourceWithBackend, getTemplateSrv } from "@grafana/runtime";
import {
  catalogSchema,
  graphCatalogSchema,
  operationsSchema,
  defaultQuery,
  resourceSchema,
  type CloudflareOptions,
  type CloudflareQuery,
} from "./types";
import { interpolateQuery } from "./interpolation";
import { queryDisplayText } from "./queryDisplay";
import { queryHasFilter, toggleQueryFilter } from "./queryFilters";

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
    DataSourceWithSupplementaryQueriesSupport<CloudflareQuery>,
    DataSourceWithToggleableQueryFiltersSupport<CloudflareQuery>
{
  queryHasFilter = queryHasFilter;
  toggleQueryFilter = toggleQueryFilter;

  override getQueryDisplayText(query: CloudflareQuery): string {
    return queryDisplayText(query);
  }

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
    const fields = row.dataFrame.fields;

    const value = (name: string) =>
      z
        .string()
        .optional()
        .catch(undefined)
        .parse(fields.find((field) => field.name === name)?.values[row.rowIndex]);

    const metadata = z
      .object({ accountId: z.string().optional(), dataset: z.string().optional() })
      .parse(row.dataFrame.meta?.custom ?? {});

    const dataset =
      metadata.dataset ||
      ((original?.queryType ?? "builder") === "builder" ? original?.dataset : undefined) ||
      (fields.some((field) => field.name === "containerId")
        ? "logs.containerLogs"
        : fields.some((field) => field.name === "scriptName")
          ? "logs.workersLogs"
          : "");

    if (!["logs.workersLogs", "logs.containerLogs"].includes(dataset)) return null;

    return contextTarget(
      {
        accountId: metadata.accountId,
        requestId: value("requestId"),
        scriptName: value("scriptName"),
        containerId: value("containerId"),
        placementId: value("placementId"),
      },
      {
        ...original,
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
      row.timeEpochMs,
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
      prepareQuery: (annotation) => {
        const target = annotation.target;

        if (!target) return undefined;

        if (
          target.queryType === "rest" &&
          target.operation === "worker-deployments" &&
          getTemplateSrv().replace(target.resourceId ?? "") === "%"
        )
          return undefined;

        return { ...target, format: "table" };
      },
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

  async graphDatasets(query: CloudflareQuery, dataset?: string, refresh = false) {
    const response = await this.getResource("graphql-datasets", {
      scope: query.scope ?? "account",
      accountId: getTemplateSrv().replace(query.accountId ?? ""),
      zoneId: getTemplateSrv().replace(query.zoneId ?? ""),
      dataset,
      refresh,
    });

    return graphCatalogSchema.parse(response).datasets;
  }

  async accounts() {
    return resourceSchema.parse(await this.getResource("accounts")).result;
  }

  async operations() {
    return operationsSchema.parse(await this.getResource("operations")).operations;
  }

  async resources(operation: string, query: CloudflareQuery) {
    const response = await this.getResource("resources", {
      operation,
      accountId: getTemplateSrv().replace(query.accountId ?? ""),
      zoneId: getTemplateSrv().replace(query.zoneId ?? ""),
    });

    return z.object({ result: resourceSchema.shape.result, limited: z.boolean() }).parse(response);
  }

  async zones(accountId?: string) {
    return resourceSchema.parse(
      await this.getResource("zones", {
        accountId: getTemplateSrv().replace(accountId ?? ""),
      }),
    ).result;
  }
}
