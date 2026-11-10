package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

type graphRef struct {
	Kind   string    `json:"kind"`
	Name   string    `json:"name"`
	OfType *graphRef `json:"ofType"`
}

func (r graphRef) named() string {
	if r.Name != "" {
		return r.Name
	}
	if r.OfType != nil {
		return r.OfType.named()
	}
	return ""
}
func (r graphRef) declaration() string {
	if r.Kind == "NON_NULL" && r.OfType != nil {
		return r.OfType.declaration() + "!"
	}
	if r.Kind == "LIST" && r.OfType != nil {
		return "[" + r.OfType.declaration() + "]"
	}
	return r.Name
}

type graphField struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Type        graphRef     `json:"type"`
	Args        []graphField `json:"args"`
	Deprecated  bool         `json:"isDeprecated"`
}
type graphType struct {
	Name   string       `json:"name"`
	Kind   string       `json:"kind"`
	Fields []graphField `json:"fields"`
	Inputs []graphField `json:"inputFields"`
	Enums  []struct {
		Name string `json:"name"`
	} `json:"enumValues"`
}
type graphColumn struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Numeric     bool   `json:"numeric"`
}
type graphSettings struct {
	MaxFields   int      `json:"maxNumberOfFields"`
	Enabled     bool     `json:"enabled"`
	Fields      []string `json:"availableFields"`
	MaxDuration int64    `json:"maxDuration"`
	MaxRows     int      `json:"maxPageSize"`
	Retention   int64    `json:"notOlderThan"`
}
type graphDataset struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Fields      []graphColumn  `json:"fields"`
	Filters     []graphColumn  `json:"filters"`
	Order       []string       `json:"orderBy"`
	Settings    *graphSettings `json:"settings,omitempty"`
}

func (d *Datasource) graphRequest(ctx context.Context, query string, variables map[string]any) (json.RawMessage, error) {
	body, err := d.client.request(ctx, http.MethodPost, "/graphql", map[string]any{"query": query, "variables": variables})
	if err != nil {
		return nil, err
	}
	var result struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := decodeJSON(body, &result); err != nil {
		return nil, err
	}
	if len(result.Errors) > 0 {
		return nil, &cloudflareAPIError{status: backend.StatusBadRequest, message: "Cloudflare GraphQL: " + result.Errors[0].Message}
	}
	if len(result.Data) == 0 || string(result.Data) == "null" {
		return nil, fmt.Errorf("Cloudflare GraphQL returned no data")
	}
	return result.Data, nil
}

func (d *Datasource) graphSchema(ctx context.Context) (map[string]graphType, error) {
	d.schemaMu.Lock()
	defer d.schemaMu.Unlock()
	const key = "graphql-schema"
	d.mu.Lock()
	entry, exists := d.cache[key]
	d.mu.Unlock()
	body := entry.Body
	if !exists || entry.Expires <= time.Now().Unix() {
		ref := "kind name ofType {kind name ofType {kind name ofType {kind name ofType {kind name}}}}"
		query := "{__schema{types{name kind fields{name description isDeprecated type{" + ref + "} args{name type{" + ref + "}}} inputFields{name description type{" + ref + "}} enumValues{name}}}}"
		var err error
		body, err = d.graphRequest(ctx, query, nil)
		if err != nil {
			return nil, err
		}
		d.cacheBody(key, body)
	}
	var result struct {
		Schema struct {
			Types []graphType `json:"types"`
		} `json:"__schema"`
	}
	if err := decodeJSON(body, &result); err != nil {
		return nil, err
	}
	if len(result.Schema.Types) == 0 {
		return nil, fmt.Errorf("Cloudflare returned an empty GraphQL schema")
	}
	types := map[string]graphType{}
	for _, t := range result.Schema.Types {
		types[t.Name] = t
	}
	return types, nil
}

func graphFields(types map[string]graphType, name, prefix string, depth int) []graphColumn {
	columns := []graphColumn{}
	if depth > 3 {
		return columns
	}
	for _, field := range types[name].Fields {
		if field.Deprecated || len(field.Args) > 0 {
			continue
		}
		target := types[field.Type.named()]
		path := prefix + field.Name
		if target.Kind == "SCALAR" || target.Kind == "ENUM" {
			lower := strings.ToLower(target.Name)
			numeric := strings.Contains(lower, "int") || strings.HasPrefix(lower, "float") || strings.HasPrefix(lower, "decimal")
			columns = append(columns, graphColumn{Name: path, Type: field.Type.declaration(), Description: field.Description, Numeric: numeric && !strings.HasPrefix(field.Type.declaration(), "[")})
		} else if target.Kind == "OBJECT" {
			columns = append(columns, graphFields(types, target.Name, path+".", depth+1)...)
		}
	}
	return columns
}

