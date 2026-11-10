package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

func TestQueryBatchesLimitConcurrencyAndSkipCanceledRequests(t *testing.T) {
	var active, calls atomic.Int32
	d := testDatasource(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if current := active.Add(1); current > 4 {
			t.Errorf("batch exceeded four concurrent requests: %d", current)
		}
		defer active.Add(-1)
		time.Sleep(5 * time.Millisecond)
		fmt.Fprint(w, `{"data":[{"value":1}]}`)
	})
	request := &backend.QueryDataRequest{}
	for i := 0; i < 12; i++ {
		query := dataQuery(Query{QueryType: "sql", Format: "table", SQL: "SELECT 1 AS value"})
		query.RefID = fmt.Sprintf("Q%d", i)
		request.Queries = append(request.Queries, query)
	}
	response, err := d.QueryData(context.Background(), request)
	if err != nil || len(response.Responses) != 12 || calls.Load() != 12 {
		t.Fatalf("batch lost responses: %+v, %v, %d requests", response, err, calls.Load())
	}
	for ref, result := range response.Responses {
		if result.Error != nil {
			t.Errorf("%s failed: %v", ref, result.Error)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response, err = d.QueryData(ctx, request)
	if err != nil || len(response.Responses) != 12 || calls.Load() != 12 {
		t.Fatalf("canceled batch dispatched requests: %+v, %v, %d requests", response, err, calls.Load())
	}
	for ref, result := range response.Responses {
		if result.Error == nil || result.Status != backend.StatusTimeout {
			t.Errorf("%s did not return a cancellation: %+v", ref, result)
		}
	}
}

func TestRejectNonObjectAndTrailingParameters(t *testing.T) {
	for _, source := range []string{"null", "[]", "1", `{} {"extra":true}`, `{} garbage`} {
		if _, err := decodeParams(source); err == nil {
			t.Errorf("accepted parameters %q", source)
		}
	}
	for _, source := range []string{"", "{}", `{"number":9007199254740993}`} {
		params, err := decodeParams(source)
		if err != nil || params == nil {
			t.Errorf("invalid object %q: %v", source, err)
		}
	}
}

func TestInvalidJSONCannotPanicQueryModes(t *testing.T) {
	d := testDatasource(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid JSON reached Cloudflare") })
	for _, q := range []Query{
		{QueryType: "graphql", GraphQL: "{ viewer { accounts { id } } }", Variables: "null"},
		{QueryType: "telemetry", Telemetry: "null"},
	} {
		t.Run(q.QueryType, func(t *testing.T) {
			response := d.query(context.Background(), backend.PluginContext{}, dataQuery(q))
			if response.Error == nil {
				t.Error("expected parameter validation error")
			}
		})
	}
}

func TestSubsecondIntervalUsesOneSecondBucket(t *testing.T) {
	dq := dataQuery(Query{})
	dq.Interval = 500 * time.Millisecond
	if intervalSeconds(dq) != 1 {
		t.Errorf("500ms interval became %d seconds", intervalSeconds(dq))
	}
	dq.Interval = 0
	if intervalSeconds(dq) != 60 {
		t.Error("missing interval lost its default")
	}
}

func TestNullableColumnTypesAndConfiguredTime(t *testing.T) {
	result, err := decodeSQL([]byte(`{"meta":[{"name":"when","type":"Nullable(DateTime64(3))"},{"name":"value","type":"Nullable(UInt64)"}],"data":[{"when":"2026-11-10 09:01:00.123","value":"42"},{"when":null,"value":null}]}`))
	if err != nil {
		t.Fatal(err)
	}
	frame := tableFrame(result.Data, Query{Format: "table"}, "A", result)
	if frame.Fields[0].Type() != data.FieldTypeNullableTime || frame.Fields[1].Type() != data.FieldTypeNullableFloat64 {
		t.Errorf("nullable schema lost: %v, %v", frame.Fields[0].Type(), frame.Fields[1].Type())
	}
	custom, _ := decodeSQL([]byte(`{"data":[{"time":"wrong column","actual":"2026-11-10 09:00:00","value":3}]}`))
	if _, err := seriesFrames(custom.Data, Query{QueryType: "sql", TimeField: "actual"}, "A", custom); err != nil {
		t.Errorf("configured time ignored: %v", err)
	}
}

func TestLogBuilderDefaultsAndConfiguredOrdering(t *testing.T) {
	q := Query{Dataset: "logs.containerLogs", Format: "logs"}
	sql, _, err := buildSQL(q, dataQuery(q), 100)
	if err != nil || strings.Contains(sql, "scriptName") || !strings.Contains(sql, "containerId") {
		t.Errorf("invalid Container defaults: %s %v", sql, err)
	}
	q = Query{Dataset: "logs.traces", Format: "logs", Columns: []string{"startTime", "spanName"}, TimeField: "startTime"}
	sql, _, err = buildSQL(q, dataQuery(q), 100)
	if err != nil || !strings.Contains(sql, "ORDER BY `startTime` DESC") {
		t.Errorf("configured log time ignored: %s %v", sql, err)
	}
}

func TestBuilderRejectsCollidingColumns(t *testing.T) {
	for _, q := range []Query{
		{Dataset: "events.workersInvocations", Format: "timeseries", Metrics: []Metric{{Aggregation: "count", Alias: "time"}}},
		{Dataset: "events.workersInvocations", Format: "timeseries", GroupBy: []string{"scriptName"}, Metrics: []Metric{{Aggregation: "count", Alias: "scriptName"}}},
		{Dataset: "events.workersInvocations", Format: "table", Metrics: []Metric{{Aggregation: "count", Alias: "same"}, {Aggregation: "count", Alias: "same"}}},
	} {
		if _, _, err := buildSQL(q, dataQuery(q), 100); err == nil {
			t.Errorf("accepted colliding columns: %+v", q)
		}
	}
}

func TestMalformedSQLResponseIsAnError(t *testing.T) {
	for _, body := range []string{`null`, `{"data":null}`, `{"data":[null]}`, `{"data":[]} {"data":[]}`} {
		if _, err := decodeSQL([]byte(body)); err == nil {
			t.Errorf("accepted malformed result %s", body)
		}
	}
}

func TestResourceSelectorsIncludeAllPages(t *testing.T) {
	for _, route := range []string{"accounts", "zones"} {
		t.Run(route, func(t *testing.T) {
			pages := []string{}
			d := testDatasource(t, func(w http.ResponseWriter, r *http.Request) {
				page := r.URL.Query().Get("page")
				pages = append(pages, page)
				if route == "accounts" {
					fmt.Fprintf(w, `{"success":true,"result":[{"id":"%s","name":"Account"}],"result_info":{"page":%s,"per_page":1,"total_count":2}}`, page, page)
				} else if page == "2" {
					fmt.Fprint(w, `{"success":true,"result":[{"id":"second","name":"Second"}],"result_info":{"page":2,"total_pages":2}}`)
				} else {
					fmt.Fprint(w, `{"success":true,"result":[{"id":"first","name":"First"}],"result_info":{"page":1,"total_pages":2}}`)
				}
			})
			var body []byte
			sender := backend.CallResourceResponseSenderFunc(func(response *backend.CallResourceResponse) error { body = response.Body; return nil })
			if err := d.CallResource(context.Background(), &backend.CallResourceRequest{Method: "GET", Path: route, URL: "/" + route}, sender); err != nil {
				t.Fatal(err)
			}
			var response struct {
				Result []map[string]string `json:"result"`
			}
			if err := json.Unmarshal(body, &response); err != nil {
				t.Fatal(err)
			}
			if len(response.Result) != 2 || len(pages) != 2 {
				t.Errorf("selector omitted pages: %s (pages %v)", body, pages)
			}
		})
	}
}

func TestNonFiniteAndInvalidNumbersNeverBecomeZero(t *testing.T) {
	for _, value := range []any{"not-a-number", "NaN", "+Inf", json.Number("1e500")} {
		n, ok := number(value)
		if ok || n != nil {
			t.Errorf("invalid number accepted: %v -> %v", value, n)
		}
	}
	if _, ok := timestamp("1e100"); ok {
		t.Error("overflowing timestamp accepted")
	}
	result, _ := decodeSQL([]byte(`{"data":[{"traceId":"a","spanId":"b","startTime":"2026-11-10 09:00:00","durationMs":-1}]}`))
	if _, err := traceFrames(result.Data, "A"); err == nil {
		t.Error("negative trace duration accepted")
	}
}

func TestResourcePaginationFailsWithoutPartialSuccess(t *testing.T) {
	d := testDatasource(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			w.WriteHeader(403)
			fmt.Fprint(w, `{"success":false}`)
			return
		}
		fmt.Fprint(w, `{"success":true,"result":[{"id":"first","name":"First"}],"result_info":{"page":1,"total_pages":2}}`)
	})
	var status int
	sender := backend.CallResourceResponseSenderFunc(func(response *backend.CallResourceResponse) error { status = response.Status; return nil })
	d.CallResource(context.Background(), &backend.CallResourceRequest{Method: "GET", Path: "accounts", URL: "/accounts"}, sender)
	if status != 502 || len(d.cache) != 0 {
		t.Errorf("partial list became successful or cached: %d", status)
	}
}

