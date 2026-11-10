package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

func decodeJSON(body []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("expected a single JSON value")
	}
	return nil
}

func decodeSQL(body []byte) (SQLResult, error) {
	var result SQLResult
	if err := decodeJSON(body, &result); err != nil {
		return result, fmt.Errorf("expected Cloudflare JSON query output (use FORMAT JSON): %w", err)
	}
	if result.Data == nil {
		return result, fmt.Errorf("Cloudflare returned no data array")
	}
	for _, row := range result.Data {
		if row == nil {
			return result, fmt.Errorf("Cloudflare returned a null row instead of an object")
		}
	}
	return result, nil
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

func selectRows(value any, path string) ([]map[string]any, error) {
	segments := []string{}
	if path != "" {
		segments = strings.Split(path, ".")
	}
	rows := []map[string]any{}
	var walk func(any, []string) error
	walk = func(current any, remaining []string) error {
		if array, ok := current.([]any); ok {
			for _, child := range array {
				if err := walk(child, remaining); err != nil {
					return err
				}
			}
			return nil
		}
		if len(remaining) > 0 {
			object, ok := current.(map[string]any)
			if !ok {
				return fmt.Errorf("data path %q does not select objects", path)
			}
			child, ok := object[remaining[0]]
			if !ok {
				return fmt.Errorf("data path %q: field %q was not returned", path, remaining[0])
			}
			return walk(child, remaining[1:])
		}
		object, ok := current.(map[string]any)
		if !ok {
			if current == nil {
				return nil
			}
			return fmt.Errorf("data path %q must select an object or array", path)
		}
		row := map[string]any{}
		flattenRow("", object, row)
		rows = append(rows, row)
		return nil
	}
	if err := walk(value, segments); err != nil {
		return nil, err
	}
	return rows, nil
}

func flattenRow(prefix string, object map[string]any, row map[string]any) {
	for key, value := range object {
		name := key
		if prefix != "" {
			name = prefix + "." + key
		}
		if child, ok := value.(map[string]any); ok && key != "attributes" {
			flattenRow(name, child, row)
		} else {
			row[name] = value
		}
	}
}
