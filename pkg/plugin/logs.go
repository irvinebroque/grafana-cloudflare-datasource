package plugin

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Other Cloudflare and Log Explorer datasets use their own timestamp and event schema.
func (d *Datasource) prepareLogs(ctx context.Context, q *Query) error {
	if q.QueryType != "builder" && q.QueryType != "" || q.Format != "logs" && !q.LogVolume || q.Dataset == "logs.workersLogs" || q.Dataset == "logs.containerLogs" {
		return nil
	}
	params := url.Values{"account_tag": {q.AccountID}, "dataset_name": {q.Dataset}, "include_columns": {"true"}}
	path := "/analytics/sql/introspection?" + params.Encode()
	d.mu.Lock()
	entry, exists := d.cache[path]
	d.mu.Unlock()
	body := entry.Body
	if !exists || entry.Expires <= time.Now().Unix() {
		var err error
		body, err = d.client.request(ctx, http.MethodGet, path, nil)
		if err != nil {
			return err
		}
		d.cacheBody(path, body)
	}
	var catalog struct {
		Datasets []struct {
			Name    string `json:"name"`
			Columns []struct {
				Name string `json:"name"`
				Type string `json:"data_type"`
			} `json:"columns"`
		} `json:"datasets"`
	}
	if err := decodeJSON(body, &catalog); err != nil {
		return err
	}
	for _, dataset := range catalog.Datasets {
		if dataset.Name != q.Dataset {
			continue
		}
		timeField := ""
		configured := false
		for _, column := range dataset.Columns {
			if strings.HasPrefix(columnType(column.Type), "Date") && (timeField == "" || column.Name == "timestamp") {
				timeField = column.Name
			}
			if column.Name == q.TimeField {
				configured = true
			}
		}
		if !configured {
			if q.TimeField != "" && q.TimeField != "timestamp" {
				return fmt.Errorf("timestamp field %q is not in this dataset", q.TimeField)
			}
			q.TimeField = timeField
		}
		if q.TimeField == "" {
			return fmt.Errorf("select a timestamp field for this log dataset")
		}
		if q.Format == "logs" && len(q.Columns) == 0 {
			for _, column := range dataset.Columns {
				q.Columns = append(q.Columns, column.Name)
			}
		}
		return nil
	}
	return fmt.Errorf("log dataset %q is not in this account's catalog", q.Dataset)
}

func logRows(rows []map[string]any) []map[string]any {
	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if _, exists := row["message"]; exists {
			result = append(result, row)
			continue
		}
		copy := make(map[string]any, len(row)+1)
		for key, value := range row {
			copy[key] = value
		}
		message := ""
		for _, key := range []string{"line", "body", "errorMessage", "errorTitle", "eventType", "action", "url"} {
			if value := textValue(row[key]); value != "" {
				message = value
				break
			}
		}
		if message == "" {
			message = textValue(row)
		}
		copy["message"] = message
		result = append(result, copy)
	}
	return result
}