func TestRetryAfterNeverOverflowsOrIgnoresHTTPDates(t *testing.T) {
	for _, header := range []string{"9223372036854775807", time.Now().Add(time.Minute).UTC().Format(http.TimeFormat)} {
		calls := 0
		d := testDatasource(t, func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.Header().Set("Retry-After", header)
			w.WriteHeader(429)
		})
		if _, err := d.client.request(context.Background(), http.MethodGet, "/x", nil); err == nil || calls != 1 {
			t.Errorf("ignored Retry-After %q: %d calls %v", header, calls, err)
		}
	}
}

func TestHealthRejectsMalformedCatalog(t *testing.T) {
	for _, body := range []string{`<html>upstream unavailable</html>`, `{"success":true}`, `{"datasets":null}`} {
		d := testDatasource(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
		result, err := d.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
		if err != nil || result.Status != backend.HealthStatusError {
			t.Errorf("invalid catalog became healthy: %s %+v %v", body, result, err)
		}
	}
}

func TestLegacySQLUsesItsOwnTextAndGraphQLAllowsComments(t *testing.T) {
	d := testDatasource(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/graphql" {
			fmt.Fprint(w, `{"data":{"rows":[{"value":1}]}}`)
			return
		}
		buffer, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if string(buffer) != "SELECT legacy" {
			t.Errorf("wrong SQL dialect: %s", buffer)
		}
		fmt.Fprint(w, `{"data":[]}`)
	})
	legacy := Query{QueryType: "analytics", Format: "table", SQL: "SELECT unified", AnalyticsSQL: "SELECT legacy"}
	if response := d.query(context.Background(), backend.PluginContext{}, dataQuery(legacy)); response.Error != nil {
		t.Fatal(response.Error)
	}
	graphql := Query{QueryType: "graphql", Format: "table", GraphQL: "# Saved query\n query { rows { value } }", DataPath: "rows"}
	if response := d.query(context.Background(), backend.PluginContext{}, dataQuery(graphql)); response.Error != nil {
		t.Fatal(response.Error)
	}
}

