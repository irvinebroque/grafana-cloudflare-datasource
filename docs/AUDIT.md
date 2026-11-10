# Bug audit: 0.1.1

Audited on 10 November 2026. This release fixes reproducible defects found with regression tests, malformed-response fuzzing, dependency scans, and real Cloudflare queries through Grafana.

## Correctness and resilience fixes

- Reject JSON `null`, non-object parameters, trailing JSON, and malformed or missing API result envelopes. GraphQL and telemetry parameters can no longer panic the backend or report malformed responses as successful empty queries.
- Parse GraphQL documents, accept leading comments and fragments, enforce query operations, and support explicit operation selection for documents containing multiple queries.
- Keep legacy Analytics Engine SQL separate from unified SQL when switching editor modes. Existing saved queries remain compatible. Legacy interval macros now use identifier syntax accepted by Analytics Engine.
- Honor custom time columns in raw frames and builder ordering; use one-second buckets for positive subsecond intervals; recognize nullable numeric, decimal, and boolean metadata; reject invalid and nonfinite numeric values instead of displaying zero.
- Use valid Container log defaults, reject duplicate result columns and builder aliases, and clear stale dataset metadata when switching to raw query modes.
- Paginate account and zone selectors, reject incomplete pagination, and bound the resource cache.
- Preserve the resolved account when loading log context and preserve the complete original query in log-volume metadata. Histograms that reach the row cap report limited coverage.
- Respect HTTP-date retry instructions and prevent integer overflow in numeric retry delays. Validate the actual catalog response before reporting a healthy connection.
- Bound frame conversion to 1,000 columns, two million cells, 10,000 time series, and 32 MiB of repeated series labels. Sparse responses and label duplication can otherwise expand far beyond the HTTP response size. Return actionable errors when these limits are exceeded.
- Produce Grafana-compatible SystemJS output on both initial and incremental Vite Plus watch builds.

## Dependency review

Go 1.27.3, `github.com/klauspost/compress` 1.20.2, and `golang.org/x/net` 0.62.0 include the security fixes identified during the audit. The final `govulncheck` reports no vulnerabilities. The minimatch override resolves the affected transitive version without changing Grafana's UI contract.

`pnpm audit --prod` still reports two moderate React Router advisories through `@grafana/ui` → `react-router-dom-v5-compat`: [GHSA-wrjc-x8rr-h8h6](https://github.com/advisories/GHSA-wrjc-x8rr-h8h6) and [GHSA-337j-9hxr-rhxg](https://github.com/advisories/GHSA-337j-9hxr-rhxg). The release imports Grafana UI from its host; React Router is absent from the plugin bundle and ZIP. The source map confirms Zod is the only bundled third-party library. Upgrading this dependency across React Router's major-version boundary requires a compatible Grafana UI release. The npm dependency tree is therefore not claimed to have a clean audit.

See [the validation record](VALIDATION.md) for automated and live results, including upstream failures and platform coverage. Testing does not establish that software has zero bugs.
