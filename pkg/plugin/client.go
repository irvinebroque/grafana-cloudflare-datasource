package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const maxResponseBytes = 32 << 20

type client struct {
	http    *http.Client
	baseURL string
	token   string
}

func (c *client) request(ctx context.Context, method, path string, body any) ([]byte, error) {
	var payload []byte
	var err error
	if body != nil {
		if sql, ok := body.(string); ok {
			payload = []byte(sql)
		} else {
			payload, err = json.Marshal(body)
		}
		if err != nil {
			return nil, fmt.Errorf("encode Cloudflare request: %w", err)
		}
	}
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "grafana-cloudflare-datasource/0.1.0")
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("Cloudflare request failed: %w", err)
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read Cloudflare response: %w", readErr)
		}
		if len(data) > maxResponseBytes {
			return nil, fmt.Errorf("Cloudflare response exceeds 32 MiB; reduce the time range or row limit")
		}
		if (resp.StatusCode == 429 || resp.StatusCode == 502 || resp.StatusCode == 503 || resp.StatusCode == 504) && attempt < 2 {
			delay := time.Duration(attempt+1) * 200 * time.Millisecond
			if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && seconds > 0 {
				delay = time.Duration(seconds) * time.Second
				if delay > 2*time.Second {
					return nil, c.apiError(resp, data)
				}
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, c.apiError(resp, data)
		}
		var envelope struct {
			Success *bool           `json:"success"`
			Errors  json.RawMessage `json:"errors"`
		}
		if json.Unmarshal(data, &envelope) == nil && envelope.Success != nil && !*envelope.Success {
			return nil, c.apiError(resp, data)
		}
		return data, nil
	}
	return nil, fmt.Errorf("Cloudflare retry limit reached")
}

func (c *client) apiError(resp *http.Response, body []byte) error {
	message := strings.ReplaceAll(string(body), c.token, "[redacted]")
	if len(message) > 2000 {
		message = message[:2000]
	}
	return fmt.Errorf("Cloudflare API HTTP %d (ray %s): %s", resp.StatusCode, resp.Header.Get("CF-Ray"), message)
}