func TestLogVolumeMarksCappedResultsLimited(t *testing.T) {
	d := testDatasource(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"time":"2026-11-10 09:00:00","level":"info","logs":1}]}`)
	})
	d.settings.MaxRows = 1
	q := Query{QueryType: "builder", Dataset: "logs.workersLogs", Format: "timeseries", LogVolume: true, GroupBy: []string{"level"}, Metrics: []Metric{{Aggregation: "count", Alias: "logs"}}, Limit: 10000}
	response := d.query(context.Background(), backend.PluginContext{}, dataQuery(q))
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	custom := response.Frames[0].Meta.Custom.(map[string]any)
	if custom["logsVolumeType"] != "Limited" || custom["accountId"] != accountID {
		t.Errorf("misleading volume metadata: %#v", custom)
	}
}

func TestRawTimeSeriesOrdersByItsSelectedTimeColumn(t *testing.T) {
	q := Query{Dataset: "events.workersInvocations", Format: "timeseries", Columns: []string{"timestamp", "cpuTimeUs"}}
	sql, _, err := buildSQL(q, dataQuery(q), 100)
	if err != nil || !strings.Contains(sql, "ORDER BY `timestamp` ASC") {
		t.Errorf("invalid raw-series SQL: %s %v", sql, err)
	}
}

func FuzzResponseFrames(f *testing.F) {
	for _, seed := range []string{`null`, `{"data":[]}`, `{"data":[null]}`, `{"data":[{"time":"2026-11-10 09:00:00","value":1}]}`, `{"meta":[{"name":"value","type":"Nullable(Float64)"}],"data":[{"timestamp":"2026-11-10 09:00:00","value":"NaN"}]}`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, body string) {
		if len(body) > 65536 {
			t.Skip()
		}
		result, err := decodeSQL([]byte(body))
		if err != nil {
			return
		}
		for _, format := range []string{"table", "timeseries", "logs", "traces"} {
			frames, err := resultFrames(result, Query{Format: format}, "A", "uid", "query", 100)
			if err != nil {
				continue
			}
			for _, frame := range frames {
				if _, err := data.FrameToJSONCache(frame); err != nil {
					t.Fatalf("frame cannot be encoded: %v", err)
				}
			}
		}
	})
}

func TestGraphQLDocumentsAreParsedBeforeExecution(t *testing.T) {
	for _, source := range []string{
		"# Saved query\nquery { viewer { id } }",
		"\ufeffquery { viewer { id } }",
		"fragment Fields on Viewer { id } query { viewer { ...Fields } }",
	} {
		if err := validateGraphQL(source, ""); err != nil {
			t.Errorf("valid read rejected: %v", err)
		}
	}
	for _, source := range []string{
		"mutation { update { id } }",
		"subscription { updates { id } }",
		"query { viewer { id } } mutation { update { id } }",
		"query {",
		"fragment Fields on Viewer { id }",
	} {
		if err := validateGraphQL(source, ""); err == nil {
			t.Errorf("invalid/non-read document accepted: %s", source)
		}
	}
	document := "query First { viewer { id } } query Second { viewer { id } }"
	if validateGraphQL(document, "") == nil || validateGraphQL(document, "Missing") == nil || validateGraphQL(document, "Second") != nil {
		t.Error("operation-name selection is invalid")
	}
	d := testDatasource(t, func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Operation string `json:"operationName"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.Operation != "Second" {
			t.Error("operation name was not sent to Cloudflare")
		}
		fmt.Fprint(w, `{"data":{"value":1}}`)
	})
	q := Query{QueryType: "graphql", Format: "table", GraphQL: document, OperationName: "Second"}
	if response := d.query(context.Background(), backend.PluginContext{}, dataQuery(q)); response.Error != nil {
		t.Fatal(response.Error)
	}
}

