package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/httpclient"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
)

type Datasource struct {
	settings Settings
	client   *client
	mu       sync.Mutex
	cache    map[string]cacheEntry
}

var _ backend.QueryDataHandler = (*Datasource)(nil)
var _ backend.CheckHealthHandler = (*Datasource)(nil)
var _ backend.CallResourceHandler = (*Datasource)(nil)
var _ instancemgmt.InstanceDisposer = (*Datasource)(nil)

func New(ctx context.Context, settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	var config Settings
	if err := json.Unmarshal(settings.JSONData, &config); err != nil {
		return nil, fmt.Errorf("invalid datasource settings: %w", err)
	}
	if config.TimeoutSeconds <= 0 {
		config.TimeoutSeconds = 60
	}
	if config.TimeoutSeconds > 300 {
		config.TimeoutSeconds = 300
	}
	if config.MaxRows <= 0 {
		config.MaxRows = 10000
	}
	if config.MaxRows > 100000 {
		config.MaxRows = 100000
	}
	token := settings.DecryptedSecureJSONData["apiToken"]
	if token == "" {
		return nil, fmt.Errorf("configure a Cloudflare API token")
	}
	opts, err := settings.HTTPClientOptions(ctx)
	if err != nil {
		return nil, err
	}
	timeouts := httpclient.DefaultTimeoutOptions
	timeouts.Timeout = time.Duration(config.TimeoutSeconds) * time.Second
	opts.Timeouts = &timeouts
	httpClient, err := httpclient.New(opts)
	if err != nil {
		return nil, err
	}
	httpClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &Datasource{settings: config, client: &client{http: httpClient, baseURL: "https://api.cloudflare.com/client/v4", token: token}, cache: make(map[string]cacheEntry)}, nil
}

func (d *Datasource) Dispose() { d.client.http.CloseIdleConnections() }

func (d *Datasource) scope(q Query) (string, string, error) {
	if q.Scope != "" && q.Scope != "account" && q.Scope != "zone" {
		return "", "", fmt.Errorf("scope must be account or zone")
	}
	account := q.AccountID
	if account == "" {
		account = d.settings.AccountID
	}
	zone := q.ZoneID
	if zone == "" {
		zone = d.settings.ZoneID
	}
	if account != "" && !tagPattern.MatchString(account) {
		return "", "", fmt.Errorf("account ID must contain 32 lowercase hexadecimal characters")
	}
	if zone != "" && !tagPattern.MatchString(zone) {
		return "", "", fmt.Errorf("zone ID must contain 32 lowercase hexadecimal characters")
	}
	if q.Scope == "zone" {
		if zone == "" {
			return "", "", fmt.Errorf("select a zone or configure a default zone ID")
		}
	} else if account == "" {
		return "", "", fmt.Errorf("select an account or configure a default account ID")
	}
	return account, zone, nil
}

func (d *Datasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	response := backend.NewQueryDataResponse()
	var mu sync.Mutex
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 4)
	for _, query := range req.Queries {
		wg.Add(1)
		go func(dq backend.DataQuery) {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				mu.Lock()
				response.Responses[dq.RefID] = backend.ErrDataResponse(backend.StatusTimeout, ctx.Err().Error())
				mu.Unlock()
				return
			}
			res := d.query(ctx, req.PluginContext, dq)
			mu.Lock()
			response.Responses[dq.RefID] = res
			mu.Unlock()
		}(query)
	}
	wg.Wait()
	return response, nil
}

