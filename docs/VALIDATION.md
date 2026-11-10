# Validation record

Version **0.1.1** validated on 10 November 2026 against a real Cloudflare account with existing traffic. No synthetic Worker or billable resource was deployed. See the [bug audit](AUDIT.md) for corrected defects and dependency findings.

## Platform analytics and native trace follow-up

The 10 November follow-up passed `vp check --deny-warnings`, all 16 frontend tests, Go race tests, the plugin build, `go vet`, and all **14 real-account browser tests**. The browser suite now checks shared GraphQL discovery, a nonempty metric query, the two-panel native Traces dashboard and its trace-selection link, Explore’s node graph, and the span-to-log link. The live core validator passed **16/16** checks, including a retained **11-span trace** returned as the native trace, nodes, and edges frames. The validator now chooses a trace ID from retained trace search and requires nonempty trace frames; an empty trace no longer passes full-trace validation.

GraphQL introspection discovered **230 account and 60 zone datasets**, excluding deprecated and staging nodes. Across **35 discovered analytics targets**, **13 returned real data**, **14 succeeded empty**, and **8 were disabled** for the selected account/zone/token. Nonempty results covered Workers, Durable Objects periodic and SQL storage, Pages Functions, AI Gateway requests, Queues backlog and operations, DNS, HTTP variants, Spectrum, DDoS, and audit analytics. Containers, AI Search, Access, Gateway, DNS Firewall, Tunnel, and some storage/Logpush queries returned empty results; this establishes compatibility, not populated product validation. A probe using an obsolete Spectrum node name was rejected by catalog validation; the discovered account-scoped Spectrum node returned real data.

The broader native log path returned nonempty Email Routing and issue events using discovered timestamps/columns and a canonical display message. A raw GraphQL HTTP-event query also returned five real events as native logs. Raw events default to logs with their exact event timestamp where available, aggregate datasets default to metrics, and datasets without time fields default to tables; numeric event IDs are not selected as default metrics. Sinkhole and storage-tracing log queries succeeded empty. Alternate timestamp/body schemas, absent severity, structured-field preservation, typed GraphQL filters, 64-bit integer precision, account availability/field/range/row limits, external trace parents, OTel status, graph deduplication, and native links also have synthetic fixture tests. Fixture data is not represented as real Cloudflare evidence.

Manual Grafana validation confirmed a native waterfall containing HTTP/cache/policy/framework and D1 operations, the native per-trace span graph, and a span link that returned matching Worker logs. These observed operation names are examples, not a parser allowlist. The trace dashboard contains only recent traces and the selected native waterfall. No resources were deployed or permissions expanded. Private account captures and credentials remain ignored in `.local/`.

The integration does not retrieve external spans or reconstruct sampling gaps. Operational REST inventory, direct Audit Logs v2 events, organization APIs, Radar, and host-local Prometheus collection remain explicit boundaries in [coverage](COVERAGE.md).

## Environment

- Grafana Enterprise 13.3.0, using its standard datasource/Explore/dashboard/alerting capabilities.
- macOS arm64 host and native Grafana process bound to localhost.
- Official Grafana archive SHA-256 verified: `eb97ed1d38de122b743e00a678ea7a435a0476d46b25e0c11b662e123ae32348`.
- Vite Plus 1.5.1; Grafana frontend packages 13.3.0; Go plugin SDK 0.298.0.
- Strict TypeScript, all generic anti-slop rules enabled. A temporary intentionally invalid probe was rejected by anti-slop and removed.

## Live API round trips through Grafana

The core validation script passed all 15 checks: grouped Workers requests, weighted p99 CPU, logs, trace search, full distributed trace, parameterized SQL with interval macros, annotation SQL, variable SQL, GraphQL, legacy Analytics Engine SQL, legacy grouped interval macros, Container log defaults, custom log timestamps, numeric builder filters, and Workers telemetry.

The SQL API catalog exposed 95 datasets. A catalog sweep executed a real, bounded SQL query through Grafana against every dataset. **91 succeeded**, including successful empty results where the account had no matching data. Two Logpush datasets required zone scope even though their catalog advertised accountTag columns.

Four datasets repeatedly returned Cloudflare HTTP 503 errors:

| Dataset                          | Cloudflare response                           |
| -------------------------------- | --------------------------------------------- |
| `events.cacheReserveOperations`  | Unable to verify permissions for this dataset |
| `states.cacheReserveStorage`     | Unable to verify permissions for this dataset |
| `states.imageResizingRequests1m` | Unable to verify permissions for this dataset |
| `states.kvStorage`               | Unable to evaluate query                      |

A separate aggregated KV storage query also returned HTTP 503. These datasets remain available in the editor; the backend surfaces the upstream errors and ray IDs. They are **not claimed as successfully validated** in this account. Empty successful queries validate request execution and frame handling; they do not prove nonempty semantics for products without account data.

## Browser and alert evaluation

Playwright drove an actual Chrome browser against Grafana, loading the installed SystemJS frontend and Go executable. Ten checks cover masked configuration and connection testing, the catalog and grouped metrics, real logs and clicking through to a matching trace, invocation log context and full-range histograms, trace waterfalls, raw SQL errors and recovery, null JSON rejection and recovery in both API modes, independent SQL dialects with a real legacy grouped query, the imported starter dashboard and variable, and Grafana server-side alert evaluation.

