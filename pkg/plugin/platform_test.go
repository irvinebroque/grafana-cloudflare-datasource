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

func TestPlatformTraceKeepsExternalParentsAndNativeLinks(t *testing.T) {
	result, err := decodeSQL([]byte(`{"data":[
 {"traceId":"trace","spanId":"edge","parentSpanId":"external","serviceName":"cache","spanName":"cache.lookup","startTime":"2026-11-10T09:00:00Z","durationMs":4,"spanStatus":"ok"},
 {"traceId":"trace","spanId":"worker","parentSpanId":"edge","serviceName":"worker","spanName":"fetch","startTime":"2026-11-10T09:00:00Z","durationMs":3,"error":true,"spanStatusMessage":"origin failed"},
 {"traceId":"trace","spanId":"worker","parentSpanId":"edge","serviceName":"worker","spanName":"fetch","startTime":"2026-11-10T09:00:00Z","durationMs":3},
 {"traceId":"other","spanId":"worker","parentSpanId":"edge","serviceName":"origin","spanName":"GET","startTime":"2026-11-10T09:00:00Z","durationMs":2}
 ]}`))
	if err != nil {
		t.Fatal(err)
	}
	frames, err := resultFrames(result, Query{QueryType: "builder", Format: "traces", TraceID: "trace", AccountID: accountID}, "A", "uid", "SELECT", 100)
	if err != nil || len(frames) != 3 {
		t.Fatalf("%v %d", err, len(frames))
	}
	if frames[0].Fields[2].At(0) != "external" || frames[0].Fields[9].At(1) != int64(2) || frames[0].Fields[10].At(1) != "origin failed" {
		t.Fatal("parent or OTel status lost")
	}
	link := frames[0].Fields[0].Config.Links[0]
	if link.Internal.DatasourceUID != "uid" || link.Internal.Query.(map[string]any)["accountId"] != accountID {
		t.Fatal("native span link lost account scope")
	}
	if frames[1].Name != "nodes" || frames[1].Rows() != 3 || frames[2].Rows() != 1 || frames[2].Fields[1].At(0) != "trace:edge" {
		t.Fatal("graph contains duplicates, cross-trace or invented parents")
	}
	for _, frame := range frames {
		if _, err := data.FrameToJSONCache(frame); err != nil {
			t.Fatal(err)
		}
	}
	q := Query{QueryType: "builder", Dataset: "logs.traces", Format: "traces"}
	sql, _, err := buildSQL(q, dataQuery(q), 100)
	if err != nil || strings.Contains(sql, "parentSpanId =") || !strings.Contains(sql, "GROUP BY traceId") {
		t.Fatalf("root-only search returned: %s %v", sql, err)
	}
}

func graphFixture() []graphType {
	scalar := func(name string) graphType { return graphType{Name: name, Kind: "SCALAR"} }
	field := func(name, kind, typ string) graphField {
		return graphField{Name: name, Type: graphRef{Kind: kind, Name: typ}}
	}
	node := field("productAdaptiveGroups", "OBJECT", "Product")
	node.Args = []graphField{field("limit", "SCALAR", "UInt64"), field("filter", "INPUT_OBJECT", "Filter")}
	deprecated := node
	deprecated.Name = "oldProduct"
	deprecated.Deprecated = true
	staging := node
	staging.Name = "stagingProduct"
	return []graphType{scalar("Time"), scalar("UInt64"), scalar("String"),
		{Name: "account", Kind: "OBJECT", Fields: []graphField{node, deprecated, staging}},
		{Name: "Product", Kind: "OBJECT", Fields: []graphField{field("count", "SCALAR", "UInt64"), field("dimensions", "OBJECT", "Dimensions")}},
		{Name: "Dimensions", Kind: "OBJECT", Fields: []graphField{field("datetimeMinute", "SCALAR", "Time"), field("service", "SCALAR", "String")}},
		{Name: "Filter", Kind: "INPUT_OBJECT", Inputs: []graphField{field("datetime_geq", "SCALAR", "Time"), field("datetime_leq", "SCALAR", "Time"), field("service", "SCALAR", "String"), field("id", "SCALAR", "UInt64")}},
	}
}