func (d *Datasource) query(ctx context.Context, pc backend.PluginContext, dq backend.DataQuery) backend.DataResponse {
	var q Query
	if err := json.Unmarshal(dq.JSON, &q); err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, "invalid query JSON")
	}
	if q.Hide {
		return backend.DataResponse{}
	}
	if dq.TimeRange.From.IsZero() || dq.TimeRange.To.Before(dq.TimeRange.From) {
		return backend.ErrDataResponse(backend.StatusBadRequest, "a valid time range is required")
	}
	if q.Format != "" && q.Format != "table" && q.Format != "logs" && q.Format != "traces" && q.Format != "timeseries" {
		return backend.ErrDataResponse(backend.StatusBadRequest, "unsupported result format")
	}
	account, zone, err := d.scope(q)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, err.Error())
	}
	if q.QueryType != "builder" && q.QueryType != "" {
		q.Dataset = ""
	}
	q.AccountID = account
	q.ZoneID = zone
	var result SQLResult
	var executed string
	switch q.QueryType {
	case "", "builder", "sql":
		result, executed, err = d.sqlQuery(ctx, q, dq, account, zone)
	case "graphql":
		result, executed, err = d.graphqlQuery(ctx, q, dq, account, zone)
	case "analytics":
		result, executed, err = d.analyticsQuery(ctx, q, dq, account, zone)
	case "telemetry":
		result, executed, err = d.telemetryQuery(ctx, q, dq, account)
	default:
		err = fmt.Errorf("unsupported query type %q", q.QueryType)
	}
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, err.Error())
	}
	uid := ""
	if pc.DataSourceInstanceSettings != nil {
		uid = pc.DataSourceInstanceSettings.UID
	}
	frames, err := resultFrames(result, q, dq.RefID, uid, executed, d.settings.MaxRows)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, err.Error())
	}
	if q.LogVolume {
		name := "Cloudflare"
		if pc.DataSourceInstanceSettings != nil {
			name = pc.DataSourceInstanceSettings.Name
		}
		for _, frame := range frames {
			kind := "FullRange"
			if len(frame.Meta.Notices) > 0 {
				kind = "Limited"
			}
			custom := frame.Meta.Custom.(map[string]any)
			custom["logsVolumeType"] = kind
			custom["absoluteRange"] = map[string]int64{"from": dq.TimeRange.From.UnixMilli(), "to": dq.TimeRange.To.UnixMilli()}
			custom["datasourceName"] = name
			custom["sourceQuery"] = q.VolumeSource
			if len(frame.Fields) > 1 {
				frame.Name = frame.Fields[1].Labels["level"]
			}
		}
	}
	return backend.DataResponse{Frames: frames}
}

func decodeSQL(body []byte) (SQLResult, error) {
	var result SQLResult
	if err := decodeJSON(body, &result); err != nil {
		return result, fmt.Errorf("expected Cloudflare JSON query output (use FORMAT JSON): %w", err)
	}
	if result.Data == nil {
		return result, fmt.Errorf("Cloudflare returned no data array")
	}
	for _, row := range result.Data {
		if row == nil {
			return result, fmt.Errorf("Cloudflare returned a null row instead of an object")
		}
	}
	return result, nil
}

func (d *Datasource) sqlQuery(ctx context.Context, q Query, dq backend.DataQuery, account, zone string) (SQLResult, string, error) {
	var sql string
	var params map[string]any
	var err error
	if q.QueryType == "sql" {
		sql = expandMacros(q.SQL, dq, account, zone)
		params, err = decodeParams(q.Params)
	} else {
		sql, params, err = buildSQL(q, dq, d.settings.MaxRows)
		sql += " FORMAT JSON"
	}
	if err != nil {
		return SQLResult{}, sql, err
	}
	if strings.TrimSpace(sql) == "" {
		return SQLResult{}, sql, fmt.Errorf("enter a SELECT query")
	}
	scope := map[string]string{"accountTag": account}
	if q.Scope == "zone" {
		scope = map[string]string{"zoneTag": zone}
	}
	body := map[string]any{"query": sql, "params": params, "scope": scope, "time_range": map[string]string{"start": dq.TimeRange.From.UTC().Format(time.RFC3339Nano), "end": dq.TimeRange.To.UTC().Format(time.RFC3339Nano)}}
	response, err := d.client.request(ctx, http.MethodPost, "/analytics/sql", body)
	if err != nil {
		return SQLResult{}, sql, err
	}
	result, err := decodeSQL(response)
	return result, sql, err
}

func (d *Datasource) CheckHealth(ctx context.Context, _ *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	if !tagPattern.MatchString(d.settings.AccountID) {
		return &backend.CheckHealthResult{Status: backend.HealthStatusError, Message: "Configure a default account ID to test the SQL API"}, nil
	}
	body, err := d.client.request(ctx, http.MethodGet, "/analytics/sql/introspection?account_tag="+d.settings.AccountID, nil)
	if err == nil {
		var catalog struct {
			Datasets []json.RawMessage `json:"datasets"`
		}
		err = decodeJSON(body, &catalog)
		if err == nil && catalog.Datasets == nil {
			err = fmt.Errorf("Cloudflare returned no SQL dataset catalog")
		}
	}
	if err != nil {
		return &backend.CheckHealthResult{Status: backend.HealthStatusError, Message: err.Error()}, nil
	}
	return &backend.CheckHealthResult{Status: backend.HealthStatusOk, Message: "Connected to Cloudflare SQL API. Dataset access depends on your token permissions and plan."}, nil
}

