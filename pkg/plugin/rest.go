package plugin

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

// Only these GET operations are exposed. Projections intentionally omit credentials,
// request/response payloads, origin headers, and deployment environment variables.
type restOperation struct {
	Name       string   `json:"name"`
	Title      string   `json:"title"`
	Scope      string   `json:"scope"`
	Resource   string   `json:"resource,omitempty"`
	Parameters []string `json:"parameters,omitempty"`
	Columns    []string `json:"columns"`
	Format     string   `json:"format"`
	TimeField  string   `json:"timeField,omitempty"`
	Permission string   `json:"permission"`
	IDField    string   `json:"-"`
	NameField  string   `json:"-"`
	path       string
	fields     string
	resultPath string
	paging     string
	cursorPath string
	from       string
	to         string
	transform  string
}

var restOperations = []restOperation{
	{Name: "dns-records", Title: "DNS · records", Scope: "zone", path: "/zones/{scope}/dns_records", fields: "id name type content proxied proxiable ttl created_on modified_on", paging: "page", Permission: "DNS Read"},
	{Name: "dns-firewall", Title: "DNS Firewall · clusters", Scope: "account", path: "/accounts/{scope}/dns_firewall", fields: "id name dns_firewall_ips upstream_ips ratelimit retries modified_on", paging: "page", Permission: "DNS Firewall Read"},
	{Name: "load-balancers", Title: "Load Balancing · load balancers", Scope: "zone", path: "/zones/{scope}/load_balancers", fields: "id name enabled proxied default_pools fallback_pool steering_policy created_on modified_on", Permission: "Load Balancers Read"},
	{Name: "pools", Title: "Load Balancing · pools", Scope: "account", path: "/accounts/{scope}/load_balancers/pools", fields: "id name enabled monitor minimum_origins check_regions created_on modified_on", Permission: "Load Balancing: Monitors and Pools Read"},
	{Name: "pool-origins", Title: "Load Balancing · origins", Scope: "account", path: "/accounts/{scope}/load_balancers/pools/{resource}", Resource: "pools", fields: "poolId name address enabled port weight", transform: "origins", Permission: "Load Balancing: Monitors and Pools Read"},
	{Name: "pool-health", Title: "Load Balancing · origin health", Scope: "account", path: "/accounts/{scope}/load_balancers/pools/{resource}/health", Resource: "pools", fields: "poolId region origin healthy healthValue failure_reason response_code rtt rttMs", transform: "pool-health", Permission: "Load Balancing: Monitors and Pools Read"},
	{Name: "health-checks", Title: "Health Checks · status", Scope: "zone", path: "/zones/{scope}/healthchecks", fields: "id name address type status suspended failure_reason interval created_on modified_on", paging: "page", Permission: "Health Checks Read"},
	{Name: "certificates", Title: "TLS · edge certificate validity", Scope: "zone", path: "/zones/{scope}/ssl/certificate_packs", fields: "id packId hosts issuer status packStatus expires_on daysUntilExpiry expired uploaded_on modified_on", transform: "certificates", paging: "page", Permission: "SSL and Certificates Read"},
	{Name: "origin-certificates", Title: "TLS · origin certificate validity", Scope: "zone", path: "/certificates", fields: "id hostnames expires_on daysUntilExpiry expired requested_validity request_type", transform: "expiry", paging: "page", Permission: "SSL and Certificates Read"},
	{Name: "custom-certificates", Title: "TLS · custom certificate validity", Scope: "zone", path: "/zones/{scope}/custom_certificates", fields: "id hosts issuer status expires_on daysUntilExpiry expired uploaded_on modified_on", transform: "expiry", paging: "page", Permission: "SSL and Certificates Read"},
	{Name: "tunnels", Title: "Tunnel · status", Scope: "account", path: "/accounts/{scope}/cfd_tunnel", fields: "id name status healthValue created_at deleted_at conns_active_at conns_inactive_at remote_config", paging: "page", Parameters: []string{"name", "status"}, Permission: "Cloudflare Tunnel Read"},
	{Name: "workers", Title: "Workers · inventory", Scope: "account", path: "/accounts/{scope}/workers/scripts", fields: "id etag created_on modified_on last_deployed_from usage_model routes logpush", Permission: "Workers Scripts Read"},
	{Name: "worker-deployments", Title: "Workers · deployment history", Scope: "account", path: "/accounts/{scope}/workers/scripts/{resource}/deployments", Resource: "workers", fields: "id created_on source strategy versions", resultPath: "result.deployments", paging: "page", from: "since", to: "until", TimeField: "created_on", Format: "logs", Permission: "Workers Scripts Read"},
	{Name: "pages", Title: "Pages · projects", Scope: "account", path: "/accounts/{scope}/pages/projects", fields: "id name subdomain created_on domains production_branch", paging: "page", IDField: "name", Permission: "Pages Read"},
	{Name: "pages-deployments", Title: "Pages · deployment history", Scope: "account", path: "/accounts/{scope}/pages/projects/{resource}/deployments", Resource: "pages", fields: "id project_name environment created_on modified_on url latest_stage.name latest_stage.status latest_stage.started_on latest_stage.ended_on", paging: "page", Parameters: []string{"env"}, TimeField: "created_on", Format: "logs", Permission: "Pages Read"},
	{Name: "containers", Title: "Containers · applications", Scope: "account", path: "/accounts/{scope}/containers/applications", fields: "id name created_at instances scheduling_policy", paging: "page_token", cursorPath: "result_info.next_page_token", Permission: "Workers Containers Read"},
	{Name: "durable-namespaces", Title: "Durable Objects · namespaces", Scope: "account", path: "/accounts/{scope}/workers/durable_objects/namespaces", fields: "id name script class use_sqlite", paging: "page", Permission: "Workers Scripts Read"},
	{Name: "d1", Title: "D1 · databases", Scope: "account", path: "/accounts/{scope}/d1/database", fields: "uuid name version num_tables file_size created_at", paging: "page", IDField: "uuid", Permission: "D1 Read"},
	{Name: "r2", Title: "R2 · buckets", Scope: "account", path: "/accounts/{scope}/r2/buckets", fields: "name creation_date location storage_class", resultPath: "result.buckets", paging: "cursor", cursorPath: "result_info.cursor", IDField: "name", Permission: "Workers R2 Storage Read"},
	{Name: "kv", Title: "KV · namespaces", Scope: "account", path: "/accounts/{scope}/storage/kv/namespaces", fields: "id title supports_url_encoding", paging: "page", NameField: "title", Permission: "Workers KV Storage Read"},
	{Name: "queues", Title: "Queues · inventory", Scope: "account", path: "/accounts/{scope}/queues", fields: "queue_id queue_name created_on modified_on producers_total consumers_total", paging: "page", IDField: "queue_id", NameField: "queue_name", Permission: "Queues Read"},
	{Name: "ai-gateways", Title: "AI Gateway · gateways", Scope: "account", path: "/accounts/{scope}/ai-gateway/gateways", fields: "id name created_at modified_at log_management cache_ttl rate_limiting_limit", paging: "page", Permission: "AI Gateway Read"},
	{Name: "ai-search", Title: "AI Search · instances", Scope: "account", path: "/accounts/{scope}/ai-search/instances", fields: "id name status created_at modified_at paused source ai_gateway_id", paging: "page", Permission: "AI Search Read"},
	{Name: "rulesets", Title: "WAF · rulesets", Scope: "zone", path: "/zones/{scope}/rulesets", fields: "id name description kind phase last_updated version", Permission: "Zone WAF Read"},
	{Name: "rules", Title: "WAF · rules", Scope: "zone", path: "/zones/{scope}/rulesets/{resource}", Resource: "rulesets", fields: "id ref description action enabled expression last_updated version", resultPath: "result.rules", Permission: "Zone WAF Read"},
	{Name: "devices", Title: "Zero Trust · devices", Scope: "account", path: "/accounts/{scope}/devices/physical-devices", fields: "id name device_type os_version os_distro_name os_distro_revision serial_number manufacturer model last_seen last_seen_user.email created_at updated_at active_registrations", paging: "cursor", cursorPath: "result_info.cursor", Parameters: []string{"search"}, Permission: "Zero Trust Read"},
	{Name: "posture-rules", Title: "Zero Trust · posture checks", Scope: "account", path: "/accounts/{scope}/devices/posture", fields: "id name description type enabled schedule expiration", Permission: "Access: Device Posture Read"},
	{Name: "dex-tests", Title: "DEX · tests and results", Scope: "account", path: "/accounts/{scope}/dex/tests/overview", fields: "id name description kind enabled host interval httpResults tracerouteResults created updated", resultPath: "result.tests", paging: "page", Permission: "Cloudflare DEX Read"},
	{Name: "dex-http", Title: "DEX · HTTP test analytics", Scope: "account", path: "/accounts/{scope}/dex/http-tests/{resource}", Resource: "dex-tests", fields: "id time name availabilityPct dnsResponseTimeMs resourceFetchTimeMs serverResponseTimeMs", transform: "dex-http", TimeField: "time", Format: "timeseries", from: "from", to: "to", Parameters: []string{"interval", "colo"}, Permission: "Cloudflare DEX Read"},
	{Name: "dex-device", Title: "DEX · device metrics", Scope: "account", path: "/accounts/{scope}/dex/devices/{resource}/fleet-status/over-time", Resource: "devices", fields: "id time cpu_pct memory_pct disk_usage_pct disk_read_bps disk_write_bps battery_pct battery_cycles network_rcvd_bps network_sent_bps connection_type mode", resultPath: "result.over_time", transform: "dex-device", from: "from", to: "to", TimeField: "time", Format: "timeseries", Parameters: []string{"interval", "colo"}, Permission: "Cloudflare DEX Read"},
	{Name: "casb-findings", Title: "CASB · findings", Scope: "account", path: "/accounts/{scope}/data-security/posture/findings", fields: "id finding.id finding.name finding.description finding.severity finding.vendor ignored instance_count active_count archived_count latest_affliction_date integration.id integration.name integration.status", paging: "cursor", cursorPath: "result_info.cursor", Parameters: []string{"severity", "ignored", "integration_id", "type", "vendor", "search"}, Permission: "Zero Trust Read"},
	{Name: "dlp-profiles", Title: "DLP · profiles", Scope: "account", path: "/accounts/{scope}/dlp/profiles", fields: "id name type description allowed_match_count confidence_threshold ocr_enabled created_at updated_at", Permission: "Zero Trust Read"},
	{Name: "gateway-rules", Title: "Gateway · rules", Scope: "account", path: "/accounts/{scope}/gateway/rules", fields: "id name description enabled action filters precedence created_at updated_at", Permission: "Zero Trust Read"},
	{Name: "audit", Title: "Audit Logs v2 · events", Scope: "account", path: "/accounts/{scope}/logs/audit", fields: "id action.time action.type action.result action.description actor.id actor.email actor.type actor.context resource.id resource.type resource.product resource.scope zone.id zone.name", paging: "audit", cursorPath: "result_info.cursor", from: "since", to: "before", TimeField: "action.time", Format: "logs", Parameters: []string{"action_type", "action_result", "actor_email", "resource_id", "resource_type", "resource_product", "zone_id"}, Permission: "Account Settings Read"},
	{Name: "audit-history", Title: "Audit Logs v2 · resource change history", Scope: "account", path: "/accounts/{scope}/logs/audit/{resource}/history", Resource: "audit", fields: "id action.time action.type action.result action.description actor.email resource.id resource.type resource.product zone.id zone.name", paging: "audit", cursorPath: "result_info.cursor", from: "since", to: "before", TimeField: "action.time", Format: "logs", Parameters: []string{"action_time"}, Permission: "Account Settings Read"},
}