func TestGraphBuilderUsesDiscoveryBindsValuesAndProviderLimits(t *testing.T) {
	calls := 0
	d := testDatasource(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		if err := decoder.Decode(&body); err != nil {
			t.Error(err)
		}
		if strings.Contains(body.Query, "settings{") {
			fmt.Fprint(w, `{"data":{"viewer":{"accounts":[{"settings":{"productAdaptiveGroups":{"enabled":true,"availableFields":["count","dimensions"],"maxDuration":7200,"maxPageSize":25,"notOlderThan":86400}}}]}}}`)
			return
		}
		if !strings.Contains(body.Query, "limit:25") || !strings.Contains(body.Query, "dimensions { datetimeMinute service }") || strings.Contains(body.Query, "injected") {
			t.Errorf("bad generated query: %s", body.Query)
		}
		if body.Variables["filter2"] != "injected\"}){secret}" || body.Variables["filter3"] != json.Number("18446744073709551615") {
			t.Errorf("filter value altered: %#v", body.Variables)
		}
		fmt.Fprint(w, `{"data":{"viewer":{"accounts":[{"productAdaptiveGroups":[{"count":7,"dimensions":{"datetimeMinute":"2026-11-10T09:00:00Z","service":"example"}}]}]}}}`)
	})
	body, _ := json.Marshal(map[string]any{"__schema": map[string]any{"types": graphFixture()}})
	d.cacheBody("graphql-schema", body)
	q := Query{QueryType: "graphql-builder", Dataset: "productAdaptiveGroups", Format: "timeseries", AccountID: accountID, Columns: []string{"count"}, GroupBy: []string{"dimensions.service"}, Limit: 1000, Filters: []Filter{{Field: "service", Value: "injected\"}){secret}"}, {Field: "id", Value: "18446744073709551615"}}}
	result := d.query(context.Background(), backend.PluginContext{}, dataQuery(q))
	if result.Error != nil || len(result.Frames) != 1 || result.Frames[0].Fields[1].Labels["dimensions.service"] != "example" {
		t.Fatalf("query: %+v", result)
	}
	if calls != 2 {
		t.Fatalf("unexpected schema/settings requests: %d", calls)
	}
	q.Columns = []string{"nonexistent"}
	if result := d.query(context.Background(), backend.PluginContext{}, dataQuery(q)); result.Error == nil {
		t.Fatal("unknown field accepted")
	}
	q.Columns = nil
	q.TimeField = "count"
	if result := d.query(context.Background(), backend.PluginContext{}, dataQuery(q)); result.Error == nil {
		t.Fatal("numeric field treated as a timestamp")
	}
	q.TimeField = ""
	dq := dataQuery(q)
	dq.TimeRange.To = dq.TimeRange.From.Add(3 * time.Hour)
	if result := d.query(context.Background(), backend.PluginContext{}, dq); result.Error == nil {
		t.Fatal("provider duration limit ignored")
	}
	if calls != 2 {
		t.Fatal("invalid queries reached provider")
	}
	types := map[string]graphType{}
	for _, typ := range graphFixture() {
		types[typ.Name] = typ
	}
	if datasets := graphDatasets(types, "account"); len(datasets) != 1 {
		t.Fatalf("deprecated/staging nodes visible: %#v", datasets)
	}
}

func TestGraphFilterValidationAndBucketSelection(t *testing.T) {
	for _, item := range []struct{ value, typ string }{{"1.5", "UInt64"}, {"-1", "UInt64"}, {"null", "[String!]"}, {"{}", "[String!]"}, {"NaN", "Float64"}, {"null", "Float64"}, {"true false", "Boolean"}} {
		if _, err := graphFilterValue(item.value, item.typ); err == nil {
			t.Errorf("accepted %s as %s", item.value, item.typ)
		}
	}
	dataset := graphDataset{Fields: []graphColumn{{Name: "dimensions.datetimeMinute", Type: "Time"}, {Name: "dimensions.datetimeHour", Type: "Time"}}}
	if graphTimeField(dataset, 5*time.Minute) != "dimensions.datetimeHour" {
		t.Fatal("bucket ignored panel interval")
	}
	dataset.Fields = append(dataset.Fields, graphColumn{Name: "datetime", Type: "Time!"})
	if graphTimeField(dataset, 0) != "datetime" {
		t.Fatal("event timestamp was replaced by an aggregate bucket")
	}
}

