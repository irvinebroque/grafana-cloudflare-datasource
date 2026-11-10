package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
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
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "grafana-cloudflare-datasource/0.1.1")
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, backend.DownstreamError(fmt.Errorf("Cloudflare request failed: %w", err))
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
		resp.Body.Close()
		if readErr != nil {
			return nil, backend.DownstreamError(fmt.Errorf("read Cloudflare response: %w", readErr))
		}
		if len(data) > maxResponseBytes {
			return nil, backend.DownstreamError(fmt.Errorf("Cloudflare response exceeds 32 MiB; reduce the time range or row limit"))
		}
		if (resp.StatusCode == 429 || resp.StatusCode == 502 || resp.StatusCode == 503 || resp.StatusCode == 504) && attempt < 2 {
			delay := time.Duration(attempt+1) * 200 * time.Millisecond
			retryAfter := resp.Header.Get("Retry-After")
			if seconds, err := strconv.ParseInt(retryAfter, 10, 64); err == nil && seconds >= 0 {
				if seconds > 2 {
					return nil, c.apiError(resp, data)
				}
				delay = time.Duration(seconds) * time.Second
			} else if until, err := http.ParseTime(retryAfter); err == nil {
				delay = time.Until(until)
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
}

func (c *client) apiError(resp *http.Response, body []byte) error {
	message := strings.ReplaceAll(string(body), c.token, "[redacted]")
	if len(message) > 2000 {
		message = message[:2000]
	}
	status := resp.StatusCode
	if status < 400 {
		status = http.StatusBadGateway
	}
	return backend.DownstreamError(&cloudflareAPIError{status: backend.Status(status), message: fmt.Sprintf("Cloudflare API HTTP %d (ray %s): %s", resp.StatusCode, resp.Header.Get("CF-Ray"), message)})
}

type cloudflareAPIError struct {
	status  backend.Status
	message string
}

func (e *cloudflareAPIError) Error() string { return e.message }

func queryError(err error) backend.DataResponse {
	response := backend.DataResponse{Error: err, Status: backend.StatusBadRequest}
	var api *cloudflareAPIError
	var network net.Error
	switch {
	case errors.As(err, &api):
		response.Status = api.status
		response.ErrorSource = backend.ErrorSourceDownstream
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded), errors.As(err, &network) && network.Timeout():
		response.Status = backend.StatusTimeout
		response.ErrorSource = backend.ErrorSourceDownstream
	case backend.IsDownstreamError(err):
		response.Status = backend.StatusBadGateway
		response.ErrorSource = backend.ErrorSourceDownstream
	}
	return response
}
