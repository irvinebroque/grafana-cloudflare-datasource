import type { CodeEditorSuggestionItem } from "@grafana/ui";
import type { CloudflareQuery, Dataset } from "./types";

export function sqlSuggestions(
  query: CloudflareQuery,
  datasets: Dataset[],
  variables: string[],
): CodeEditorSuggestionItem[] {
  const suggestions: CodeEditorSuggestionItem[] = [
    {
      label: "$__timeGroup(timestamp)",
      detail: "Bucket timestamps using the Grafana query interval",
    },
    { label: "$__interval_s", detail: "Query interval in seconds" },
    { label: "$__interval_ms", detail: "Query interval in milliseconds" },
    { label: "$__fromTime", detail: "Time range start, Unix seconds" },
    { label: "$__toTime", detail: "Time range end, Unix seconds" },
  ];

  for (const name of variables)
    suggestions.push({ label: `$${name}`, detail: "Dashboard variable" });

  const legacy = query.queryType === "analytics";
  const sql = legacy ? (query.analyticsSql ?? query.sql ?? "") : (query.sql ?? "");

  const sources = new Set(
    Array.from(sql.matchAll(/\b(?:FROM|JOIN)\s+([A-Za-z_`"][A-Za-z0-9_.`"-]*)/gi), (match) =>
      match[1]?.replace(/[`"]+/g, ""),
    ),
  );

  const seen = new Set<string>();

  for (const dataset of datasets) {
    if (dataset.hidden || (legacy && !dataset.name.startsWith("events.analyticsEngine."))) continue;

    const name = legacy ? dataset.name.slice("events.analyticsEngine.".length) : dataset.name;
    suggestions.push({
      label: name,
      insertText: name
        .split(".")
        .map((part) => (part.includes("-") ? `"${part}"` : part))
        .join("."),
      detail: dataset.title,
      documentation: dataset.description,
    });

    if (!sources.has(name)) continue;

    for (const column of dataset.columns ?? []) {
      if (seen.has(column.name)) continue;

      seen.add(column.name);
      suggestions.push({
        label: column.name,
        detail: column.data_type,
        documentation: column.description,
      });
    }

    for (const attribute of dataset.attributes ?? []) {
      const label = `attributes['${attribute.name}']`;

      if (seen.has(label) || /['\\\p{Cc}]/u.test(attribute.name)) continue;

      seen.add(label);
      suggestions.push({
        label,
        detail: attribute.data_type,
        documentation: attribute.description,
      });
    }
  }

  return suggestions;
}