The alert test sends the datasource query to Grafana's alert evaluator and uses an expression reducer over each labeled series. It does not create a recurring alert or send notifications.

## Automated tests

All 32 Go tests pass with the race detector, covering parameter binding and injection rejection; dataset quoting; row caps; sampling; scope and time request bodies; labeled time-series grouping, nulls and sorting; trace field structure, units, errors and log correlation; empty and invalid data; query error isolation; cancellation; retries and credential redaction; pagination and catalog caching; GraphQL parsing and operation selection; legacy timestamp compatibility; and frame expansion limits. Backend statement coverage is 81.5%.

Ten Vite Plus tests cover SQL, telemetry and builder interpolation boundaries, query-mode migration and switching, catalog metadata validation, Worker and Container context scoping and pagination bounds, and supplementary log-volume query construction. Strict TypeScript and all generic anti-slop rules pass. No module mocking is used. The account had no recent Container logs, so nonempty Container context semantics have not been live-validated.

The final 30-second malformed-response fuzz run exercised 373,113 cases with no failures, converting accepted results into each frame format and Grafana's JSON encoding. An earlier run exercised another 405,660 cases. `go vet` and Staticcheck pass; Staticcheck's error-capitalization style rule is excluded to retain proper product names. Initial and incremental watch builds were checked for Grafana-compatible SystemJS output.

`govulncheck` reports no vulnerabilities after the dependency and Go toolchain upgrades. The npm dependency scan has two remaining moderate advisories in Grafana's external React Router dependency, detailed in the audit report.

The plugin manifest also validates against Grafana's official JSON schema. The release ZIP passed its integrity and SHA-256 checks, and was extracted for the final Grafana browser and API checks.

The plugin is unsigned. Tests validate a development installation with an explicit unsigned-plugin allowlist. The native macOS executable was exercised; cross-compiled release executables are build-validated, not executed on their target systems. Docker was unavailable, so the Compose configuration has not been exercised in this session.

Reports and screenshots from live validation are under ignored `.local/`; they can contain account data and are not included in the source or release ZIP.

## Simplification follow-up

After the 0.1.1 audit, the cleanup removed unused models, query flags, catalog properties, exports, an uncalled resource route, and obsolete Babel type dependencies. Builder defaults, metric defaults, result column discovery, numeric inference, row limits, and Grafana script requests now share their implementations. Grafana's concurrent query helper replaces the custom scheduler and retains the four-query limit. Production code decreased by 141 lines overall.

The follow-up passes 33 Go tests with the race detector, 10 Vite Plus tests, strict TypeScript and anti-slop checks, `go vet`, Staticcheck with the previously documented style exclusion, and all five platform builds. Backend statement coverage is 81.7%. A further 30-second fuzz run exercised 382,180 cases without failure. Knip found no unused files, exports, or dependencies after accounting for the required local Oxlint executable; Go's dead-code analyzer found no unreachable functions, both with and without tests. `govulncheck` remains clean.

All 10 browser checks and 15 live core API checks passed after restarting Grafana with the current build. The catalog sweep again passed 91 of 95 datasets, with the same four upstream 503 failures listed above. The dashboard installer also succeeded using the shared request helper. Local `--cf-auth` startup now validates the CLI login before reading its saved token, allowing `cf` to refresh expired credentials.

The log-link browser check verifies navigation to the exact log trace ID. An independently selected stored trace verifies the full waterfall, since ingestion delays or sampling can leave an individual log's trace unavailable.

## Grafana comparison follow-up: 0.1.2

The [comparison](COMPARISON.md) records the exact reference repository revisions and the changes selected from them. The follow-up adds Grafana code editors, catalog completion, keyboard execution, readable query history, native builder log-detail filters, numeric metric suggestions, shared metric units, and upstream error classification. It adds no datasource settings or runtime dependencies.

All 38 Go tests pass with the race detector, as do 15 Vite Plus unit tests and 12 browser checks against native Grafana 13.3.0. Strict TypeScript, formatting, all generic anti-slop rules, `go vet`, Staticcheck with the documented capitalization exclusion, Knip, and the Go dead-code analyzer pass. Backend package statement coverage is 84.5%. A 30-second malformed-response fuzz run completed 35,544 cases without a failure. The five supported platform executables build, and the manifest validates against Grafana's official schema.

Browser coverage now includes accepting a suggestion from the live SQL catalog, keyboard execution after changing another query setting, native include/exclude log filters, preserving the two SQL dialects, and checking that a real Cloudflare 422 arrives through Grafana with its status and downstream error source intact. Fixtures additionally verify authorization failures, rate limits, outages, timeouts, malformed responses in all four API modes, literal `$variable` text in log filters, and retaining trace links with filter metadata. Object and array log values do not advertise scalar filtering.

All 15 live core checks pass. The catalog sweep again succeeds for 91 of 95 datasets; `events.cacheReserveOperations`, `states.cacheReserveStorage`, `states.imageResizingRequests1m`, and `states.kvStorage` retain the previously recorded upstream 503 failures. Empty successful datasets establish API and frame compatibility, not the semantics of products without account data. Container log context still lacks a nonempty live dataset in this account.

`govulncheck` remains clean and is now a pinned CI step. The two existing moderate npm advisories in Grafana's external React Router dependency remain as described in the audit. The plugin remains unsigned, and cross-compiled executables still require execution on their target systems. Local release packaging includes ZIP integrity and SHA-256 verification; test reports and account-specific captures remain ignored.
