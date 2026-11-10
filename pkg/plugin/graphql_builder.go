package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

func graphSelection(paths []string) string {
	fields := []string{}
	children := map[string][]string{}
	order := []string{}
	for _, path := range paths {
		root, rest, nested := strings.Cut(path, ".")
		if !nested {
			fields = append(fields, root)
			continue
		}
		if _, exists := children[root]; !exists {
			order = append(order, root)
		}
		children[root] = append(children[root], rest)
	}
	for _, root := range order {
		fields = append(fields, root+" { "+graphSelection(children[root])+" }")
	}
	return strings.Join(fields, " ")
}

func graphFilterValue(value, declaration string) (any, error) {
	if strings.HasPrefix(declaration, "[") {
		var result []any
		if err := decodeJSON([]byte(value), &result); err != nil || result == nil {
			return nil, fmt.Errorf("list filter requires a JSON array")
		}
		return result, nil
	}
	base := strings.ToLower(strings.TrimSuffix(declaration, "!"))
	if base == "bool" || base == "boolean" {
		return strconv.ParseBool(value)
	}
	if strings.Contains(base, "int") || strings.HasPrefix(base, "float") || strings.HasPrefix(base, "decimal") {
		if strings.Contains(base, "int") {
			n, ok := new(big.Int).SetString(value, 10)
			if !ok || strings.HasPrefix(base, "uint") && n.Sign() < 0 {
				return nil, fmt.Errorf("filter requires an integer value")
			}
		}
		var parsed json.Number
		if err := decodeJSON([]byte(value), &parsed); err != nil || parsed == "" {
			return nil, fmt.Errorf("filter requires a numeric value")
		}
		return parsed, nil
	}
	return value, nil
}

func graphTimeField(dataset graphDataset, interval time.Duration) string {
	if interval <= 0 {
		for _, name := range []string{"datetime", "timestamp", "dimensions.datetime", "dimensions.timestamp"} {
			for _, column := range dataset.Fields {
				if column.Name == name && strings.TrimSuffix(column.Type, "!") == "Time" {
					return column.Name
				}
			}
		}
	}
	// Choose a provider-supported bucket without combining already aggregated quantiles.
	for _, bucket := range []struct {
		name  string
		width time.Duration
	}{
		{"datetimeMinute", time.Minute}, {"datetimeFiveMinutes", 5 * time.Minute}, {"datetimeFifteenMinutes", 15 * time.Minute}, {"datetimeHour", time.Hour}, {"datetimeSixHours", 6 * time.Hour}, {"date", 24 * time.Hour},
	} {
		if bucket.width < interval {
			continue
		}
		for _, column := range dataset.Fields {
			if column.Name == "dimensions."+bucket.name || column.Name == bucket.name {
				return column.Name
			}
		}
	}
	for _, column := range dataset.Fields {
		if column.Type == "Time!" || column.Type == "Time" || column.Type == "Date!" || column.Type == "Date" {
			return column.Name
		}
	}
	return ""
}

