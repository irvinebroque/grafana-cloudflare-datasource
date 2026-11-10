# Token permissions

Start with the [API token setup](../README.md#api-token-permissions). This reference covers additional permissions and troubleshooting.

## SQL datasets and zones

**Account → Account Analytics → Read** is required for the plugin's SQL catalog and **Save & test**. Individual datasets and fields may also require permissions for the products whose data they expose. Account Analytics Read alone does not grant access to every product's data.

Zone-scoped SQL queries accept either **Zone → Analytics → Read** for the selected zones or **Account Analytics Read** for their owning account. Keep Account Analytics Read for the plugin's account-scoped catalog and connection test.

The optional **Zone → Zone → Read** permission enables zone browsing; it does not grant analytics access.

See Cloudflare's [SQL authorization requirements](https://developers.cloudflare.com/analytics/sql-api/get-started/), [catalog permissions](https://developers.cloudflare.com/analytics/sql-api/datasets/), and [zone listing permissions](https://developers.cloudflare.com/api/resources/zones/methods/list/).

## Workers telemetry

Add the account permission **Workers Observability Write** only when using the **Workers telemetry** query language. Cloudflare's [telemetry query endpoint](https://developers.cloudflare.com/api/resources/workers/subresources/observability/subresources/telemetry/methods/query/) requires this permission even to retrieve events; Workers Observability Read is not listed as an accepted permission.

The included Workers logs, Containers logs, and trace examples use the SQL API. They do not use the telemetry endpoint and do not by themselves require its Write permission.

## Other query languages

- **GraphQL:** Use the permissions required by the account or zone analytics you query. See [GraphQL authentication](https://developers.cloudflare.com/analytics/graphql-api/getting-started/authentication/api-token-auth/).
- **Analytics Engine SQL:** Uses Account Analytics Read. See [Analytics Engine authentication](https://developers.cloudflare.com/analytics/analytics-engine/sql-api/#authentication).

## Resources and audit

The **Resources and audit** editor shows the read permission for the selected operation. Add only permissions for operations you use, scoped to the appropriate accounts or zones. Resource browsing uses the same permission as the inventory operation; you can enter IDs manually.

| Operations                                          | Read permission                                                                |
| --------------------------------------------------- | ------------------------------------------------------------------------------ |
| DNS records / DNS Firewall                          | DNS Read (zone) / DNS Firewall Read (account)                                  |
| Load balancers / pools, origins, health             | Load Balancers Read (zone) / Load Balancing: Monitors and Pools Read (account) |
| Health Checks / certificates                        | Health Checks Read / SSL and Certificates Read (zone)                          |
| Tunnels                                             | Cloudflare Tunnel Read                                                         |
| Workers, deployments, Durable Object namespaces     | Workers Scripts Read                                                           |
| Pages and deployments / Containers                  | Pages Read / Workers Containers Read                                           |
| D1 / R2 / KV / Queues                               | D1 Read / Workers R2 Storage Read / Workers KV Storage Read / Queues Read      |
| AI Gateway / AI Search                              | AI Gateway Read / AI Search Read                                               |
| WAF rulesets and rules                              | Zone WAF Read                                                                  |
| Devices, Gateway rules, DLP profiles, CASB findings | Zero Trust Read                                                                |
| Posture checks / DEX tests and metrics              | Access: Device Posture Read / Cloudflare DEX Read                              |
| Audit Logs v2 and resource history                  | Account Settings Read                                                          |

Permissions without a zone label are account permissions. In the token editor, product permission groups may display **Read** separately from their name. Cloudflare may accept additional permission groups; these are the read-oriented choices exposed by the plugin. In particular, the [Audit Logs v2 endpoint](https://developers.cloudflare.com/api/resources/accounts/subresources/logs/subresources/audit/methods/list/) lists **Account Settings Read**, and the [DLP profiles endpoint](https://developers.cloudflare.com/api/resources/zero_trust/subresources/dlp/subresources/profiles/methods/list/) lists **Zero Trust Read**. See the [API permission reference](https://developers.cloudflare.com/fundamentals/api/reference/permissions/) and the [API schemas](https://github.com/cloudflare/api-schemas) for operation-specific alternatives.

## Troubleshooting

- **Save & test succeeds, but a query fails:** The connection test checks the SQL catalog. Verify the permissions for the query's API, dataset, fields, and account or zone.
- **Resource browsing fails:** Add the [optional browsing permissions](../README.md#api-token-permissions), or enter the account or zone ID directly.
- **Data is unavailable:** Check that the product is enabled, your plan supports the data, and the time range is within retention. Extra token permissions do not change these limits.

The setup link uses Cloudflare's documented [API token template URL](https://developers.cloudflare.com/fundamentals/api/how-to/account-owned-token-template/) to preselect Account Analytics Read. You can also create a custom token manually using the [permission reference](https://developers.cloudflare.com/fundamentals/api/reference/permissions/).
