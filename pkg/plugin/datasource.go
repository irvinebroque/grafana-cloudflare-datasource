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
	"github.com/grafana/grafana-plugin-sdk-go/experimental/concurrent"
)

type Datasource struct {
	settings Settings
	client   *client
	mu       sync.Mutex
	schemaMu sync.Mutex
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
	return concurrent.QueryData(ctx, req, func(ctx context.Context, query concurrent.Query) backend.DataResponse {
		return d.query(ctx, query.PluginContext, query.DataQuery)
	}, 4)
}

func (d *Datasource) query(ctx context.Context, pc backend.PluginContext, dq backend.DataQuery) backend.DataResponse {
	if err := ctx.Err(); err != nil {
		return queryError(err)
	}
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
	if q.QueryType == "rest" {
		op, err := findRESTOperation(q.Operation)
		if err != nil {
			return queryError(err)
		}
		q.Scope = op.Scope
	}
	account, zone, err := d.scope(q)
	if err != nil {
		return queryError(err)
	}
	if q.QueryType != "builder" && q.QueryType != "" && q.QueryType != "graphql-builder" {
		q.Dataset = ""
	}
	q.AccountID = account
	q.ZoneID = zone
	var result SQLResult
	var executed string
	switch q.QueryType {
	case "", "builder", "sql":
		if err = d.prepareLogs(ctx, &q); err == nil {
			result, executed, err = d.sqlQuery(ctx, q, dq)
		}
	case "graphql-builder":
		result, executed, err = d.graphBuilderQuery(ctx, &q, dq)
	case "graphql":
		result, executed, err = d.graphqlQuery(ctx, q, dq)
	case "analytics":
		result, executed, err = d.analyticsQuery(ctx, q, dq)
	case "telemetry":
		result, executed, err = d.telemetryQuery(ctx, q, dq)
	case "rest":
		result, executed, err = d.restQuery(ctx, &q, dq)
	default:
		err = fmt.Errorf("unsupported query type %q", q.QueryType)
	}
	if err != nil {
		return queryError(err)
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

func (d *Datasource) sqlQuery(ctx context.Context, q Query, dq backend.DataQuery) (SQLResult, string, error) {
	var sql string
	var params map[string]any
	var err error
	if q.QueryType == "sql" {
		sql = expandMacros(q.SQL, dq, q)
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
	scope := map[string]string{"accountTag": q.AccountID}
	if q.Scope == "zone" {
		scope = map[string]string{"zoneTag": q.ZoneID}
	}
	body := map[string]any{"query": sql, "params": params, "scope": scope, "time_range": map[string]string{"start": dq.TimeRange.From.UTC().Format(time.RFC3339Nano), "end": dq.TimeRange.To.UTC().Format(time.RFC3339Nano)}}
	response, err := d.client.request(ctx, http.MethodPost, "/analytics/sql", body)
	if err != nil {
		return SQLResult{}, sql, err
	}
	result, err := decodeSQL(response)
	if err != nil {
		err = backend.DownstreamError(err)
	}
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
	if route == "operations" {
		body, _ := json.Marshal(map[string]any{"operations": restCatalog()})
		return sender.Send(&backend.CallResourceResponse{Status: 200, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: body})
	}
	if route == "resources" {
		q := Query{QueryType: "rest", Operation: params.Get("operation"), AccountID: account, ZoneID: params.Get("zoneId"), Limit: 1000, Format: "table"}
		op, cause := findRESTOperation(q.Operation)
		if cause != nil || op.Resource != "" {
			return sendResourceError(sender, 400, "select an inventory operation without a parent resource")
		}
		q.Scope = op.Scope
		q.AccountID, q.ZoneID, cause = d.scope(q)
		if cause != nil {
			return sendResourceError(sender, 400, cause.Error())
		}
		result, _, cause := d.restQuery(ctx, &q, backend.DataQuery{})
		if cause != nil {
			return sendResourceError(sender, int(queryError(cause).Status), cause.Error())
		}
		resources := []map[string]string{}
		for _, row := range result.Data {
			id := textValue(row[op.IDField])
			name := textValue(row[op.NameField])
			if id == "" {
				continue
			}
			if name == "" {
				name = id
			}
			resources = append(resources, map[string]string{"id": id, "name": name})
		}
		body, _ := json.Marshal(map[string]any{"result": resources, "limited": result.Incomplete})
		return sender.Send(&backend.CallResourceResponse{Status: 200, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: body})
	}
	if route == "graphql-datasets" {
		q := Query{Scope: params.Get("scope"), AccountID: account, ZoneID: params.Get("zoneId"), Dataset: params.Get("dataset")}
		q.AccountID, q.ZoneID, err = d.scope(q)
		if err != nil {
			return sendResourceError(sender, 400, err.Error())
		}
		if params.Get("refresh") == "true" {
			d.mu.Lock()
			for key := range d.cache {
				if strings.HasPrefix(key, "graphql-") {
					delete(d.cache, key)
				}
			}
			d.mu.Unlock()
		}
		catalog, cause := d.graphCatalog(ctx, q)
		if cause != nil {
			return sendResourceError(sender, int(queryError(cause).Status), cause.Error())
		}
		body, _ := json.Marshal(map[string]any{"datasets": catalog})
		return sender.Send(&backend.CallResourceResponse{Status: 200, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: body})
	}
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
		return sendResourceError(sender, int(queryError(backend.DownstreamError(err)).Status), err.Error())
	}
	d.cacheBody(path, body)
	return sender.Send(&backend.CallResourceResponse{Status: 200, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: body})
}

func (d *Datasource) cacheBody(key string, body []byte) {
	d.mu.Lock()
	for cachedKey, cached := range d.cache {
		if cached.Expires <= time.Now().Unix() {
			delete(d.cache, cachedKey)
		}
	}
	if len(d.cache) >= 256 {
		for cachedKey := range d.cache {
			delete(d.cache, cachedKey)
			break
		}
	}
	d.cache[key] = cacheEntry{Body: body, Expires: time.Now().Add(5 * time.Minute).Unix()}
	d.mu.Unlock()
}

func sendResourceError(sender backend.CallResourceResponseSender, status int, message string) error {
	body, _ := json.Marshal(map[string]string{"message": message})
	return sender.Send(&backend.CallResourceResponse{Status: status, Body: body})
}

func (d *Datasource) graphqlQuery(ctx context.Context, q Query, dq backend.DataQuery) (SQLResult, string, error) {
	if err := validateGraphQL(q.GraphQL, q.OperationName); err != nil {
		return SQLResult{}, q.GraphQL, err
	}
	variables, err := decodeParams(q.Variables)
	if err != nil {
		return SQLResult{}, q.GraphQL, err
	}
	variables["accountTag"] = q.AccountID
	variables["zoneTag"] = q.ZoneID
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
		return SQLResult{}, q.GraphQL, backend.DownstreamError(err)
	}
	if len(envelope.Errors) > 0 {
		return SQLResult{}, q.GraphQL, backend.DownstreamError(&cloudflareAPIError{status: backend.StatusBadRequest, message: "Cloudflare GraphQL: " + envelope.Errors[0].Message})
	}
	if envelope.Data == nil {
		return SQLResult{}, q.GraphQL, backend.DownstreamError(fmt.Errorf("Cloudflare GraphQL returned no data object"))
	}
	rows, err := selectRows(envelope.Data, q.DataPath)
	return SQLResult{Data: rows}, q.GraphQL, err
}

func (d *Datasource) analyticsQuery(ctx context.Context, q Query, dq backend.DataQuery) (SQLResult, string, error) {
	if q.AccountID == "" {
		return SQLResult{}, "", fmt.Errorf("Analytics Engine requires an account ID")
	}
	source := q.AnalyticsSQL
	if source == "" {
		source = q.SQL
	}
	sql := expandMacros(source, dq, q)
	if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(sql)), "SELECT ") && !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(sql)), "SELECT\n") {
		return SQLResult{}, sql, fmt.Errorf("Analytics Engine requires a SELECT query")
	}
	body, err := d.client.request(ctx, http.MethodPost, "/accounts/"+q.AccountID+"/analytics_engine/sql", sql)
	if err != nil {
		return SQLResult{}, sql, err
	}
	result, err := decodeSQL(body)
	if err != nil {
		err = backend.DownstreamError(err)
	}
	return result, sql, err
}

func (d *Datasource) telemetryQuery(ctx context.Context, q Query, dq backend.DataQuery) (SQLResult, string, error) {
	if q.AccountID == "" {
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
	response, err := d.client.request(ctx, http.MethodPost, "/accounts/"+q.AccountID+"/workers/observability/telemetry/query", body)
	if err != nil {
		return SQLResult{}, q.Telemetry, err
	}
	var envelope struct {
		Result any `json:"result"`
	}
	if err = decodeJSON(response, &envelope); err != nil {
		return SQLResult{}, q.Telemetry, backend.DownstreamError(err)
	}
	if envelope.Result == nil {
		return SQLResult{}, q.Telemetry, backend.DownstreamError(fmt.Errorf("Cloudflare telemetry returned no result object"))
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