func TestLegacyTimeGroupDoesNotUseUnsupportedIdentifierQuotes(t *testing.T) {
	q := dataQuery(Query{})
	sql := expandMacros("SELECT $__timeGroup(timestamp) AS time", q, Query{QueryType: "analytics", AccountID: accountID, ZoneID: zoneID})
	if strings.Contains(sql, "`") || !strings.Contains(sql, "toStartOfInterval(timestamp,") {
		t.Errorf("legacy macro uses unsupported quoting: %s", sql)
	}
}

func TestSparseResponsesCannotExpandIntoUnboundedFrames(t *testing.T) {
	rows := make([]map[string]any, 3000)
	for i := range rows {
		rows[i] = map[string]any{fmt.Sprintf("field_%d", i%1000): json.Number("1")}
	}
	if _, err := resultFrames(SQLResult{Data: rows}, Query{Format: "table"}, "A", "", "", 10000); err == nil || !strings.Contains(err.Error(), "cell limit") {
		t.Fatalf("sparse table was not bounded: %v", err)
	}
	row := map[string]any{}
	for i := 0; i < 1001; i++ {
		row[fmt.Sprintf("field_%d", i)] = json.Number("1")
	}
	if _, err := resultFrames(SQLResult{Data: []map[string]any{row}}, Query{Format: "table"}, "A", "", "", 10000); err == nil || !strings.Contains(err.Error(), "too many columns") {
		t.Fatalf("wide table was not bounded: %v", err)
	}
	rows = make([]map[string]any, 60)
	for i := range rows {
		rows[i] = map[string]any{"time": "2026-11-10 09:00:00", "dimension": fmt.Sprintf("group_%d", i)}
		for metric := 0; metric < 200; metric++ {
			rows[i][fmt.Sprintf("metric_%d", metric)] = json.Number("1")
		}
	}
	if _, err := resultFrames(SQLResult{Data: rows}, Query{Format: "timeseries"}, "A", "", "", 10000); err == nil || !strings.Contains(err.Error(), "time series limit") {
		t.Fatalf("series expansion was not bounded: %v", err)
	}
}

func TestDuplicateRawSQLColumnsAreRejected(t *testing.T) {
	result, err := decodeSQL([]byte(`{"meta":[{"name":"value","type":"UInt64"},{"name":"value","type":"UInt64"}],"data":[{"value":1}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resultFrames(result, Query{Format: "table"}, "A", "", "", 100); err == nil {
		t.Error("ambiguous raw column names were accepted")
	}
}

func TestRepeatedMetricLabelsHaveAnOutputBudget(t *testing.T) {
	row := map[string]any{"time": "2026-11-10 09:00:00"}
	for i := 0; i < 100; i++ {
		row[fmt.Sprintf("metric_%d", i)] = json.Number("1")
	}
	for i := 0; i < 200; i++ {
		row[fmt.Sprintf("dimension_%d", i)] = strings.Repeat("x", 2000)
	}
	if _, err := resultFrames(SQLResult{Data: []map[string]any{row}}, Query{Format: "timeseries"}, "A", "", "", 100); err == nil || !strings.Contains(err.Error(), "labels exceed") {
		t.Fatalf("repeated labels have no output limit: %v", err)
	}
}