func restCatalog() []restOperation {
	result := append([]restOperation{}, restOperations...)
	for i := range result {
		result[i].Columns = append(strings.Fields(result[i].fields), "snapshotTime")
		if result[i].Format == "" {
			result[i].Format = "table"
		}
		if result[i].IDField == "" {
			result[i].IDField = "id"
		}
		if result[i].NameField == "" {
			result[i].NameField = "name"
		}
	}
	return result
}
func findRESTOperation(name string) (restOperation, error) {
	for _, op := range restCatalog() {
		if op.Name == name {
			return op, nil
		}
	}
	return restOperation{}, fmt.Errorf("select a supported read-only operation")
}

var restResourceID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,255}$`)

func jsonPath(value any, path string) any {
	for _, key := range strings.Split(path, ".") {
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = object[key]
	}
	return value
}

func (d *Datasource) restQuery(ctx context.Context, q *Query, dq backend.DataQuery) (SQLResult, string, error) {
	op, err := findRESTOperation(q.Operation)
	if err != nil {
		return SQLResult{}, "", err
	}
	if q.Format == "traces" {
		return SQLResult{}, "", fmt.Errorf("read-only operations support Table, Logs, or Time series")
	}
	if op.Resource != "" && !restResourceID.MatchString(q.ResourceID) {
		return SQLResult{}, "", fmt.Errorf("select a resource or enter a valid resource ID")
	}
	allowed := map[string]bool{}
	for _, name := range op.Columns {
		allowed[name] = true
	}
	for _, name := range q.Columns {
		if !allowed[name] {
			return SQLResult{}, "", fmt.Errorf("field %q is not available in this operation", name)
		}
	}
	id := q.AccountID
	if op.Scope == "zone" {
		id = q.ZoneID
	}
	path := strings.ReplaceAll(op.path, "{scope}", id)
	path = strings.ReplaceAll(path, "{resource}", url.PathEscape(q.ResourceID))
	params := url.Values{}
	if op.Name == "origin-certificates" {
		params.Set("zone_id", q.ZoneID)
	}
	if op.Name == "tunnels" {
		params.Set("is_deleted", "false")
	}
	if op.Name == "dex-http" || op.Name == "dex-device" {
		params.Set("interval", "hour")
	}
	for _, filter := range q.Filters {
		if !contains(op.Parameters, filter.Field) {
			return SQLResult{}, "", fmt.Errorf("unsupported parameter %q", filter.Field)
		}
		if filter.Operator != "" && filter.Operator != "=" {
			return SQLResult{}, "", fmt.Errorf("REST parameters support equality only")
		}
		if params.Has(filter.Field) && !((op.Name == "dex-http" || op.Name == "dex-device") && filter.Field == "interval") {
			return SQLResult{}, "", fmt.Errorf("duplicate parameter %q", filter.Field)
		}
		params.Set(filter.Field, filter.Value)
	}
	if op.Name == "audit-history" {
		stamp, ok := timestamp(params.Get("action_time"))
		if !ok || stamp == nil {
			return SQLResult{}, "", fmt.Errorf("audit history requires the source entry's action_time in RFC3339 format")
		}
		params.Set("action_time", stamp.UTC().Format(time.RFC3339Nano))
	}
	if op.from != "" {
		if dq.TimeRange.From.IsZero() {
			return SQLResult{}, "", fmt.Errorf("a time range is required for this operation")
		}
		params.Set(op.from, dq.TimeRange.From.UTC().Format(time.RFC3339Nano))
		params.Set(op.to, dq.TimeRange.To.UTC().Format(time.RFC3339Nano))
	}
	limit := q.rowLimit(d.settings.MaxRows)
	q.Limit = limit
	result := SQLResult{Data: []map[string]any{}}
	now := time.Now().UTC()
	pageSize := min(50, limit)
	if op.Name == "origin-certificates" {
		pageSize = max(5, pageSize)
	}
	cursor := ""
	seen := map[string]bool{}
	size := 0
	scanned := 0
	for page := 1; page <= 2000; page++ {
		if op.paging != "" {
			key := "per_page"
			if op.paging == "audit" {
				key = "limit"
			}
			params.Set(key, strconv.Itoa(pageSize))
		}
		if op.paging == "page" {
			params.Set("page", strconv.Itoa(page))
		}
		if cursor != "" {
			key := op.paging
			if key == "audit" {
				key = "cursor"
			}
			params.Set(key, cursor)
		}
		executed := "GET " + path
		if len(params) > 0 {
			executed += "?" + params.Encode()
		}
		body, err := d.client.request(ctx, http.MethodGet, strings.TrimPrefix(executed, "GET "), nil)
		if err != nil {
			return SQLResult{}, executed, err
		}
		size += len(body)
		if size > maxResponseBytes {
			return SQLResult{}, executed, backend.DownstreamError(fmt.Errorf("resource results exceed 32 MiB; reduce the row limit"))
		}
		var envelope map[string]any
		if err := decodeJSON(body, &envelope); err != nil {
			return SQLResult{}, executed, backend.DownstreamError(err)
		}
		resultPath := op.resultPath
		if resultPath == "" {
			resultPath = "result"
		}
		raw := jsonPath(envelope, resultPath)
		if raw == nil {
			return SQLResult{}, executed, backend.DownstreamError(fmt.Errorf("Cloudflare returned no %s", resultPath))
		}
		rows, err := restRows(raw, op, q.ResourceID, now)
		if err != nil {
			return SQLResult{}, executed, backend.DownstreamError(err)
		}
		rawCount := len(rows)
		if values, ok := raw.([]any); ok {
			rawCount = len(values)
		}
		scanned += rawCount
		for _, row := range rows {
			// Pages does not provide server-side time filtering for deployment lists.
			if op.Name == "pages-deployments" {
				if t, ok := timestamp(row[op.TimeField]); ok && t != nil && (t.Before(dq.TimeRange.From) || t.After(dq.TimeRange.To)) {
					continue
				}
			}
			result.Data = append(result.Data, row)
		}
		status := textValue(jsonPath(envelope, "result_info.history_status"))
		if status != "" && status != "exact" {
			result.Notices = []string{"Cloudflare resource history identification: " + status}
		}
		if op.paging == "" {
			result.Incomplete = len(result.Data) > limit
			break
		}
		next := ""
		more := false
		if op.paging == "page" {
			total, _ := number(jsonPath(envelope, "result_info.total_pages"))
			count, _ := number(jsonPath(envelope, "result_info.total_count"))
			more = rawCount >= pageSize
			if count != nil {
				more = float64(page*pageSize) < *count
			}
			if total != nil {
				more = float64(page) < *total
			}
		} else {
			next = textValue(jsonPath(envelope, op.cursorPath))
			more = next != ""
		}
		if !more {
			break
		}
		if op.Name == "pages-deployments" && scanned >= limit {
			result.Incomplete = true
			result.Notices = append(result.Notices, "Pages deployment scan reached the row limit before all pages were inspected; increase the limit to search older deployments.")
			break
		}
		if len(result.Data) >= limit || page == 2000 {
			result.Incomplete = true
			break
		}
		if rawCount == 0 {
			return SQLResult{}, executed, backend.DownstreamError(fmt.Errorf("Cloudflare returned an empty page before pagination finished"))
		}
		if next != "" {
			if seen[next] {
				return SQLResult{}, executed, backend.DownstreamError(fmt.Errorf("Cloudflare repeated a pagination cursor"))
			}
			seen[next] = true
			cursor = next
		}
	}
	if len(result.Data) > limit {
		result.Data = result.Data[:limit]
	}
	q.TimeField = op.TimeField
	if q.Format == "timeseries" && q.TimeField == "" {
		q.TimeField = "snapshotTime"
	}
	for _, row := range result.Data {
		if q.Format == "timeseries" && q.TimeField != "snapshotTime" {
			delete(row, "snapshotTime")
		}
		for name := range row {
			if len(q.Columns) > 0 && !restFieldSelected(q.Columns, name) && name != "snapshotTime" && name != op.TimeField && name != op.IDField && name != op.NameField {
				delete(row, name)
			}
		}
	}
	for _, name := range op.Columns {
		if q.Format == "timeseries" && name == "snapshotTime" && q.TimeField != "snapshotTime" {
			continue
		}
		if len(q.Columns) > 0 && name != "snapshotTime" && name != q.TimeField && name != op.IDField && name != op.NameField && !restFieldSelected(q.Columns, name) {
			continue
		}
		column := resultColumn{Name: name}
		if name == "snapshotTime" || name == op.TimeField || strings.HasSuffix(name, "_on") || strings.HasSuffix(name, "_at") || name == "last_seen" {
			column.Type = "DateTime64(3)"
		}
		result.Meta = append(result.Meta, column)
	}
	return result, "GET " + path + "?" + params.Encode(), nil
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func restRows(raw any, op restOperation, resource string, now time.Time) ([]map[string]any, error) {
	if op.transform == "origins" {
		object, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid pool response")
		}
		raw = object["origins"]
	}
	if op.transform == "dex-http" || op.transform == "dex-device" {
		rows, err := dexRows(raw, op, now)
		for _, row := range rows {
			row["id"] = resource
		}
		return rows, err
	}
	if op.transform == "pool-health" {
		return poolHealthRows(raw, resource, now)
	}
	if op.transform == "certificates" {
		packs, ok := raw.([]any)
		if !ok {
			return nil, fmt.Errorf("invalid certificate pack list")
		}
		certs := []any{}
		for _, pack := range packs {
			object, ok := pack.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("invalid certificate pack")
			}
			values, _ := object["certificates"].([]any)
			if len(values) == 0 {
				certs = append(certs, map[string]any{"packId": object["id"], "packStatus": object["status"], "hosts": object["hosts"]})
			}
			for _, v := range values {
				cert, ok := v.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("invalid certificate")
				}
				cert["packId"] = object["id"]
				cert["packStatus"] = object["status"]
				certs = append(certs, cert)
			}
		}
		raw = certs
	}
	rows, err := selectRows(raw, "")
	if err != nil {
		return nil, err
	}
	for i, row := range rows {
		selected := map[string]any{"snapshotTime": now.Format(time.RFC3339Nano)}
		for _, name := range strings.Fields(op.fields) {
			for key, value := range row {
				if key == name || strings.HasPrefix(key, name+".") {
					selected[key] = value
				}
			}
		}
		if op.transform == "origins" {
			selected["poolId"] = resource
		}
		if op.transform == "certificates" || op.transform == "expiry" {
			if t, ok := timestamp(row["expires_on"]); ok && t != nil {
				selected["daysUntilExpiry"] = t.Sub(now).Hours() / 24
				selected["expired"] = t.Before(now)
			}
		}
		if op.Name == "tunnels" {
			switch row["status"] {
			case "healthy":
				selected["healthValue"] = float64(1)
			case "degraded", "down", "inactive":
				selected["healthValue"] = float64(0)
			}
		}
		rows[i] = selected
	}
	return rows, nil
}

func poolHealthRows(raw any, pool string, now time.Time) ([]map[string]any, error) {
	object, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid pool health response")
	}
	pops, ok := object["pop_health"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid pool region health")
	}
	regions := []string{}
	for name := range pops {
		regions = append(regions, name)
	}
	sort.Strings(regions)
	rows := []map[string]any{}
	for _, region := range regions {
		pop, ok := pops[region].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid pool region")
		}
		origins, _ := pop["origins"].([]any)
		if len(origins) == 0 {
			origins = []any{map[string]any{"": map[string]any{"healthy": pop["healthy"]}}}
		}
		for _, v := range origins {
			origins, ok := v.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("invalid origin health")
			}
			names := []string{}
			for name := range origins {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				health, ok := origins[name].(map[string]any)
				if !ok {
					return nil, fmt.Errorf("invalid origin status")
				}
				row := map[string]any{"poolId": pool, "region": region, "origin": name, "snapshotTime": now.Format(time.RFC3339Nano)}
				for _, field := range []string{"healthy", "failure_reason", "response_code", "rtt"} {
					if value, ok := health[field]; ok {
						row[field] = value
					}
				}
				if healthy, ok := health["healthy"].(bool); ok {
					row["healthValue"] = float64(0)
					if healthy {
						row["healthValue"] = float64(1)
					}
				}
				if rtt, err := time.ParseDuration(textValue(health["rtt"])); err == nil {
					row["rttMs"] = float64(rtt) / float64(time.Millisecond)
				}
				rows = append(rows, row)
			}
		}
	}
	return rows, nil
}

func dexRows(raw any, op restOperation, now time.Time) ([]map[string]any, error) {
	object, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid DEX metrics")
	}
	stats := object
	if op.transform == "dex-http" {
		if object["httpStats"] == nil {
			return []map[string]any{}, nil
		}
		stats, ok = object["httpStats"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid DEX HTTP statistics")
		}
	}
	byTime := map[string]map[string]any{}
	for _, metric := range strings.Fields(op.fields) {
		if metric == "time" || metric == "name" {
			continue
		}
		value := stats[metric]
		if op.transform == "dex-http" {
			value = jsonPath(value, "slots")
		}
		if value == nil {
			continue
		}
		slots, ok := value.([]any)
		if !ok {
			return nil, fmt.Errorf("invalid DEX %s slots", metric)
		}
		for _, v := range slots {
			slot, ok := v.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("invalid DEX observation")
			}
			stamp, ok := timestamp(slot["timestamp"])
			if !ok || stamp == nil {
				return nil, fmt.Errorf("invalid DEX timestamp")
			}
			key := stamp.Format(time.RFC3339Nano)
			if byTime[key] == nil {
				byTime[key] = map[string]any{"time": key, "snapshotTime": now.Format(time.RFC3339Nano)}
				if op.transform == "dex-http" {
					byTime[key]["name"] = object["name"]
				}
			}
			byTime[key][metric] = slot["value"]
		}
	}
	keys := []string{}
	for key := range byTime {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rows := []map[string]any{}
	for _, key := range keys {
		rows = append(rows, byTime[key])
	}
	return rows, nil
}

func restFieldSelected(fields []string, name string) bool {
	for _, field := range fields {
		if name == field || strings.HasPrefix(name, field+".") {
			return true
		}
	}
	return false
}
