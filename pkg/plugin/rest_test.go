package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

func TestRESTPaginationProjectionAndScope(t *testing.T) {
	calls := 0
	d := testDatasource(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || !strings.HasPrefix(r.URL.Path, "/zones/"+strings.Repeat("b", 32)+"/healthchecks") {
			t.Error("incorrect method or scope", r.URL.Path)
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if r.URL.Query().Get("per_page") != "50" {
			t.Error("page size changed")
		}
		rows := []map[string]any{}
		for i := 0; i < 50; i++ {
			rows = append(rows, map[string]any{"id": fmt.Sprint((page-1)*50 + i), "name": "check", "status": "healthy", "secret": "must not leave backend"})
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "result": rows, "result_info": map[string]any{"page": page, "per_page": 50, "total_count": 120, "total_pages": 3}})
	})
	q := Query{QueryType: "rest", Operation: "health-checks", ZoneID: strings.Repeat("b", 32), Format: "table", Limit: 75}
	result := d.query(context.Background(), backend.PluginContext{}, dataQuery(q))
	if result.Error != nil || calls != 2 {
		t.Fatalf("%+v calls=%d", result, calls)
	}
	frame := result.Frames[0]
	if frame.Rows() != 75 || len(frame.Meta.Notices) == 0 {
		t.Fatal("row cap not reported")
	}
	for _, field := range frame.Fields {
		if field.Name == "secret" {
			t.Fatal("unapproved property returned")
		}
		if field.Name == "id" && *field.At(74).(*string) != "74" {
			t.Fatal("pagination duplicated or skipped items")
		}
	}
}

func TestRESTRejectsUnapprovedRequestsBeforeNetwork(t *testing.T) {
	d := testDatasource(t, func(http.ResponseWriter, *http.Request) { t.Error("invalid query reached network") })
	for _, q := range []Query{
		{Operation: "https://example.com"}, {Operation: "pool-health", ResourceID: "../secrets"},
		{Operation: "tunnels", Filters: []Filter{{Field: "token", Value: "secret"}}},
		{Operation: "tunnels", Columns: []string{"credentials"}},
		{Operation: "audit-history", ResourceID: "audit-id"},
		{Operation: "tunnels", Format: "traces"},
	} {
		q.QueryType = "rest"
		r := d.query(context.Background(), backend.PluginContext{}, dataQuery(q))
		if r.Error == nil || r.Status != backend.StatusBadRequest {
			t.Fatalf("accepted invalid query %+v", q)
		}
	}
}

func TestRESTCursorPaginationAndAuditHistory(t *testing.T) {
	calls := 0
	d := testDatasource(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("since") == "" || r.URL.Query().Get("before") == "" || r.URL.Query().Get("action_time") == "" {
			t.Error("history lost time bounds")
		}
		cursor := "next"
		if r.URL.Query().Get("cursor") != "" {
			cursor = ""
		}
		fmt.Fprintf(w, `{"result":[{"id":"event","action":{"time":"2026-11-10T09:00:00Z","description":"updated setting","type":"update"},"resource":{"request":{"password":"secret"},"id":"resource"}}],"result_info":{"cursor":%q,"history_status":"approximate"}}`, cursor)
	})
	q := Query{QueryType: "rest", Operation: "audit-history", ResourceID: "source", Format: "logs", Filters: []Filter{{Field: "action_time", Value: "2026-11-10T09:00:00Z"}}}
	r := d.query(context.Background(), backend.PluginContext{}, dataQuery(q))
	if r.Error != nil {
		t.Fatal(r.Error)
	}
	f := r.Frames[0]
	if calls != 2 || f.Rows() != 2 || f.Fields[0].Name != "action.time" || f.Fields[0].At(0).(*time.Time) == nil || f.Meta.PreferredVisualization != data.VisTypeLogs || len(f.Meta.Notices) == 0 {
		t.Fatal("native history or pagination failed")
	}
	for _, field := range f.Fields {
		if strings.Contains(field.Name, "request") {
			t.Fatal("audit payload exposed")
		}
	}
}

