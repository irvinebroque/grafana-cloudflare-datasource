package plugin

import "encoding/json"

type Settings struct {
	AccountID      string `json:"accountId"`
	ZoneID         string `json:"zoneId"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
	MaxRows        int    `json:"maxRows"`
}

type Query struct {
	LogVolume    bool     `json:"logVolume"`
	VolumeSource *Query   `json:"volumeSource"`
	LogOrder     string   `json:"logOrder"`
	QueryType    string   `json:"queryType"`
	Format       string   `json:"format"`
	AccountID    string   `json:"accountId"`
	ZoneID       string   `json:"zoneId"`
	Scope        string   `json:"scope"`
	Dataset      string   `json:"dataset"`
	SQL          string   `json:"sql"`
	Params       string   `json:"params"`
	Metrics      []Metric `json:"metrics"`
	GroupBy      []string `json:"groupBy"`
	Filters      []Filter `json:"filters"`
	Columns      []string `json:"columns"`
	Limit        int      `json:"limit"`
	TimeField    string   `json:"timeField"`
	TimeSeries   bool     `json:"timeSeries"`
	Sampled      *bool    `json:"sampled"`
	TraceID      string   `json:"traceId"`
	GraphQL      string   `json:"graphql"`
	Variables    string   `json:"variables"`
	DataPath     string   `json:"dataPath"`
	Telemetry    string   `json:"telemetry"`
	Hide         bool     `json:"hide"`
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

type Column struct {
	Name        string `json:"name"`
	DataType    string `json:"data_type"`
	Description string `json:"description"`
}

type SQLResult struct {
	Data []map[string]any `json:"data"`
	Meta []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"meta"`
	Rows       int             `json:"rows"`
	Statistics json.RawMessage `json:"statistics"`
}

type cacheEntry struct {
	Body    []byte
	Expires int64
}