func (d *Datasource) CallResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	if req.Method != http.MethodGet {
		return sender.Send(&backend.CallResourceResponse{Status: 405, Body: []byte(`{"message":"only GET is supported"}`)})
	}
	parsed, err := url.Parse(req.URL)
	if err != nil {
		return err
	}
	params := parsed.Query()
	account := params.Get("accountId")
	if account == "" {
		account = d.settings.AccountID
	}
	path := ""
	route := strings.Trim(req.Path, "/")
	switch route {
	case "datasets":
		if !tagPattern.MatchString(account) {
			return sendResourceError(sender, 400, "select a valid account ID")
		}
		query := url.Values{"account_tag": {account}, "include_columns": {"true"}}
		if dataset := params.Get("dataset"); dataset != "" {
			query.Set("dataset_name", dataset)
			query.Set("include_attributes", "true")
		}
		path = "/analytics/sql/introspection?" + query.Encode()
	case "accounts":
		path = "/accounts?per_page=50"
	case "zones":
		if !tagPattern.MatchString(account) {
			return sendResourceError(sender, 400, "select a valid account ID")
		}
		path = "/zones?per_page=50&account.id=" + account
	default:
		return sendResourceError(sender, 404, "unknown datasource resource")
	}
	d.mu.Lock()
	entry, exists := d.cache[path]
	d.mu.Unlock()
	if exists && entry.Expires > time.Now().Unix() && params.Get("refresh") != "true" {
		return sender.Send(&backend.CallResourceResponse{Status: 200, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: entry.Body})
	}
	var body []byte
	if route == "accounts" || route == "zones" {
		body, err = d.client.listResource(ctx, path)
	} else {
		body, err = d.client.request(ctx, http.MethodGet, path, nil)
	}
	if err != nil {
		return sendResourceError(sender, 502, err.Error())
	}
	d.mu.Lock()
	for key, cached := range d.cache {
		if cached.Expires <= time.Now().Unix() {
			delete(d.cache, key)
		}
	}
	if len(d.cache) >= 256 {
		for key := range d.cache {
			delete(d.cache, key)
			break
		}
	}
	d.cache[path] = cacheEntry{Body: body, Expires: time.Now().Add(5 * time.Minute).Unix()}
	d.mu.Unlock()
	return sender.Send(&backend.CallResourceResponse{Status: 200, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: body})
}

func sendResourceError(sender backend.CallResourceResponseSender, status int, message string) error {
	body, _ := json.Marshal(map[string]string{"message": message})
	return sender.Send(&backend.CallResourceResponse{Status: status, Body: body})
}

