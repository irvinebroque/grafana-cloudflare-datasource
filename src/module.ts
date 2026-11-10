import { DataSourcePlugin } from "@grafana/data";
import { DataSource } from "./datasource";
import { ConfigEditor } from "./components/ConfigEditor";
import { QueryEditor } from "./components/QueryEditor";
import type { CloudflareOptions, CloudflareQuery, CloudflareSecrets } from "./types";

export const plugin = new DataSourcePlugin<
  DataSource,
  CloudflareQuery,
  CloudflareOptions,
  CloudflareSecrets
>(DataSource)
  .setConfigEditor(ConfigEditor)
  .setQueryEditor(QueryEditor);
