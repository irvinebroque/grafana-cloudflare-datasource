package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

const accountID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const zoneID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func dataQuery(q Query) backend.DataQuery {
	body, _ := json.Marshal(q)
	return backend.DataQuery{RefID: "A", JSON: body, Interval: time.Minute, TimeRange: backend.TimeRange{From: time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC), To: time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)}}
}
func testDatasource(t *testing.T, handler http.HandlerFunc) *Datasource {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &Datasource{settings: Settings{AccountID: accountID, ZoneID: zoneID, MaxRows: 10000}, client: &client{http: server.Client(), baseURL: server.URL, token: "private-token"}, cache: map[string]cacheEntry{}}
}
func TestBuilderBindsValuesAndCapsRows(t *testing.T) {
	q := Query{Dataset: "events.workersInvocations", Format: "timeseries", Metrics: []Metric{{Aggregation: "count", Alias: "requests"}}, GroupBy: []string{"scriptName"}, Filters: []Filter{{Field: "scriptName", Operator: "=", Value: "x' OR 1=1 --"}}, Limit: 1000000}
	sql, params, err := buildSQL(q, dataQuery(q), 50)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sql, q.Filters[0].Value) || params["filter_0"] != q.Filters[0].Value || !strings.HasSuffix(sql, "LIMIT 50") || !strings.Contains(sql, "INTERVAL '60' SECOND") {
		t.Fatalf("unsafe or incorrect SQL: %s %#v", sql, params)
	}
	for _, mutation := range []func(*Query){func(q *Query) { q.Dataset = "events.httpRequests;DROP TABLE x" }, func(q *Query) { q.GroupBy = []string{"scriptName;DROP"} }, func(q *Query) { q.Metrics[0].Alias = "x` FROM x" }, func(q *Query) { q.Filters[0].Operator = "= 1; --" }} {
		copy := q
		copy.Metrics = append([]Metric{}, q.Metrics...)
		copy.Filters = append([]Filter{}, q.Filters...)
		mutation(&copy)
		if _, _, err := buildSQL(copy, dataQuery(copy), 50); err == nil {
			t.Fatalf("accepted invalid input: %#v", copy)
		}
	}
}
func TestBuilderSamplingAndQuotedDataset(t *testing.T) {
	q := Query{Dataset: "logs.workersLogs", Format: "timeseries", Metrics: []Metric{{Aggregation: "p99", Field: "cpuTimeMs", Alias: "cpu"}}}
	sql, _, err := buildSQL(q, dataQuery(q), 1000)
	if err != nil || !strings.Contains(sql, "quantileWeighted(0.99, `cpuTimeMs`, sampleInterval)") {
		t.Fatalf("%s %v", sql, err)
	}
	sampled := false
	q.Sampled = &sampled
	sql, _, _ = buildSQL(q, dataQuery(q), 1000)
	if !strings.Contains(sql, "`cpuTimeMs`, 1)") {
		t.Fatal(sql)
	}
	q.Dataset = "events.analyticsEngine.example-dataset"
	sql, _, err = buildSQL(q, dataQuery(q), 1000)
	if err != nil || !strings.Contains(sql, `events.analyticsEngine."example-dataset"`) {
		t.Fatalf("%s %v", sql, err)
	}
}
func TestSQLRequestHasBoundsAndParameters(t *testing.T) {
	d := testDatasource(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-token" || r.URL.Path != "/analytics/sql" {
			t.Error("wrong Cloudflare request")
		}
		var body struct {
			Query  string            `json:"query"`
			Params map[string]any    `json:"params"`
			Scope  map[string]string `json:"scope"`
			Range  map[string]string `json:"time_range"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Scope["zoneTag"] != zoneID || body.Range["start"] != "2026-11-10T09:00:00Z" || body.Params["status"] != float64(500) || !strings.Contains(body.Query, "INTERVAL '60' SECOND") {
			t.Errorf("wrong payload: %#v", body)
		}
		fmt.Fprint(w, `{"data":[{"time":"2026-11-10 09:00:00","requests":3}],"rows":1}`)
	})
	q := Query{QueryType: "sql", Format: "timeseries", Scope: "zone", SQL: "SELECT $__timeGroup(timestamp) AS time, COUNT(*) AS requests FROM events.httpRequests WHERE edgeResponseStatus=$status GROUP BY time", Params: `{"status":500}`}
	result := d.query(context.Background(), backend.PluginContext{}, dataQuery(q))
	if result.Error != nil || len(result.Frames) != 1 {
		t.Fatalf("%+v", result)
	}
}
func TestMetricsPreserveLabelsNullsAndOrder(t *testing.T) {
	result, err := decodeSQL([]byte(`{"data":[{"time":"2026-11-10 09:02:00","status":200,"version":"123","value":null},{"time":"2026-11-10 09:01:00","status":200,"version":"123","value":4},{"time":"2026-11-10 09:00:00","status":500,"version":"123","value":9}]}`))
	if err != nil {
		t.Fatal(err)
	}
	frames, err := resultFrames(result, Query{Format: "timeseries", GroupBy: []string{"status"}, Metrics: []Metric{{Alias: "value"}}}, "A", "", "", 100)
	if err != nil || len(frames) != 2 {
		t.Fatalf("%v %d", err, len(frames))
	}
	if frames[0].Fields[1].Labels["status"] != "200" || frames[0].Fields[1].Labels["version"] != "123" {
		t.Fatalf("labels lost: %#v", frames[0].Fields[1].Labels)
	}
	first := frames[0].Fields[0].At(0).(time.Time)
	last := frames[0].Fields[0].At(1).(time.Time)
	if !first.Before(last) || frames[0].Fields[1].At(1).(*float64) != nil {
		t.Fatal("time order or null value lost")
	}
	if _, err = data.FrameToJSONCache(frames[0]); err != nil {
		t.Fatal(err)
	}
}
func TestTraceAndLogFrames(t *testing.T) {
	result, _ := decodeSQL([]byte(`{"data":[{"timestamp":"2026-11-10 09:01:00.123","message":"hello","level":"error","traceId":"abc","spanId":"def","parentSpanId":"","serviceName":"worker","spanName":"fetch","startTime":"2026-11-10 09:01:00.123","durationMs":12.5,"spanStatus":"error","attributes":{"http.method":"GET"}}]}`))
	frames, err := resultFrames(result, Query{QueryType: "builder", Format: "traces", TraceID: "abc"}, "A", "uid", "SELECT", 100)
	if err != nil || frames[0].Meta.PreferredVisualization != data.VisTypeTrace {
		t.Fatalf("%v", err)
	}
	if frames[0].Fields[5].At(0).(float64) != float64(time.Date(2026, 11, 10, 9, 1, 0, 123000000, time.UTC).UnixMilli()) {
		t.Fatal("trace start time is not epoch milliseconds")
	}
	tags := frames[0].Fields[7].At(0).(json.RawMessage)
	if !strings.Contains(string(tags), `"error","value":true`) {
		t.Fatal(string(tags))
	}
	logs, err := resultFrames(result, Query{QueryType: "builder", Format: "logs", Limit: 1, AccountID: accountID}, "A", "uid", "", 100)
	if err != nil || logs[0].Fields[0].Name != "timestamp" || logs[0].Fields[1].Name != "message" || len(logs[0].Meta.Notices) != 1 {
		t.Fatalf("invalid log frame %v", err)
	}
	linked := false
	for _, f := range logs[0].Fields {
		if f.Name == "traceId" {
			linked = f.Config.Links[0].Internal.DatasourceUID == "uid"
		}
	}
	if !linked {
		t.Fatal("missing trace correlation link")
	}
	search, err := resultFrames(result, Query{QueryType: "builder", Format: "traces"}, "A", "uid", "", 100)
	if err != nil || search[0].Meta.PreferredVisualization != data.VisTypeTable {
		t.Fatal("trace search must be a table")
	}
}
func TestEmptyAndInvalidFrames(t *testing.T) {
	for _, format := range []string{"timeseries", "logs", "traces", "table"} {
		frames, err := resultFrames(SQLResult{Data: []map[string]any{}}, Query{Format: format, TraceID: "trace"}, "A", "", "", 100)
		if err != nil || len(frames) != 1 {
			t.Fatalf("%s: %v", format, err)
		}
	}
	result, _ := decodeSQL([]byte(`{"data":[{"time":"not a date","value":1}]}`))
	if _, err := resultFrames(result, Query{Format: "timeseries"}, "A", "", "", 100); err == nil {
		t.Fatal("accepted invalid time")
	}
	if _, err := decodeSQL([]byte(`{"success":true}`)); err == nil {
		t.Fatal("accepted missing data")
	}
}
func TestIndependentQueriesAndCancellation(t *testing.T) {
	d := testDatasource(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"data":[{"value":2}]}`) })
	valid := dataQuery(Query{QueryType: "sql", Format: "table", SQL: "SELECT COUNT(*) AS value FROM events.workersInvocations"})
	invalid := valid
	invalid.RefID = "B"
	invalid.JSON = []byte(`{"queryType":"no-such-mode"}`)
	response, err := d.QueryData(context.Background(), &backend.QueryDataRequest{Queries: []backend.DataQuery{valid, invalid}})
	if err != nil || response.Responses["A"].Error != nil || response.Responses["B"].Error == nil {
		t.Fatalf("%+v %v", response, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = d.client.request(ctx, http.MethodGet, "/never", nil)
	if err == nil {
		t.Fatal("request ignored cancellation")
	}
}
func TestClientRetriesAndRedactsErrors(t *testing.T) {
	var calls atomic.Int32
	d := testDatasource(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 2 {
			w.WriteHeader(503)
			fmt.Fprint(w, "retry")
			return
		}
		fmt.Fprint(w, `{"data":[]}`)
	})
	if _, err := d.client.request(context.Background(), http.MethodPost, "/analytics/sql", map[string]string{"query": "SELECT"}); err != nil || calls.Load() != 2 {
		t.Fatalf("%v %d", err, calls.Load())
	}
	calls.Store(0)
	d = testDatasource(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, "permanent failure")
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := d.client.request(ctx, http.MethodGet, "/x", nil); err == nil || calls.Load() != 3 || !strings.Contains(err.Error(), "permanent failure") {
		t.Fatalf("retry limit lost the upstream error: %v (%d calls)", err, calls.Load())
	}
	d = testDatasource(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		fmt.Fprint(w, "private-token permission denied")
	})
	_, err := d.client.request(context.Background(), http.MethodGet, "/x", nil)
	if err == nil || strings.Contains(err.Error(), "private-token") || !strings.Contains(err.Error(), "403") {
		t.Fatal(err)
	}
}
func TestResourceAllowlistAndCache(t *testing.T) {
	var calls atomic.Int32
	d := testDatasource(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); fmt.Fprint(w, `{"datasets":[]}`) })
	var status int
	sender := backend.CallResourceResponseSenderFunc(func(response *backend.CallResourceResponse) error { status = response.Status; return nil })
	for i := 0; i < 2; i++ {
		if err := d.CallResource(context.Background(), &backend.CallResourceRequest{Method: "GET", Path: "datasets", URL: "/datasets"}, sender); err != nil || status != 200 {
			t.Fatal(err, status)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("catalog not cached")
	}
	d.CallResource(context.Background(), &backend.CallResourceRequest{Method: "GET", Path: "datasets", URL: "/datasets?refresh=true"}, sender)
	if calls.Load() != 2 {
		t.Fatal("refresh did not bypass cache")
	}
	d.CallResource(context.Background(), &backend.CallResourceRequest{Method: "POST", Path: "datasets"}, sender)
	if status != 405 {
		t.Fatal(status)
	}
	d.CallResource(context.Background(), &backend.CallResourceRequest{Method: "GET", Path: "https://example.com", URL: "/x"}, sender)
	if status != 404 {
		t.Fatal("arbitrary proxy path allowed")
	}
}
func TestNestedGraphQLAndTelemetryPaths(t *testing.T) {
	var value any
	json.Unmarshal([]byte(`{"viewer":{"accounts":[{"groups":[{"dimensions":{"datetimeMinute":"2026-11-10T09:00:00Z","scriptName":"worker"},"sum":{"requests":5}}]}]}}`), &value)
	rows, err := selectRows(value, "viewer.accounts.groups")
	if err != nil || len(rows) != 1 || rows[0]["sum.requests"] != float64(5) {
		t.Fatalf("%#v %v", rows, err)
	}
	if _, err := selectRows(value, "viewer.accounts.missing"); err == nil {
		t.Fatal("silently accepted invalid response path")
	}
}
func TestLegacyMacrosUseCompatibleDateTime(t *testing.T) {
	q := dataQuery(Query{})
	sql := expandMacros("timestamp >= toDateTime($__fromTime) AND timestamp <= toDateTime($__toTime)", q, Query{QueryType: "analytics", AccountID: accountID, ZoneID: zoneID})
	if !strings.Contains(sql, "toDateTime('2026-11-10 09:00:00')") || strings.Contains(sql, "Z") {
		t.Fatal(sql)
	}
}

func TestFullRangeLogVolumeMetadata(t *testing.T) {
	d := testDatasource(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"time":"2026-11-10 09:00:00","level":"error","logs":5}]}`)
	})
	source := json.RawMessage(`{"refId":"A","datasource":{"uid":"uid"},"queryType":"builder","format":"logs","dataset":"logs.workersLogs"}`)
	q := Query{QueryType: "builder", Format: "timeseries", Dataset: "logs.workersLogs", Metrics: []Metric{{Aggregation: "count", Alias: "logs"}}, GroupBy: []string{"level"}, LogVolume: true, VolumeSource: source}
	result := d.query(context.Background(), backend.PluginContext{}, dataQuery(q))
	if result.Error != nil {
		t.Fatal(result.Error)
	}
	meta := result.Frames[0].Meta.Custom.(map[string]any)
	if meta["logsVolumeType"] != "FullRange" || result.Frames[0].Name != "error" || string(meta["sourceQuery"].(json.RawMessage)) != string(source) {
		t.Fatalf("%#v", meta)
	}
}