func (d *Datasource) graphqlQuery(ctx context.Context, q Query, dq backend.DataQuery, account, zone string) (SQLResult, string, error) {
	if err := validateGraphQL(q.GraphQL, q.OperationName); err != nil {
		return SQLResult{}, q.GraphQL, err
	}
	variables, err := decodeParams(q.Variables)
	if err != nil {
		return SQLResult{}, q.GraphQL, err
	}
	variables["accountTag"] = account
	variables["zoneTag"] = zone
	variables["from"] = dq.TimeRange.From.UTC().Format(time.RFC3339)
	variables["to"] = dq.TimeRange.To.UTC().Format(time.RFC3339)
	payload := map[string]any{"query": q.GraphQL, "variables": variables}
	if q.OperationName != "" {
		payload["operationName"] = q.OperationName
	}
	body, err := d.client.request(ctx, http.MethodPost, "/graphql", payload)
	if err != nil {
		return SQLResult{}, q.GraphQL, err
	}
	var envelope struct {
		Data   any `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err = decodeJSON(body, &envelope); err != nil {
		return SQLResult{}, q.GraphQL, err
	}
	if len(envelope.Errors) > 0 {
		return SQLResult{}, q.GraphQL, fmt.Errorf("Cloudflare GraphQL: %s", envelope.Errors[0].Message)
	}
	if envelope.Data == nil {
		return SQLResult{}, q.GraphQL, fmt.Errorf("Cloudflare GraphQL returned no data object")
	}
	rows, err := selectRows(envelope.Data, q.DataPath)
	return SQLResult{Data: rows}, q.GraphQL, err
}

func (d *Datasource) analyticsQuery(ctx context.Context, q Query, dq backend.DataQuery, account, zone string) (SQLResult, string, error) {
	if account == "" {
		return SQLResult{}, "", fmt.Errorf("Analytics Engine requires an account ID")
	}
	source := q.AnalyticsSQL
	if source == "" {
		source = q.SQL
	}
	sql := expandSQLMacros(source, dq, account, zone, true)
	if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(sql)), "SELECT ") && !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(sql)), "SELECT\n") {
		return SQLResult{}, sql, fmt.Errorf("Analytics Engine requires a SELECT query")
	}
	body, err := d.client.request(ctx, http.MethodPost, "/accounts/"+account+"/analytics_engine/sql", sql)
	if err != nil {
		return SQLResult{}, sql, err
	}
	result, err := decodeSQL(body)
	return result, sql, err
}

func (d *Datasource) telemetryQuery(ctx context.Context, q Query, dq backend.DataQuery, account string) (SQLResult, string, error) {
	if account == "" {
		return SQLResult{}, "", fmt.Errorf("Workers telemetry requires an account ID")
	}
	body, err := decodeParams(q.Telemetry)
	if err != nil {
		return SQLResult{}, "", err
	}
	body["timeframe"] = map[string]int64{"from": dq.TimeRange.From.UnixMilli(), "to": dq.TimeRange.To.UnixMilli()}
	if _, ok := body["queryId"]; !ok {
		body["queryId"] = "grafana-" + dq.RefID
	}
	response, err := d.client.request(ctx, http.MethodPost, "/accounts/"+account+"/workers/observability/telemetry/query", body)
	if err != nil {
		return SQLResult{}, q.Telemetry, err
	}
	var envelope struct {
		Result any `json:"result"`
	}
	if err = decodeJSON(response, &envelope); err != nil {
		return SQLResult{}, q.Telemetry, err
	}
	if envelope.Result == nil {
		return SQLResult{}, q.Telemetry, fmt.Errorf("Cloudflare telemetry returned no result object")
	}
	rows, err := selectRows(envelope.Result, q.DataPath)
	for _, row := range rows {
		for _, pair := range [][2]string{{"source.message", "message"}, {"source.level", "level"}, {"$metadata.traceId", "traceId"}, {"$workers.scriptName", "scriptName"}} {
			if v, ok := row[pair[0]]; ok {
				row[pair[1]] = v
				delete(row, pair[0])
			}
		}
	}
	return SQLResult{Data: rows}, q.Telemetry, err
}

func selectRows(value any, path string) ([]map[string]any, error) {
	segments := []string{}
	if path != "" {
		segments = strings.Split(path, ".")
	}
	var walk func(any, []string) ([]map[string]any, error)
	walk = func(current any, remaining []string) ([]map[string]any, error) {
		if array, ok := current.([]any); ok {
			rows := []map[string]any{}
			for _, child := range array {
				found, err := walk(child, remaining)
				if err != nil {
					return nil, err
				}
				rows = append(rows, found...)
			}
			return rows, nil
		}
		if len(remaining) > 0 {
			object, ok := current.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("data path %q does not select objects", path)
			}
			child, ok := object[remaining[0]]
			if !ok {
				return nil, fmt.Errorf("data path %q: field %q was not returned", path, remaining[0])
			}
			return walk(child, remaining[1:])
		}
		object, ok := current.(map[string]any)
		if !ok {
			if current == nil {
				return []map[string]any{}, nil
			}
			return nil, fmt.Errorf("data path %q must select an object or array", path)
		}
		row := map[string]any{}
		flattenRow("", object, row)
		return []map[string]any{row}, nil
	}
	return walk(value, segments)
}

func flattenRow(prefix string, object map[string]any, row map[string]any) {
	for key, value := range object {
		name := key
		if prefix != "" {
			name = prefix + "." + key
		}
		if child, ok := value.(map[string]any); ok && key != "attributes" {
			flattenRow(name, child, row)
		} else {
			row[name] = value
		}
	}
}