func TestRESTCertificateHealthAndDEXShapes(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	op, _ := findRESTOperation("certificates")
	rows, err := restRows([]any{map[string]any{"id": "pack", "status": "active", "certificates": []any{map[string]any{"id": "cert", "expires_on": "2026-11-12T12:00:00Z", "private_key": "secret"}}}}, op, "", now)
	if err != nil || len(rows) != 1 || rows[0]["daysUntilExpiry"] != float64(2) || rows[0]["private_key"] != nil || rows[0]["packStatus"] != "active" {
		t.Fatal(rows, err)
	}
	rows, err = poolHealthRows(map[string]any{"pop_health": map[string]any{"region": map[string]any{"healthy": true, "origins": []any{map[string]any{"192.0.2.1": map[string]any{"healthy": false, "failure_reason": "timeout", "rtt": "12.1ms"}}}}}}, "pool", now)
	if err != nil || len(rows) != 1 || rows[0]["healthValue"] != float64(0) || rows[0]["rttMs"] != 12.1 {
		t.Fatal(rows, err)
	}
	op, _ = findRESTOperation("dex-http")
	rows, err = restRows(map[string]any{"name": "test", "httpStats": map[string]any{"availabilityPct": map[string]any{"slots": []any{map[string]any{"timestamp": "2026-11-10 09:00:00+00", "value": 99.5}}}, "dnsResponseTimeMs": map[string]any{"slots": []any{map[string]any{"timestamp": "2026-11-10 09:00:00+00", "value": 12}}}}}, op, "", now)
	if err != nil || len(rows) != 1 || rows[0]["availabilityPct"] != 99.5 || rows[0]["dnsResponseTimeMs"] != 12 {
		t.Fatal(rows, err)
	}
}

func TestLogTimestampPrecision(t *testing.T) {
	expected := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	for _, n := range []int64{expected.Unix(), expected.UnixMilli(), expected.UnixMicro(), expected.UnixNano()} {
		got, ok := timestamp(json.Number(strconv.FormatInt(n, 10)))
		if !ok || got == nil || !got.Equal(expected) {
			t.Fatalf("timestamp %d: %v", n, got)
		}
	}
}

func TestRESTPagesHistoryBoundsScannedRows(t *testing.T) {
	calls := 0
	d := testDatasource(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `{"result":[{"id":"old","created_on":"2020-01-01T00:00:00Z"}],"result_info":{"total_pages":100}}`)
	})
	q := Query{QueryType: "rest", Operation: "pages-deployments", ResourceID: "project", Format: "logs", Limit: 1}
	r := d.query(context.Background(), backend.PluginContext{}, dataQuery(q))
	if r.Error != nil || calls != 1 || r.Frames[0].Rows() != 0 || len(r.Frames[0].Meta.Notices) == 0 {
		t.Fatalf("unbounded or silent history scan: %+v calls=%d", r, calls)
	}
}

func TestRESTSnapshotNumericSeries(t *testing.T) {
	d := testDatasource(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("per_page") != "5" {
			t.Error("origin certificates require at least five records per page")
		}
		fmt.Fprint(w, `{"result":[{"id":"cert","expires_on":"2099-01-01T00:00:00Z"}]}`)
	})
	q := Query{QueryType: "rest", Operation: "origin-certificates", ZoneID: strings.Repeat("b", 32), Format: "timeseries", Columns: []string{"daysUntilExpiry"}, Limit: 1}
	r := d.query(context.Background(), backend.PluginContext{}, dataQuery(q))
	if r.Error != nil || len(r.Frames) != 1 {
		t.Fatalf("snapshot series failed: %+v", r)
	}
	f := r.Frames[0]
	if f.Rows() != 1 || f.Fields[0].Type() != data.FieldTypeTime || f.Fields[1].Type() != data.FieldTypeNullableFloat64 || f.Fields[1].Labels["id"] != "cert" || f.Fields[1].Config.Unit != "d" {
		t.Fatal("snapshot series lost time, numeric value, unit, or resource identity", f)
	}
}

func TestRESTHistoricalSeriesUsesObservationTimeAndStableLabels(t *testing.T) {
	d := testDatasource(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("interval") != "hour" {
			t.Error("DEX request omitted required interval")
		}
		fmt.Fprint(w, `{"result":{"over_time":{"cpu_pct":[{"timestamp":"2026-11-10 09:00:00+00","value":42}]}}}`)
	})
	q := Query{QueryType: "rest", Operation: "dex-device", ResourceID: "device-id", Format: "timeseries", Columns: []string{"cpu_pct"}}
	r := d.query(context.Background(), backend.PluginContext{}, dataQuery(q))
	if r.Error != nil || len(r.Frames) != 1 {
		t.Fatalf("DEX series failed: %+v", r)
	}
	f := r.Frames[0]
	if !f.Fields[0].At(0).(time.Time).Equal(dataQuery(q).TimeRange.From) || *f.Fields[1].At(0).(*float64) != 42 {
		t.Fatal("historical series used fetch time instead of observation time")
	}
	if f.Fields[1].Labels["id"] != "device-id" {
		t.Fatal("DEX series lost device identity")
	}
	if _, ok := f.Fields[1].Labels["snapshotTime"]; ok {
		t.Fatal("fetch time must not change the identity of a historical series")
	}
}

