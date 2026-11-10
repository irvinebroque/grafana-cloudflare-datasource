package plugin

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
)

const maxFrameFields = 1000
const maxFrameCells = 2000000
const maxTimeSeries = 10000
const maxSeriesLabelBytes = 32 << 20

func number(value any) (*float64, bool) {
	if value == nil {
		return nil, true
	}
	var n float64
	var err error
	switch v := value.(type) {
	case json.Number:
		n, err = v.Float64()
	case float64:
		n = v
	case string:
		n, err = strconv.ParseFloat(v, 64)
	default:
		return nil, false
	}
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return nil, false
	}
	return &n, true
}

func timestamp(value any) (*time.Time, bool) {
	if value == nil {
		return nil, true
	}
	if s, ok := value.(string); ok {
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05Z07", "2006-01-02 15:04:05.999999999", "2006-01-02", "2006-01-02T15:04:05"} {
			if t, err := time.Parse(layout, s); err == nil {
				return &t, true
			}
		}
	}
	if n, ok := number(value); ok && n != nil {
		millis := *n
		if millis >= 1e17 {
			millis /= 1e6
		} else if millis >= 1e14 {
			millis /= 1e3
		} else if millis < 1e11 {
			millis *= 1000
		}
		if millis < math.MinInt64 || millis >= math.MaxInt64 {
			return nil, false
		}
		t := time.UnixMilli(int64(millis)).UTC()
		if t.Year() < 1 || t.Year() > 9999 {
			return nil, false
		}
		return &t, true
	}
	return nil, false
}

func textValue(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	b, _ := json.Marshal(value)
	return string(b)
}

func isTimeField(name, hint, configured string) bool {
	return name == configured || name == "time" || name == "timestamp" || name == "datetime" || name == "startTime" || strings.HasPrefix(columnType(hint), "Date")
}

func columnType(hint string) string {
	for {
		unwrapped := false
		for _, wrapper := range []string{"Nullable(", "LowCardinality("} {
			if strings.HasPrefix(hint, wrapper) && strings.HasSuffix(hint, ")") {
				hint = strings.TrimSpace(hint[len(wrapper) : len(hint)-1])
				unwrapped = true
				break
			}
		}
		if !unwrapped {
			return hint
		}
	}
}

func numericType(hint string) bool {
	hint = columnType(hint)
	return strings.HasPrefix(hint, "UInt") || strings.HasPrefix(hint, "Int") || strings.HasPrefix(hint, "Float") || strings.HasPrefix(hint, "Decimal")
}

func numericColumn(rows []map[string]any, column resultColumn) bool {
	if numericType(column.Type) {
		return true
	}
	for _, row := range rows {
		if value := row[column.Name]; value != nil {
			switch value.(type) {
			case json.Number, float64:
				return true
			}
			return false
		}
	}
	return false
}