func TestOtherProductLogsUseTheirCatalogAndNativeMessage(t *testing.T) {
	calls := 0
	d := testDatasource(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method == http.MethodGet {
			if r.URL.Query().Get("dataset_name") != "logs.product" {
				t.Error("wrong dataset discovery")
			}
			fmt.Fprint(w, `{"datasets":[{"name":"logs.product","columns":[{"name":"eventTime","data_type":"DateTime64(3)"},{"name":"body","data_type":"String"},{"name":"traceId","data_type":"String"}]}]}`)
			return
		}
		var payload struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if !strings.Contains(payload.Query, "ORDER BY `eventTime`") || strings.Contains(payload.Query, "`level`") {
			t.Error(payload.Query)
		}
		fmt.Fprint(w, `{"meta":[{"name":"eventTime","type":"DateTime64(3)"},{"name":"body","type":"String"},{"name":"traceId","type":"String"}],"data":[{"eventTime":"2026-11-10T09:00:00Z","body":"event body","traceId":"correlation"}]}`)
	})
	q := Query{QueryType: "builder", Dataset: "logs.product", Format: "logs", AccountID: accountID}
	result := d.query(context.Background(), backend.PluginContext{}, dataQuery(q))
	if result.Error != nil || result.Frames[0].Meta.PreferredVisualization != data.VisTypeLogs {
		t.Fatalf("%+v", result)
	}
	frame := result.Frames[0]
	if frame.Fields[0].Name != "eventTime" || frame.Fields[1].Name != "message" || frame.Fields[1].At(0).(*string) == nil || *frame.Fields[1].At(0).(*string) != "event body" {
		t.Fatal("native log timestamp or body lost")
	}
	q.LogVolume = true
	q.Format = "timeseries"
	q.Metrics = []Metric{{Aggregation: "count", Alias: "logs"}}
	if err := d.prepareLogs(context.Background(), &q); err != nil || q.TimeField != "eventTime" || calls != 2 {
		t.Fatal("schema cache or generic volume timestamp failed", err, calls)
	}
	row := map[string]any{"action": "allow", "resource": "example"}
	mapped := logRows([]map[string]any{row})
	if mapped[0]["message"] != "allow" || row["message"] != nil || mapped[0]["resource"] != "example" {
		t.Fatal("message mapping mutated or discarded structured fields")
	}
}

func TestGraphRawEventsDefaultToNativeLogsInsteadOfNumericIDs(t *testing.T) {
	d := testDatasource(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if strings.Contains(body.Query, "settings{") {
			fmt.Fprint(w, `{"data":{"viewer":{"accounts":[{"settings":{"rawEvents":{"enabled":true,"availableFields":["datetime","clientAsn","action"],"maxDuration":3600,"maxPageSize":10}}}]}}}`)
			return
		}
		if !strings.Contains(body.Query, "{datetime clientAsn action}") {
			t.Error(body.Query)
		}
		fmt.Fprint(w, `{"data":{"viewer":{"accounts":[{"rawEvents":[{"datetime":"2026-11-10T09:00:00Z","clientAsn":12345,"action":"allow"}]}]}}}`)
	})
	types := graphFixture()
	node := types[3].Fields[0]
	node.Name = "rawEvents"
	node.Type.Name = "RawEvent"
	types[3].Fields = []graphField{node}
	types = append(types, graphType{Name: "RawEvent", Kind: "OBJECT", Fields: []graphField{
		{Name: "datetime", Type: graphRef{Kind: "SCALAR", Name: "Time"}},
		{Name: "clientAsn", Type: graphRef{Kind: "SCALAR", Name: "UInt64"}},
		{Name: "action", Type: graphRef{Kind: "SCALAR", Name: "String"}},
	}})
	body, _ := json.Marshal(map[string]any{"__schema": map[string]any{"types": types}})
	d.cacheBody("graphql-schema", body)
	q := Query{QueryType: "graphql-builder", Dataset: "rawEvents", Format: "logs"}
	result := d.query(context.Background(), backend.PluginContext{}, dataQuery(q))
	if result.Error != nil || len(result.Frames) != 1 {
		t.Fatalf("%+v", result)
	}
	frame := result.Frames[0]
	if frame.Meta.PreferredVisualization != data.VisTypeLogs || frame.Fields[0].Name != "datetime" || frame.Fields[1].Name != "message" || *frame.Fields[1].At(0).(*string) != "allow" {
		t.Fatal("raw event did not use the native log contract")
	}
	q.Format = "timeseries"
	if result := d.query(context.Background(), backend.PluginContext{}, dataQuery(q)); result.Error == nil {
		t.Fatal("raw event ASN was charted as a default metric")
	}
}