func TestRESTConfigurationAndDiscoveryAreNotPanelQueries(t *testing.T) {
	d := testDatasource(t, func(http.ResponseWriter, *http.Request) { t.Fatal("configuration query reached Cloudflare") })
	for _, operation := range []string{
		"dns-records", "load-balancers", "pool-origins", "rulesets", "rules", "posture-rules", "dlp-profiles", "gateway-rules",
		"d1", "r2", "kv", "queues", "dns-firewall", "pools", "workers", "pages", "containers", "durable-namespaces", "ai-gateways", "ai-search", "devices",
	} {
		q := Query{QueryType: "rest", Operation: operation, ZoneID: strings.Repeat("b", 32), ResourceID: "resource", Format: "table"}
		result := d.query(context.Background(), backend.PluginContext{}, dataQuery(q))
		if result.Error == nil || result.Status != backend.StatusBadRequest {
			t.Fatalf("accepted non-observability panel query %s", operation)
		}
		for _, op := range restCatalog() {
			if op.Name == operation {
				t.Fatalf("non-query source %s appeared in editor", operation)
			}
		}
	}
}

func TestRESTDiscoveryReturnsOnlyNamesAndUsableIDs(t *testing.T) {
	for _, operation := range []string{"workers", "pages", "containers", "durable-namespaces", "dns-firewall", "pools", "ai-gateways", "ai-search", "devices", "dex-tests"} {
		t.Run(operation, func(t *testing.T) {
			d := testDatasource(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Fatal("discovery must be read-only")
				}
				row := `{"id":"resource-id","name":"resource-name","expression":"configuration","serial_number":"device-serial","env_vars":{"TOKEN":"secret"}}`
				if operation == "workers" {
					row = `{"id":"resource-id","env_vars":{"TOKEN":"secret"}}`
				}
				if operation == "dex-tests" {
					fmt.Fprintf(w, `{"result":{"tests":[%s]}}`, row)
				} else {
					fmt.Fprintf(w, `{"result":[%s]}`, row)
				}
			})
			var response *backend.CallResourceResponse
			sender := backend.CallResourceResponseSenderFunc(func(r *backend.CallResourceResponse) error { response = r; return nil })
			err := d.CallResource(context.Background(), &backend.CallResourceRequest{Method: "GET", Path: "resources", URL: "/resources?operation=" + operation}, sender)
			if err != nil || response == nil || response.Status != 200 {
				t.Fatalf("discovery failed: %v %+v", err, response)
			}
			var body struct {
				Result []map[string]string `json:"result"`
			}
			if err := json.Unmarshal(response.Body, &body); err != nil {
				t.Fatal(err)
			}
			wantID, wantName := "resource-id", "resource-name"
			if operation == "pages" {
				wantID = wantName
			}
			if operation == "workers" {
				wantName = wantID
			}
			if len(body.Result) != 1 || len(body.Result[0]) != 2 || body.Result[0]["id"] != wantID || body.Result[0]["name"] != wantName {
				t.Fatalf("discovery leaked fields or lost resource identifier: %s", response.Body)
			}
		})
	}
}

func TestRESTPagesPagination(t *testing.T) {
	for _, operation := range []string{"pages", "pages-deployments"} {
		t.Run(operation, func(t *testing.T) {
			calls := 0
			d := testDatasource(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Query().Get("per_page") != "10" {
					t.Fatal("Pages rejected page sizes above ten in live validation")
				}
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				rows := []map[string]any{}
				for i := (page - 1) * 10; i < min(page*10, 15); i++ {
					rows = append(rows, map[string]any{"id": fmt.Sprint(i), "name": fmt.Sprint(i), "created_on": "2026-11-10T09:30:00Z"})
				}
				json.NewEncoder(w).Encode(map[string]any{"result": rows, "result_info": map[string]any{"total_pages": 2, "total_count": 15}})
			})
			if operation == "pages" {
				var response *backend.CallResourceResponse
				sender := backend.CallResourceResponseSenderFunc(func(r *backend.CallResourceResponse) error { response = r; return nil })
				err := d.CallResource(context.Background(), &backend.CallResourceRequest{Method: "GET", Path: "resources", URL: "/resources?operation=pages"}, sender)
				if err != nil || response == nil || response.Status != 200 {
					t.Fatalf("Pages discovery failed: %v %+v", err, response)
				}
				var body struct {
					Result []map[string]string `json:"result"`
				}
				if err := json.Unmarshal(response.Body, &body); err != nil {
					t.Fatal(err)
				}
				if len(body.Result) != 15 || body.Result[14]["id"] != "14" {
					t.Fatal("Pages discovery skipped or duplicated resources")
				}
			} else {
				q := Query{QueryType: "rest", Operation: operation, ResourceID: "project", Format: "logs", Limit: 15}
				result := d.query(context.Background(), backend.PluginContext{}, dataQuery(q))
				if result.Error != nil || len(result.Frames) != 1 || result.Frames[0].Rows() != 15 {
					t.Fatalf("Pages history pagination failed: %+v", result)
				}
			}
			if calls != 2 {
				t.Fatalf("Pages pagination made %d requests, expected two", calls)
			}
		})
	}
}
