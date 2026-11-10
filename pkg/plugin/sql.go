package plugin

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var datasetName = regexp.MustCompile(`^(events|states|logs)\.[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_-]*)?$`)
var tagPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var attributeField = regexp.MustCompile(`^attributes\['[^'\\\x00-\x1f]+'\]$`)
var timeGroup = regexp.MustCompile(`\$__timeGroup\(([A-Za-z_][A-Za-z0-9_]*)\)`)

func fieldSQL(field string) (string, error) {
	if attributeField.MatchString(field) {
		return field, nil
	}
	if !identifier.MatchString(field) {
		return "", fmt.Errorf("invalid field %q", field)
	}
	return "`" + field + "`", nil
}

func intervalSeconds(q backend.DataQuery) int64 {
	seconds := int64(q.Interval / time.Second)
	if q.Interval <= 0 {
		return 60
	}
	if seconds < 1 {
		return 1
	}
	return seconds
}

func expandMacros(sql string, dq backend.DataQuery, q Query) string {
	seconds := intervalSeconds(dq)
	sql = timeGroup.ReplaceAllStringFunc(sql, func(match string) string {
		field := timeGroup.FindStringSubmatch(match)[1]
		if q.QueryType != "analytics" {
			field = "`" + field + "`"
		}
		return fmt.Sprintf("toStartOfInterval(%s, INTERVAL '%d' SECOND)", field, seconds)
	})
	return strings.NewReplacer(
		"$__interval_ms", strconv.FormatInt(seconds*1000, 10),
		"$__interval_s", strconv.FormatInt(seconds, 10),
		"$__fromTime", "'"+dq.TimeRange.From.UTC().Format("2006-01-02 15:04:05")+"'",
		"$__from_s", strconv.FormatInt(dq.TimeRange.From.Unix(), 10),
		"$__toTime", "'"+dq.TimeRange.To.UTC().Format("2006-01-02 15:04:05")+"'",
		"$__to_s", strconv.FormatInt(dq.TimeRange.To.Unix(), 10),
		"$__accountId", "'"+q.AccountID+"'", "$__zoneId", "'"+q.ZoneID+"'",
	).Replace(sql)
}