func resultFrames(result SQLResult, q Query, refID, uid, executed string, maxRows int) (data.Frames, error) {
	rows := result.Data
	if q.Format == "logs" {
		rows = logRows(rows)
		// Include the canonical display message alongside the provider fields.
		if len(rows) > 0 {
			found := false
			for _, column := range result.Meta {
				if column.Name == "message" {
					found = true
				}
			}
			if len(result.Meta) > 0 && !found {
				result.Meta = append(append([]resultColumn{}, result.Meta...), resultColumn{Name: "message", Type: "String"})
			}
		}
	}
	truncated := result.Incomplete || len(rows) > maxRows
	if len(rows) > maxRows {
		rows = rows[:maxRows]
	}
	columns, err := resultColumns(rows, result)
	if err != nil {
		return nil, err
	}
	if len(columns) > maxFrameFields {
		return nil, fmt.Errorf("query returned too many columns (limit %d); select fewer fields", maxFrameFields)
	}
	if len(rows) > 0 && len(columns) > maxFrameCells/len(rows) {
		return nil, fmt.Errorf("query exceeds the %d frame cell limit; reduce columns or rows", maxFrameCells)
	}
	var frames data.Frames
	switch q.Format {
	case "traces":
		if (q.QueryType == "builder" || q.QueryType == "") && q.TraceID == "" {
			frames = data.Frames{tableFrame(rows, q, refID, columns)}
		} else {
			frames, err = traceFrames(rows, refID)
			if err == nil && len(rows) > 0 {
				frames = append(frames, traceGraph(frames[0], refID)...)
			}
		}
	case "timeseries", "":
		frames, err = seriesFrames(rows, q, refID, columns)
	default:
		frames = data.Frames{tableFrame(rows, q, refID, columns)}
	}
	if err != nil {
		return nil, err
	}
	for index, frame := range frames {
		if frame.Meta == nil {
			frame.Meta = &data.FrameMeta{}
		}
		if index == 0 {
			frame.Meta.ExecutedQueryString = executed
		}
		frame.Meta.Custom = map[string]any{"statistics": result.Statistics, "rows": len(rows), "accountId": q.AccountID, "zoneId": q.ZoneID, "dataset": q.Dataset}
		if frame.Meta.PreferredVisualization == data.VisTypeTrace && uid != "" {
			frame.Fields[0].Config = &data.FieldConfig{Links: []data.DataLink{{Title: "Related Worker logs", URL: "", Internal: &data.InternalDataLink{DatasourceUID: uid, Query: map[string]any{"queryType": "builder", "dataset": "logs.workersLogs", "format": "logs", "accountId": q.AccountID, "scope": "account", "filters": []map[string]any{{"field": "traceId", "operator": "=", "value": "${__value.raw}", "literal": true}}, "limit": 1000}}}}}
		}
		limit := maxRows
		if q.QueryType == "builder" || q.QueryType == "" || q.QueryType == "graphql-builder" || q.QueryType == "rest" {
			limit = q.rowLimit(maxRows)
		}
		if truncated || len(rows) >= limit {
			frame.Meta.Notices = append(frame.Meta.Notices, data.Notice{Severity: data.NoticeSeverityWarning, Text: "Row limit reached. Narrow the query or raise the limit; results may be incomplete."})
		}
		for _, notice := range result.Notices {
			frame.Meta.Notices = append(frame.Meta.Notices, data.Notice{Severity: data.NoticeSeverityWarning, Text: notice})
		}
		if q.Format == "logs" || (q.Format == "traces" && q.TraceID == "") {
			if q.Format == "logs" {
				frame.Meta.PreferredVisualization = data.VisTypeLogs
			}
			for _, field := range frame.Fields {
				if field.Config == nil {
					field.Config = &data.FieldConfig{}
				}
				if field.Config.Filterable == nil {
					field.Config.SetFilterable(false)
				}
				if field.Name == "traceId" && uid != "" {
					field.Config.Links = []data.DataLink{{Title: "View trace", URL: "", Internal: &data.InternalDataLink{DatasourceUID: uid, Query: map[string]any{"queryType": "builder", "dataset": "logs.traces", "format": "traces", "traceId": "${__value.raw}", "accountId": q.AccountID, "scope": "account", "limit": 10000}}}}
				}
				if q.QueryType == "rest" && q.Operation == "audit" && field.Name == "id" && uid != "" {
					field.Config.Links = []data.DataLink{{Title: "Resource history", URL: "", Internal: &data.InternalDataLink{DatasourceUID: uid, Query: map[string]any{"queryType": "rest", "operation": "audit-history", "resourceId": "${__value.raw}", "format": "logs", "accountId": q.AccountID, "scope": "account", "filters": []map[string]any{{"field": "action_time", "operator": "=", "value": "${__data.fields[\"action.time\"]}", "literal": true}}, "limit": 1000}}}}
				}
			}
		}
	}
	return frames, nil
}

