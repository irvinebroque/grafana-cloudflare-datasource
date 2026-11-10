package plugin

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

func TestUpstreamErrorStatusAndSource(t *testing.T) {
	for _, status := range []int{401, 403, 422, 429, 500, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			d := testDatasource(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "0")
				w.Header().Set("CF-Ray", "test-ray")
				w.WriteHeader(status)
				fmt.Fprint(w, "private-token query failed")
			})
			response := d.query(context.Background(), backend.PluginContext{}, dataQuery(Query{QueryType: "sql", SQL: "SELECT 1"}))
			if response.Status != backend.Status(status) || response.ErrorSource != backend.ErrorSourceDownstream || !backend.IsDownstreamError(response.Error) {
				t.Fatalf("lost upstream status/source: %+v", response)
			}
			if strings.Contains(response.Error.Error(), "private-token") || !strings.Contains(response.Error.Error(), "test-ray") {
				t.Fatalf("lost redaction or ray ID: %v", response.Error)
			}
		})
	}
}

func TestEnvelopeFailureAndTimeoutAreDownstreamErrors(t *testing.T) {
	d := testDatasource(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"success":false,"errors":[{"message":"failed"}]}`)
	})
	response := d.query(context.Background(), backend.PluginContext{}, dataQuery(Query{QueryType: "sql", SQL: "SELECT 1"}))
	if response.Status != backend.StatusBadGateway || response.ErrorSource != backend.ErrorSourceDownstream {
		t.Fatalf("successful HTTP status hid envelope failure: %+v", response)
	}
	d = testDatasource(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	response = d.query(ctx, backend.PluginContext{}, dataQuery(Query{QueryType: "sql", SQL: "SELECT 1"}))
	if response.Status != backend.StatusTimeout || response.ErrorSource != backend.ErrorSourceDownstream {
		t.Fatalf("timeout became a bad query: %+v", response)
	}
	response = d.query(context.Background(), backend.PluginContext{}, dataQuery(Query{QueryType: "sql", Params: "null"}))
	if response.Status != backend.StatusBadRequest {
		t.Fatalf("input validation lost its status: %+v", response)
	}
}

func TestLogFieldFilteringPreservesTraceLinks(t *testing.T) {
	result, _ := decodeSQL([]byte(`{"data":[{"timestamp":"2026-11-10T09:01:00Z","message":"hello","level":"info","traceId":"trace","attributes":{"key":"value"},"cpuTimeUs":5}]}`))
	for _, mode := range []string{"builder", "sql", "telemetry"} {
		frames, err := resultFrames(result, Query{QueryType: mode, Format: "logs"}, "A", "source", "SELECT", 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range frames[0].Fields {
			filterable := mode == "builder" && (field.Name == "message" || field.Name == "level" || field.Name == "traceId")
			if field.Config.Filterable == nil || *field.Config.Filterable != filterable {
				t.Errorf("%s/%s filterability incorrect", mode, field.Name)
			}
			if field.Name == "traceId" && len(field.Config.Links) != 1 {
				t.Error("filter metadata removed trace link")
			}
		}
	}
}

func TestMetricUnitsAcrossBuilderAndRawFormats(t *testing.T) {
	for _, format := range []string{"table", "timeseries"} {
		for _, mode := range []string{"builder", "sql"} {
			name := "cpuTimeUs"
			q := Query{QueryType: mode, Format: format, Metrics: []Metric{{Aggregation: "p99", Field: name, Alias: "cpu"}}}
			if mode == "builder" {
				name = "cpu"
			}
			result := SQLResult{Data: []map[string]any{{"time": "2026-11-10T09:01:00Z", name: float64(5)}}}
			frames, err := resultFrames(result, q, "A", "", "SELECT", 100)
			if err != nil {
				t.Fatal(err)
			}
			for _, field := range frames[0].Fields {
				if field.Name == name && (field.Config == nil || field.Config.Unit != "µs") {
					t.Errorf("%s/%s lost unit", mode, format)
				}
			}
		}
	}
	if unit := metricUnit(Query{Metrics: []Metric{{Aggregation: "count", Field: "cpuTimeUs", Alias: "totalBytes"}}}, "totalBytes"); unit != "" {
		t.Fatal("count inherited a measurement unit")
	}
}
