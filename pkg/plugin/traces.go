package plugin

import (
	"encoding/json"
	"fmt"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"sort"
)

func traceFrames(rows []map[string]any, refID string) (data.Frames, error) {
	ids, spanIDs, parents, services, operations := []string{}, []string{}, []string{}, []string{}, []string{}
	starts, durations := []float64{}, []float64{}
	statuses, messages := []int64{}, []string{}
	tags := []json.RawMessage{}
	serviceTags := []json.RawMessage{}
	for _, row := range rows {
		id := textValue(row["traceId"])
		if id == "" {
			id = textValue(row["traceID"])
		}
		span := textValue(row["spanId"])
		if span == "" {
			span = textValue(row["spanID"])
		}
		if id == "" || span == "" {
			return nil, fmt.Errorf("trace result requires traceId and spanId columns")
		}
		t, ok := timestamp(row["startTime"])
		if !ok || t == nil {
			return nil, fmt.Errorf("trace result requires valid startTime")
		}
		d, ok := number(row["durationMs"])
		if !ok || d == nil || *d < 0 {
			return nil, fmt.Errorf("trace result requires numeric durationMs")
		}
		ids = append(ids, id)
		spanIDs = append(spanIDs, span)
		parents = append(parents, textValue(row["parentSpanId"]))
		services = append(services, textValue(row["serviceName"]))
		operations = append(operations, textValue(row["spanName"]))
		starts = append(starts, float64(t.UnixMicro())/1000)
		durations = append(durations, *d)
		pairs := []map[string]any{}
		if attributes, ok := row["attributes"].(map[string]any); ok {
			names := []string{}
			for key := range attributes {
				names = append(names, key)
			}
			sort.Strings(names)
			for _, key := range names {
				pairs = append(pairs, map[string]any{"key": key, "value": attributes[key]})
			}
		}
		status := int64(0)
		switch textValue(row["spanStatus"]) {
		case "ok":
			status = 1
		case "error":
			status = 2
		}
		errorText := textValue(row["error"])
		if errorText != "" && errorText != "false" {
			status = 2
		}
		if status == 2 {
			pairs = append(pairs, map[string]any{"key": "error", "value": true})
		}
		statuses = append(statuses, status)
		message := textValue(row["spanStatusMessage"])
		if message == "" && status == 2 {
			message = errorText
		}
		messages = append(messages, message)
		encoded, _ := json.Marshal(pairs)
		tags = append(tags, encoded)
		serviceTags = append(serviceTags, json.RawMessage(`[]`))
	}
	frame := data.NewFrame(refID, data.NewField("traceID", nil, ids), data.NewField("spanID", nil, spanIDs), data.NewField("parentSpanID", nil, parents), data.NewField("serviceName", nil, services), data.NewField("operationName", nil, operations), data.NewField("startTime", nil, starts), data.NewField("duration", nil, durations), data.NewField("tags", nil, tags), data.NewField("serviceTags", nil, serviceTags), data.NewField("statusCode", nil, statuses), data.NewField("statusMessage", nil, messages))
	frame.RefID = refID
	frame.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeTrace}
	return data.Frames{frame}, nil
}

// Grafana renders these standard frames alongside its native trace waterfall.
func traceGraph(trace *data.Frame, refID string) data.Frames {
	ids, titles, services := []string{}, []string{}, []string{}
	durations, failures, successes := []float64{}, []float64{}, []float64{}
	seen := map[string]bool{}
	idAt := func(index int) string {
		return trace.Fields[0].At(index).(string) + ":" + trace.Fields[1].At(index).(string)
	}
	for index := 0; index < trace.Rows(); index++ {
		id := idAt(index)
		if seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
		titles = append(titles, trace.Fields[4].At(index).(string))
		services = append(services, trace.Fields[3].At(index).(string))
		durations = append(durations, trace.Fields[6].At(index).(float64))
		failure := 0.0
		if trace.Fields[9].At(index).(int64) == 2 {
			failure = 1
		}
		failures = append(failures, failure)
		successes = append(successes, 1-failure)
	}
	edgeIDs, sources, targets := []string{}, []string{}, []string{}
	edgesSeen := map[string]bool{}
	for index := 0; index < trace.Rows(); index++ {
		parent := trace.Fields[2].At(index).(string)
		source := trace.Fields[0].At(index).(string) + ":" + parent
		target := idAt(index)
		edge := source + ">" + target
		// An upstream parent may be outside the retained Cloudflare trace.
		if parent == "" || !seen[source] || source == target || edgesSeen[edge] {
			continue
		}
		edgesSeen[edge] = true
		edgeIDs = append(edgeIDs, edge)
		sources = append(sources, source)
		targets = append(targets, target)
	}
	nodes := data.NewFrame("nodes", data.NewField("id", nil, ids), data.NewField("title", nil, titles), data.NewField("subTitle", nil, services), data.NewField("mainStat", nil, durations), data.NewField("arc__errors", nil, failures), data.NewField("arc__other", nil, successes))
	nodes.Fields[3].Config = &data.FieldConfig{Unit: "ms"}
	nodes.Fields[4].Config = &data.FieldConfig{Color: map[string]any{"mode": "fixed", "fixedColor": "red"}}
	nodes.Fields[5].Config = &data.FieldConfig{Color: map[string]any{"mode": "fixed", "fixedColor": "gray"}}
	edges := data.NewFrame("edges", data.NewField("id", nil, edgeIDs), data.NewField("source", nil, sources), data.NewField("target", nil, targets))
	for _, frame := range []*data.Frame{nodes, edges} {
		frame.RefID = refID
		frame.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeNodeGraph}
	}
	return data.Frames{nodes, edges}
}