func (d *Datasource) graphBuilderQuery(ctx context.Context, q *Query, dq backend.DataQuery) (SQLResult, string, error) {
	catalog, err := d.graphCatalog(ctx, *q)
	if err != nil {
		return SQLResult{}, "", err
	}
	if q.Dataset == "" {
		return SQLResult{}, "", fmt.Errorf("select a GraphQL dataset")
	}
	dataset := catalog[0]
	if !dataset.Settings.Enabled {
		return SQLResult{}, "", fmt.Errorf("%s is not enabled for this scope and token; check the product plan and analytics permissions", dataset.Name)
	}
	if dataset.Settings.MaxDuration > 0 && dq.TimeRange.Duration() > time.Duration(dataset.Settings.MaxDuration)*time.Second {
		return SQLResult{}, "", fmt.Errorf("%s supports at most %s per query; narrow the time range", dataset.Name, time.Duration(dataset.Settings.MaxDuration)*time.Second)
	}
	columns := map[string]graphColumn{}
	for _, field := range dataset.Fields {
		columns[field.Name] = field
	}
	// The provider counts the scope and dataset fields in addition to the selected data fields.
	fieldLimit := dataset.Settings.MaxFields
	if fieldLimit > 0 {
		fieldLimit -= 2
		if fieldLimit < 1 {
			return SQLResult{}, "", fmt.Errorf("Cloudflare field limit is too small for this query")
		}
	}
	paths := []string{}
	selected := map[string]bool{}
	add := func(path string) error {
		if _, ok := columns[path]; !ok {
			return fmt.Errorf("field %q is not available in %s", path, dataset.Name)
		}
		if !selected[path] {
			paths = append(paths, path)
			selected[path] = true
		}
		return nil
	}
	if q.TimeField == "" {
		interval := time.Duration(0)
		if q.Format == "timeseries" {
			interval = dq.Interval
		}
		q.TimeField = graphTimeField(dataset, interval)
	}
	if q.TimeField != "" {
		typ := strings.TrimSuffix(columns[q.TimeField].Type, "!")
		if typ != "Time" && typ != "Date" {
			return SQLResult{}, "", fmt.Errorf("select a Time or Date field for the time bucket")
		}
		if err := add(q.TimeField); err != nil {
			return SQLResult{}, "", err
		}
	}
	if q.Format == "timeseries" && q.TimeField == "" {
		return SQLResult{}, "", fmt.Errorf("this dataset has no time field; use Table")
	}
	for _, path := range q.GroupBy {
		if err := add(path); err != nil {
			return SQLResult{}, "", err
		}
	}
	chosen := q.Columns
	limitedFields := false
	if len(chosen) == 0 && q.Format == "logs" {
		for _, field := range dataset.Fields {
			if !strings.Contains(field.Name, ".") && !selected[field.Name] {
				if fieldLimit > 0 && len(chosen)+len(paths) >= fieldLimit {
					limitedFields = true
					continue
				}
				chosen = append(chosen, field.Name)
			}
		}
	}
	if len(chosen) == 0 {
		for _, preferred := range []string{"sum.requests", "count", "sum.count"} {
			if _, ok := columns[preferred]; ok {
				chosen = []string{preferred}
				break
			}
		}
		if len(chosen) == 0 {
			for _, field := range dataset.Fields {
				if field.Numeric && !strings.HasSuffix(field.Name, ".sampleInterval") && (strings.HasPrefix(field.Name, "sum.") || strings.HasPrefix(field.Name, "avg.") || strings.HasPrefix(field.Name, "min.") || strings.HasPrefix(field.Name, "max.") || strings.HasPrefix(field.Name, "quantiles.")) {
					chosen = []string{field.Name}
					break
				}
			}
		}
		if len(chosen) == 0 {
			return SQLResult{}, "", fmt.Errorf("select result fields for this dataset")
		}
	}
	for _, path := range chosen {
		if err := add(path); err != nil {
			return SQLResult{}, "", err
		}
	}
	selectionFields := map[string]bool{}
	for _, path := range paths {
		parts := strings.Split(path, ".")
		for i := range parts {
			selectionFields[strings.Join(parts[:i+1], ".")] = true
		}
	}
	if fieldLimit > 0 && len(selectionFields) > fieldLimit {
		return SQLResult{}, "", fmt.Errorf("%s allows at most %d selected fields; select fewer fields or dimensions", dataset.Name, fieldLimit)
	}
	filters := map[string]graphColumn{}
	for _, field := range dataset.Filters {
		filters[field.Name] = field
	}
	lower, upper := "", ""
	for _, prefix := range []string{"datetime", "date", "timestamp", "datetimeMinute", "datetimeHour"} {
		if _, ok := filters[prefix+"_geq"]; ok {
			if _, ok := filters[prefix+"_leq"]; ok {
				lower = prefix + "_geq"
				upper = prefix + "_leq"
				break
			}
		}
	}
	if lower == "" {
		return SQLResult{}, "", fmt.Errorf("this dataset requires custom time filters; use the GraphQL code editor")
	}
	scope := q.Scope
	if scope == "" {
		scope = "account"
	}
	id := q.AccountID
	if scope == "zone" {
		id = q.ZoneID
	}
	declarations := []string{}
	variables := map[string]any{}
	conditions := []string{}
	bind := func(field graphColumn, value any) {
		name := fmt.Sprintf("filter%d", len(conditions))
		declarations = append(declarations, "$"+name+":"+field.Type)
		variables[name] = value
		conditions = append(conditions, field.Name+":$"+name)
	}
	dateFormat := time.RFC3339
	if strings.HasPrefix(filters[lower].Type, "Date") {
		dateFormat = "2006-01-02"
	}
	bind(filters[lower], dq.TimeRange.From.UTC().Format(dateFormat))
	bind(filters[upper], dq.TimeRange.To.UTC().Format(dateFormat))
	seenFilters := map[string]bool{lower: true, upper: true}
	for _, filter := range q.Filters {
		field, ok := filters[filter.Field]
		if !ok {
			return SQLResult{}, "", fmt.Errorf("unknown GraphQL filter %q", filter.Field)
		}
		if seenFilters[field.Name] {
			return SQLResult{}, "", fmt.Errorf("duplicate GraphQL filter %q", field.Name)
		}
		seenFilters[field.Name] = true
		value, err := graphFilterValue(filter.Value, field.Type)
		if err != nil {
			return SQLResult{}, "", fmt.Errorf("%s: %w", field.Name, err)
		}
		bind(field, value)
	}
	limit := q.rowLimit(d.settings.MaxRows)
	if dataset.Settings.MaxRows > 0 {
		limit = min(limit, dataset.Settings.MaxRows)
	}
	q.Limit = limit
	ordering := ""
	_, bucket, _ := strings.Cut(q.TimeField, ".")
	if bucket == "" {
		bucket = q.TimeField
	}
	for _, order := range dataset.Order {
		if order == bucket+"_ASC" {
			ordering = ",orderBy:[" + order + "]"
			break
		}
	}
	source := "query Cloudflare(" + strings.Join(declarations, ",") + "){viewer{" + scope + "s(filter:{" + scope + "Tag:" + strconv.Quote(id) + "}){" + dataset.Name + "(limit:" + strconv.Itoa(limit) + ",filter:{" + strings.Join(conditions, ",") + "}" + ordering + "){" + graphSelection(paths) + "}}}}"
	q.GraphQL = source
	q.OperationName = "Cloudflare"
	q.DataPath = "viewer." + scope + "s." + dataset.Name
	body, err := d.graphRequest(ctx, source, variables)
	if err != nil {
		return SQLResult{}, source, backend.DownstreamError(err)
	}
	var data any
	if err := decodeJSON(body, &data); err != nil {
		return SQLResult{}, source, err
	}
	rows, err := selectRows(data, q.DataPath)
	result := SQLResult{Data: rows}
	if limitedFields {
		result.Notices = []string{"Default log fields were limited by Cloudflare's field limit. Select fields explicitly to inspect other attributes."}
	}
	return result, source, err
}
