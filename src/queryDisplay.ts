import { defaultQuery, type CloudflareQuery } from "./types";

export function queryDisplayText(query: CloudflareQuery): string {
  switch (query.queryType ?? "builder") {
    case "sql":
      return query.sql ?? "";
    case "analytics":
      return query.analyticsSql ?? query.sql ?? "";
    case "graphql-builder":
      return [query.dataset ?? "", ...(query.columns ?? []), ...(query.groupBy ?? [])].join(" · ");
    case "graphql":
      return query.graphql ?? "";
    case "telemetry":
      return query.telemetry ?? "";
    case "builder": {
      const parts = [query.dataset ?? defaultQuery.dataset ?? ""];

      for (const metric of query.metrics ?? []) {
        parts.push(`${metric.aggregation}(${metric.field || "*"}) AS ${metric.alias}`);
      }

      if (query.groupBy?.length) parts.push(`by ${query.groupBy.join(", ")}`);

      for (const filter of query.filters ?? []) {
        parts.push(`${filter.field} ${filter.operator} ${JSON.stringify(filter.value)}`);
      }

      if (query.traceId) parts.push(`traceId = ${JSON.stringify(query.traceId)}`);

      return parts.join(" · ");
    }
  }
}
