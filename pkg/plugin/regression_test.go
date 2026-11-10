package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

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
