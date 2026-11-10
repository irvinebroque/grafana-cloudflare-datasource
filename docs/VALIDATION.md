# Validation record

Validated on 10 November 2026 against a real Cloudflare account with existing traffic. No synthetic Worker or billable resource was deployed.

## Environment

- Grafana Enterprise 13.3.0, using its standard datasource/Explore/dashboard/alerting capabilities.
- macOS arm64 host and native Grafana process bound to localhost.
- Official Grafana archive SHA-256 verified: `eb97ed1d38de122b743e00a678ea7a435a0476d46b25e0c11b662e123ae32348`.
- Vite Plus 1.5.1; Grafana frontend packages 13.3.0; Go plugin SDK 0.298.0.
- Strict TypeScript, all generic anti-slop rules enabled. A temporary intentionally invalid probe was rejected by anti-slop and removed.

## Live API round trips through Grafana

The core validation script passed all 11 checks: grouped Workers requests, weighted p99 CPU, logs, trace search, full distributed trace, parameterized SQL with interval macros, annotation SQL, variable SQL, GraphQL, legacy Analytics Engine SQL, and Workers telemetry.

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

Playwright drove an actual Chrome browser against Grafana, loading the installed SystemJS frontend and Go executable. Eight checks cover masked configuration and connection testing, the catalog and grouped metrics, real logs and clicking through to a matching trace, invocation log context and full-range histograms, trace waterfalls, raw SQL errors and recovery, the imported starter dashboard and variable, and Grafana server-side alert evaluation.

The alert test sends the datasource query to Grafana's alert evaluator and uses an expression reducer over each labeled series. It does not create a recurring alert or send notifications.

## Automated tests

Go tests cover parameter binding and injection rejection; dataset quoting; row caps; sampling; scope and time request bodies; labeled time-series grouping, nulls and sorting; trace field structure, units, errors and log correlation; empty and invalid data; query error isolation; cancellation; retries and credential redaction; resource route restrictions and catalog caching; GraphQL flattening; and legacy timestamp compatibility. Tests pass with the race detector.

Six Vite Plus tests cover SQL and builder interpolation boundaries, catalog metadata validation, Worker and Container context scoping and pagination bounds, and supplementary log-volume query construction. No module mocking is used. The account had no recent Container logs, so nonempty Container context semantics have not been live-validated.

The plugin manifest also validates against Grafana's official JSON schema. The release ZIP passed its integrity and SHA-256 checks, and was extracted for the final Grafana browser and API checks.

The plugin is unsigned. Tests validate a development installation with an explicit unsigned-plugin allowlist. The native macOS executable was exercised; cross-compiled release executables are build-validated, not executed on their target systems. Docker was unavailable, so the Compose configuration has not been exercised in this session.

Reports and screenshots from live validation are under ignored `.local/`; they can contain account data and are not included in the source or release ZIP.
