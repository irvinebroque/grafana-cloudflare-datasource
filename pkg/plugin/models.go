package plugin

import "encoding/json"

type Settings struct {
	AccountID      string `json:"accountId"`
	ZoneID         string `json:"zoneId"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
	MaxRows        int    `json:"maxRows"`
}

type Query struct {
	LogVolume     bool            `json:"logVolume"`
	VolumeSource  json.RawMessage `json:"volumeSource"`
	LogOrder      string          `json:"logOrder"`
	QueryType     string          `json:"queryType"`
	Format        string          `json:"format"`
	AccountID     string          `json:"accountId"`
	ZoneID        string          `json:"zoneId"`
	Scope         string          `json:"scope"`
	Dataset       string          `json:"dataset"`
	SQL           string          `json:"sql"`
	AnalyticsSQL  string          `json:"analyticsSql"`
	Params        string          `json:"params"`
	Metrics       []Metric        `json:"metrics"`
	GroupBy       []string        `json:"groupBy"`
	Filters       []Filter        `json:"filters"`
	Columns       []string        `json:"columns"`
	Limit         int             `json:"limit"`
	TimeField     string          `json:"timeField"`
	Sampled       *bool           `json:"sampled"`
	TraceID       string          `json:"traceId"`
	GraphQL       string          `json:"graphql"`
	OperationName string          `json:"operationName"`
	Variables     string          `json:"variables"`
	DataPath      string          `json:"dataPath"`
	Telemetry     string          `json:"telemetry"`
	Hide          bool            `json:"hide"`
}

func (q Query) rowLimit(maxRows int) int {
	limit := q.Limit
	if limit <= 0 {
		limit = 1000
	}
	return min(limit, maxRows)
}

type Metric struct {
	Aggregation string `json:"aggregation"`
	Field       string `json:"field"`
	Alias       string `json:"alias"`
}

type Filter struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

type SQLResult struct {
	Data       []map[string]any `json:"data"`
	Meta       []resultColumn   `json:"meta"`
	Statistics json.RawMessage  `json:"statistics"`
}

type resultColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type cacheEntry struct {
	Body    []byte
	Expires int64
}