func tableFrame(rows []map[string]any, q Query, refID string, columns []resultColumn) *data.Frame {
	if len(columns) == 0 && q.Format == "logs" {
		columns = []resultColumn{{Name: "timestamp"}, {Name: "message"}, {Name: "level"}}
	}
	if q.Format == "logs" {
		priority := map[string]int{"timestamp": 5, "time": 4, "message": 3, "line": 2, "level": 1}
		if q.TimeField != "" {
			priority[q.TimeField] = 6
		}
		sort.SliceStable(columns, func(i, j int) bool { return priority[columns[i].Name] > priority[columns[j].Name] })
	}
	frame := data.NewFrame(refID)
	frame.RefID = refID
	frame.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeTable}
	for _, column := range columns {
		key := column.Name
		if isTimeField(key, column.Type, q.TimeField) {
			values := make([]*time.Time, len(rows))
			for i, row := range rows {
				values[i], _ = timestamp(row[key])
			}
			frame.Fields = append(frame.Fields, data.NewField(key, nil, values))
			continue
		}
		if numericColumn(rows, column) {
			values := make([]*float64, len(rows))
			for i, row := range rows {
				values[i], _ = number(row[key])
			}
			field := data.NewField(key, nil, values)
			field.Config = &data.FieldConfig{Unit: metricUnit(q, key)}
			frame.Fields = append(frame.Fields, field)
			continue
		}
		boolean := columnType(column.Type) == "Bool"
		for _, row := range rows {
			if row[key] != nil {
				_, boolean = row[key].(bool)
				break
			}
		}
		if boolean {
			values := make([]*bool, len(rows))
			for i, row := range rows {
				if v, ok := row[key].(bool); ok {
					values[i] = &v
				}
			}
			frame.Fields = append(frame.Fields, data.NewField(key, nil, values))
			continue
		}
		values := make([]*string, len(rows))
		filterable := q.Format == "logs" && (q.QueryType == "builder" || q.QueryType == "") && identifier.MatchString(key) && key != "attributes"
		for i, row := range rows {
			if row[key] != nil {
				if _, ok := row[key].(string); !ok {
					filterable = false
				}
				s := textValue(row[key])
				values[i] = &s
			}
		}
		field := data.NewField(key, nil, values)
		field.Config = (&data.FieldConfig{}).SetFilterable(filterable)
		frame.Fields = append(frame.Fields, field)
	}
	return frame
}

func resultColumns(rows []map[string]any, result SQLResult) ([]resultColumn, error) {
	columns := append([]resultColumn{}, result.Meta...)
	seen := map[string]bool{}
	for _, meta := range result.Meta {
		if seen[meta.Name] {
			return nil, fmt.Errorf("result contains duplicate column %q; use unique aliases", meta.Name)
		}
		seen[meta.Name] = true
	}
	var additional []string
	for _, row := range rows {
		for key := range row {
			if !seen[key] {
				additional = append(additional, key)
				seen[key] = true
			}
		}
	}
	sort.Strings(additional)
	for _, name := range additional {
		columns = append(columns, resultColumn{Name: name})
	}
	return columns, nil
}