func graphDatasets(types map[string]graphType, scope string) []graphDataset {
	datasets := []graphDataset{}
	for _, node := range types[scope].Fields {
		lower := strings.ToLower(node.Name + " " + node.Description)
		if node.Deprecated || strings.Contains(lower, "staging") {
			continue
		}
		dataset := graphDataset{Name: node.Name, Description: node.Description, Fields: graphFields(types, node.Type.named(), "", 0), Filters: []graphColumn{}, Order: []string{}}
		hasLimit := false
		for _, arg := range node.Args {
			if arg.Name == "limit" {
				hasLimit = true
			}
			target := types[arg.Type.named()]
			if arg.Name == "filter" {
				for _, field := range target.Inputs {
					base := types[field.Type.named()]
					if base.Kind == "SCALAR" || base.Kind == "ENUM" {
						dataset.Filters = append(dataset.Filters, graphColumn{Name: field.Name, Type: field.Type.declaration(), Description: field.Description})
					}
				}
			}
			if arg.Name == "orderBy" {
				for _, value := range target.Enums {
					dataset.Order = append(dataset.Order, value.Name)
				}
			}
		}
		if hasLimit && len(dataset.Fields) > 0 {
			datasets = append(datasets, dataset)
		}
	}
	sort.Slice(datasets, func(i, j int) bool { return datasets[i].Name < datasets[j].Name })
	return datasets
}

func (d *Datasource) graphAvailability(ctx context.Context, scope, id, name string) (*graphSettings, error) {
	key := "graphql-settings:" + scope + ":" + id + ":" + name
	d.mu.Lock()
	entry, exists := d.cache[key]
	d.mu.Unlock()
	body := entry.Body
	if !exists || entry.Expires <= time.Now().Unix() {
		query := "{viewer{" + scope + "s(filter:{" + scope + "Tag:" + fmt.Sprintf("%q", id) + "}){settings{" + name + "{enabled availableFields maxDuration maxPageSize maxNumberOfFields notOlderThan}}}}}"
		var err error
		body, err = d.graphRequest(ctx, query, nil)
		if err != nil {
			return nil, err
		}
		d.cacheBody(key, body)
	}
	var envelope struct {
		Viewer map[string][]struct {
			Settings map[string]graphSettings `json:"settings"`
		} `json:"viewer"`
	}
	if err := decodeJSON(body, &envelope); err != nil {
		return nil, err
	}
	items := envelope.Viewer[scope+"s"]
	if len(items) == 0 {
		return nil, fmt.Errorf("no GraphQL %s is accessible; check the ID and token permissions", scope)
	}
	settings, ok := items[0].Settings[name]
	if !ok {
		return nil, fmt.Errorf("Cloudflare returned no availability settings for %s", name)
	}
	return &settings, nil
}

func (d *Datasource) graphCatalog(ctx context.Context, q Query) ([]graphDataset, error) {
	types, err := d.graphSchema(ctx)
	if err != nil {
		return nil, err
	}
	scope := q.Scope
	if scope == "" {
		scope = "account"
	}
	datasets := graphDatasets(types, scope)
	if q.Dataset == "" {
		return datasets, nil
	}
	for _, dataset := range datasets {
		if dataset.Name != q.Dataset {
			continue
		}
		id := q.AccountID
		if scope == "zone" {
			id = q.ZoneID
		}
		dataset.Settings, err = d.graphAvailability(ctx, scope, id, dataset.Name)
		if err == nil && len(dataset.Settings.Fields) > 0 {
			available := map[string]bool{}
			for _, name := range dataset.Settings.Fields {
				available[name] = true
			}
			fields := []graphColumn{}
			for _, field := range dataset.Fields {
				if available[strings.ReplaceAll(field.Name, ".", "_")] || available[strings.Split(field.Name, ".")[0]] {
					fields = append(fields, field)
				}
			}
			dataset.Fields = fields
		}
		return []graphDataset{dataset}, err
	}
	return nil, fmt.Errorf("select a GraphQL dataset from the current %s schema", scope)
}