func buildSQL(q Query, dq backend.DataQuery, maxRows int) (string, map[string]any, error) {
	if !datasetName.MatchString(q.Dataset) {
		return "", nil, fmt.Errorf("select a schema-qualified dataset from the catalog")
	}
	dataset := q.Dataset
	if strings.Contains(dataset, "-") {
		parts := strings.SplitN(dataset, ".", 3)
		if len(parts) != 3 || parts[0] != "events" || parts[1] != "analyticsEngine" {
			return "", nil, fmt.Errorf("invalid dataset name")
		}
		dataset = parts[0] + "." + parts[1] + ".\"" + parts[2] + "\""
	}
	params := make(map[string]any)
	var selectParts, groupParts []string
	bucketed := false
	selected := map[string]bool{}
	addColumn := func(name string) error {
		if selected[name] {
			return fmt.Errorf("duplicate result column %q; choose unique metric aliases and dimensions", name)
		}
		selected[name] = true
		return nil
	}
	format := q.Format
	if format == "" {
		format = "timeseries"
	}
	timeColumn := ""
	if format == "logs" || format == "timeseries" {
		name := q.TimeField
		if name == "" {
			name = "timestamp"
		}
		var err error
		timeColumn, err = fieldSQL(name)
		if err != nil {
			return "", nil, err
		}
	}
	columns := q.Columns
	if format == "logs" && len(columns) == 0 {
		columns = []string{"timestamp", "message", "level", "scriptName", "traceId", "spanId", "id", "requestId"}
		if q.Dataset == "logs.containerLogs" {
			columns = []string{"timestamp", "message", "level", "containerId", "placementId", "id"}
		}
	}
	if format == "traces" {
		selectParts = []string{"traceId", "spanId", "parentSpanId", "serviceName", "spanName", "startTime", "durationMs", "spanStatus", "attributes"}
		if q.TraceID == "" {
			selectParts = []string{"traceId", "serviceName", "spanName", "startTime", "durationMs", "spanStatus"}
		}
	} else if format == "logs" || (len(columns) > 0 && len(q.Metrics) == 0) {
		for _, name := range columns {
			if err := addColumn(name); err != nil {
				return "", nil, err
			}
			col, err := fieldSQL(name)
			if err != nil {
				return "", nil, err
			}
			selectParts = append(selectParts, col)
		}
	} else {
		if format == "timeseries" {
			bucket := fmt.Sprintf("toStartOfInterval(%s, INTERVAL '%d' SECOND)", timeColumn, intervalSeconds(dq))
			selectParts = append(selectParts, bucket+" AS time")
			groupParts = append(groupParts, "time")
			selected["time"] = true
			bucketed = true
		}
		for _, name := range q.GroupBy {
			if err := addColumn(name); err != nil {
				return "", nil, err
			}
			col, err := fieldSQL(name)
			if err != nil {
				return "", nil, err
			}
			selectParts = append(selectParts, col)
			groupParts = append(groupParts, col)
		}
		metrics := q.Metrics
		if len(metrics) == 0 {
			metrics = []Metric{{Aggregation: "count", Alias: "count"}}
		}
		for i, m := range metrics {
			alias := m.Alias
			if alias == "" {
				alias = fmt.Sprintf("value_%d", i)
			}
			if !identifier.MatchString(alias) {
				return "", nil, fmt.Errorf("invalid metric alias %q", alias)
			}
			if err := addColumn(alias); err != nil {
				return "", nil, err
			}
			var expression string
			if m.Aggregation == "count" {
				expression = "COUNT(*)"
			} else {
				col, err := fieldSQL(m.Field)
				if err != nil {
					return "", nil, err
				}
				switch m.Aggregation {
				case "sum", "avg", "min", "max":
					expression = strings.ToUpper(m.Aggregation) + "(" + col + ")"
				case "p50", "p90", "p95", "p99":
					weight := "sampleInterval"
					if q.Sampled != nil && !*q.Sampled {
						weight = "1"
					}
					expression = fmt.Sprintf("quantileWeighted(0.%s, %s, %s)", strings.TrimPrefix(m.Aggregation, "p"), col, weight)
				default:
					return "", nil, fmt.Errorf("unsupported aggregation %q", m.Aggregation)
				}
			}
			selectParts = append(selectParts, expression+" AS `"+alias+"`")
		}
	}
	var filters []string
	if format == "traces" && q.TraceID == "" {
		filters = append(filters, "parentSpanId = ''")
	}
	for i, f := range q.Filters {
		col, err := fieldSQL(f.Field)
		if err != nil {
			return "", nil, err
		}
		op := strings.ToUpper(f.Operator)
		if op == "IS NULL" || op == "IS NOT NULL" {
			filters = append(filters, col+" "+op)
			continue
		}
		switch op {
		case "=", "!=", ">", ">=", "<", "<=", "LIKE", "NOT LIKE":
		default:
			return "", nil, fmt.Errorf("unsupported filter operator %q", f.Operator)
		}
		key := fmt.Sprintf("filter_%d", i)
		params[key] = f.Value
		filters = append(filters, col+" "+op+" $"+key)
	}
	if q.TraceID != "" {
		filters = append(filters, "traceId = $trace_id")
		params["trace_id"] = q.TraceID
	}
	sql := "SELECT " + strings.Join(selectParts, ", ") + " FROM " + dataset
	if len(filters) > 0 {
		sql += " WHERE " + strings.Join(filters, " AND ")
	}
	if len(groupParts) > 0 {
		sql += " GROUP BY " + strings.Join(groupParts, ", ")
	}
	orderColumn, direction := timeColumn, "ASC"
	switch format {
	case "logs":
		if q.LogOrder != "asc" {
			direction = "DESC"
		}
	case "traces":
		orderColumn = "startTime"
		if q.TraceID == "" {
			direction = "DESC"
		}
	case "timeseries":
		if bucketed {
			orderColumn = "time"
		}
	}
	if orderColumn != "" {
		sql += " ORDER BY " + orderColumn + " " + direction
	}
	sql += fmt.Sprintf(" LIMIT %d", q.rowLimit(maxRows))
	return sql, params, nil
}

func decodeParams(source string) (map[string]any, error) {
	params := make(map[string]any)
	if strings.TrimSpace(source) == "" {
		return params, nil
	}
	if err := decodeJSON([]byte(source), &params); err != nil {
		return nil, fmt.Errorf("parameters must be a JSON object: %w", err)
	}
	if params == nil {
		return nil, fmt.Errorf("parameters must be a JSON object, not null")
	}
	return params, nil
}