func seriesFrames(rows []map[string]any, q Query, refID string, columns []resultColumn) (data.Frames, error) {
	if len(rows) == 0 {
		frame := data.NewFrame(refID, data.NewField("time", nil, []time.Time{}), data.NewField("value", nil, []float64{}))
		frame.RefID = refID
		frame.Meta = &data.FrameMeta{Type: data.FrameTypeTimeSeriesMulti}
		return data.Frames{frame}, nil
	}
	timeKey := q.TimeField
	if q.QueryType == "builder" || q.QueryType == "" || timeKey == "" {
		if _, ok := rows[0]["time"]; ok {
			timeKey = "time"
		}
	}
	if timeKey == "" {
		for _, name := range []string{"time", "timestamp", "datetime", "dimensions.datetime", "dimensions.datetimeMinute", "dimensions.datetimeFiveMinutes", "dimensions.datetimeHour"} {
			if _, ok := rows[0][name]; ok {
				timeKey = name
				break
			}
		}
	}
	if timeKey == "" {
		return nil, fmt.Errorf("time series requires a time column; select Table for aggregates without time")
	}
	dimensions := map[string]bool{}
	for _, name := range q.GroupBy {
		dimensions[name] = true
	}
	metricNames := []string{}
	metrics := map[string]Metric{}
	for _, metric := range q.Metrics {
		metrics[metric.Alias] = metric
	}
	for _, column := range columns {
		key := column.Name
		if key == timeKey {
			continue
		}
		_, numeric := metrics[key]
		numeric = numeric || numericColumn(rows, column)
		if numeric && !dimensions[key] && !strings.HasPrefix(key, "dimensions.") {
			metricNames = append(metricNames, key)
		} else {
			dimensions[key] = true
		}
	}
	if len(metricNames) == 0 {
		return nil, fmt.Errorf("time series requires at least one numeric value column")
	}
	type point struct {
		t time.Time
		v *float64
	}
	type series struct {
		labels data.Labels
		metric string
		points []point
	}
	type seriesKey struct {
		metric string
		labels string
	}
	groups := map[seriesKey]*series{}
	order := []seriesKey{}
	labelBytes := 0
	for _, row := range rows {
		t, ok := timestamp(row[timeKey])
		if !ok || t == nil {
			return nil, fmt.Errorf("invalid time value in column %s", timeKey)
		}
		labels := data.Labels{}
		for key := range dimensions {
			labels[key] = textValue(row[key])
		}
		encoded, _ := json.Marshal(labels)
		encodedLabels := string(encoded)
		for _, metric := range metricNames {
			key := seriesKey{metric: metric, labels: encodedLabels}
			s := groups[key]
			if s == nil {
				if len(groups) >= maxTimeSeries {
					return nil, fmt.Errorf("query exceeds the %d time series limit; reduce dimensions or metrics", maxTimeSeries)
				}
				labelBytes += len(encoded)
				if labelBytes > maxSeriesLabelBytes {
					return nil, fmt.Errorf("time series labels exceed 32 MiB; reduce dimensions or metrics")
				}
				s = &series{labels: labels, metric: metric}
				groups[key] = s
				order = append(order, key)
			}
			value, _ := number(row[metric])
			s.points = append(s.points, point{*t, value})
		}
	}
	frames := data.Frames{}
	for _, key := range order {
		s := groups[key]
		sort.SliceStable(s.points, func(i, j int) bool { return s.points[i].t.Before(s.points[j].t) })
		times := make([]time.Time, len(s.points))
		values := make([]*float64, len(s.points))
		for i, p := range s.points {
			times[i] = p.t
			values[i] = p.v
		}
		frame := data.NewFrame(s.metric, data.NewField("time", nil, times), data.NewField(s.metric, s.labels, values))
		frame.RefID = refID
		frame.Meta = &data.FrameMeta{Type: data.FrameTypeTimeSeriesMulti, PreferredVisualization: data.VisTypeGraph}
		unit := metricUnit(q, s.metric)
		if unit != "" {
			frame.Fields[1].Config = &data.FieldConfig{Unit: unit}
		}
		frames = append(frames, frame)
	}
	return frames, nil
}

func metricUnit(q Query, name string) string {
	for _, metric := range q.Metrics {
		if metric.Alias == name && (q.QueryType == "builder" || q.QueryType == "") {
			if metric.Aggregation == "count" {
				return ""
			}
			name = metric.Field
			break
		}
	}
	switch {
	case name == "daysUntilExpiry":
		return "d"
	case strings.HasSuffix(name, "Us"):
		return "µs"
	case strings.HasSuffix(name, "Ms"):
		return "ms"
	case strings.HasSuffix(name, "Bytes"):
		return "bytes"
	default:
		return ""
	}
}
