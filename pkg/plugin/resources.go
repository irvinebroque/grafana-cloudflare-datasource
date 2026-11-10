package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

func (c *client) listResource(ctx context.Context, path string) ([]byte, error) {
	parsed, err := url.Parse(path)
	if err != nil {
		return nil, err
	}
	params := parsed.Query()
	items := []json.RawMessage{}
	size := 0
	for page := 1; ; page++ {
		params.Set("page", strconv.Itoa(page))
		parsed.RawQuery = params.Encode()
		body, err := c.request(ctx, http.MethodGet, parsed.String(), nil)
		if err != nil {
			return nil, err
		}
		var response struct {
			Result []json.RawMessage `json:"result"`
			Info   struct {
				Page       int `json:"page"`
				TotalPages int `json:"total_pages"`
				TotalCount int `json:"total_count"`
				PerPage    int `json:"per_page"`
			} `json:"result_info"`
		}
		if err := decodeJSON(body, &response); err != nil {
			return nil, fmt.Errorf("invalid Cloudflare resource list: %w", err)
		}
		if response.Result == nil || (response.Info.Page != 0 && response.Info.Page != page) {
			return nil, fmt.Errorf("Cloudflare returned an invalid resource page")
		}
		size += len(body)
		perPage, _ := strconv.Atoi(params.Get("per_page"))
		if response.Info.PerPage > 0 {
			perPage = response.Info.PerPage
		}
		if perPage < 1 {
			perPage = 50
		}
		totalPages := response.Info.TotalPages
		if totalPages == 0 && response.Info.TotalCount > 0 {
			totalPages = (response.Info.TotalCount-1)/perPage + 1
		}
		if size > maxResponseBytes || totalPages > 2000 || page > 2000 {
			return nil, fmt.Errorf("Cloudflare resource list exceeds the safety limit; enter the resource ID manually")
		}
		items = append(items, response.Result...)
		if (totalPages > 0 && page >= totalPages) || (totalPages == 0 && len(response.Result) < perPage) {
			break
		}
		if len(response.Result) == 0 {
			return nil, fmt.Errorf("Cloudflare returned an empty page before the end of the resource list")
		}
	}
	return json.Marshal(struct {
		Success bool              `json:"success"`
		Result  []json.RawMessage `json:"result"`
	}{Success: true, Result: items})
}
