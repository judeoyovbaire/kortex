/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	gatewayv1alpha1 "github.com/judeoyovbaire/inference-gateway/api/v1alpha1"
)

// CostStats contains aggregated cost statistics
type CostStats struct {
	TotalCost         float64   `json:"totalCost"`
	TotalRequests     int64     `json:"totalRequests"`
	TotalInputTokens  int64     `json:"totalInputTokens"`
	TotalOutputTokens int64     `json:"totalOutputTokens"`
	Currency          string    `json:"currency"`
	LastUpdated       time.Time `json:"lastUpdated"`
}

// TokenUsage represents token usage from an API response
type TokenUsage struct {
	InputTokens  int64
	OutputTokens int64
}

// CostTracker tracks costs per route and backend
type CostTracker struct {
	mu           sync.RWMutex
	routeCosts   map[string]*CostStats
	backendCosts map[string]*CostStats
	metrics      *MetricsRecorder
}

// NewCostTracker creates a new cost tracker
func NewCostTracker(metrics *MetricsRecorder) *CostTracker {
	return &CostTracker{
		routeCosts:   make(map[string]*CostStats),
		backendCosts: make(map[string]*CostStats),
		metrics:      metrics,
	}
}

// TrackRequest records cost for a request
func (c *CostTracker) TrackRequest(
	route, backend string,
	usage TokenUsage,
	costConfig *gatewayv1alpha1.CostConfig,
) {
	if costConfig == nil {
		return
	}

	// Calculate cost
	cost := c.calculateCost(usage, costConfig)
	currency := costConfig.Currency
	if currency == "" {
		currency = "USD"
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()

	// Update route costs
	c.updateStats(c.routeCosts, route, usage, cost, currency, now)

	// Update backend costs
	c.updateStats(c.backendCosts, backend, usage, cost, currency, now)

	// Record in metrics
	if c.metrics != nil {
		c.metrics.RecordCost(route, backend, cost)
		c.metrics.RecordTokens(route, backend, usage.InputTokens, usage.OutputTokens)
	}
}

// updateStats updates cost statistics for a given key
func (c *CostTracker) updateStats(
	stats map[string]*CostStats,
	key string,
	usage TokenUsage,
	cost float64,
	currency string,
	timestamp time.Time,
) {
	s, exists := stats[key]
	if !exists {
		s = &CostStats{Currency: currency}
		stats[key] = s
	}

	s.TotalCost += cost
	s.TotalRequests++
	s.TotalInputTokens += usage.InputTokens
	s.TotalOutputTokens += usage.OutputTokens
	s.LastUpdated = timestamp
}

// calculateCost computes the cost based on token usage and config
func (c *CostTracker) calculateCost(usage TokenUsage, config *gatewayv1alpha1.CostConfig) float64 {
	var cost float64

	// Input token cost (per 1000 tokens)
	if config.InputTokenCost != "" {
		inputCostPer1K, err := strconv.ParseFloat(config.InputTokenCost, 64)
		if err == nil {
			cost += (float64(usage.InputTokens) / 1000.0) * inputCostPer1K
		}
	}

	// Output token cost (per 1000 tokens)
	if config.OutputTokenCost != "" {
		outputCostPer1K, err := strconv.ParseFloat(config.OutputTokenCost, 64)
		if err == nil {
			cost += (float64(usage.OutputTokens) / 1000.0) * outputCostPer1K
		}
	}

	// Fixed request cost
	if config.RequestCost != "" {
		requestCost, err := strconv.ParseFloat(config.RequestCost, 64)
		if err == nil {
			cost += requestCost
		}
	}

	return cost
}

// GetRouteCosts returns cost statistics for a route
func (c *CostTracker) GetRouteCosts(route string) *CostStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if stats, exists := c.routeCosts[route]; exists {
		// Return a copy
		return &CostStats{
			TotalCost:         stats.TotalCost,
			TotalRequests:     stats.TotalRequests,
			TotalInputTokens:  stats.TotalInputTokens,
			TotalOutputTokens: stats.TotalOutputTokens,
			Currency:          stats.Currency,
			LastUpdated:       stats.LastUpdated,
		}
	}
	return nil
}

// GetBackendCosts returns cost statistics for a backend
func (c *CostTracker) GetBackendCosts(backend string) *CostStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if stats, exists := c.backendCosts[backend]; exists {
		// Return a copy
		return &CostStats{
			TotalCost:         stats.TotalCost,
			TotalRequests:     stats.TotalRequests,
			TotalInputTokens:  stats.TotalInputTokens,
			TotalOutputTokens: stats.TotalOutputTokens,
			Currency:          stats.Currency,
			LastUpdated:       stats.LastUpdated,
		}
	}
	return nil
}

// GetAllStats returns all cost statistics
func (c *CostTracker) GetAllStats() (routes, backends map[string]*CostStats) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	routes = make(map[string]*CostStats, len(c.routeCosts))
	for k, v := range c.routeCosts {
		routes[k] = &CostStats{
			TotalCost:         v.TotalCost,
			TotalRequests:     v.TotalRequests,
			TotalInputTokens:  v.TotalInputTokens,
			TotalOutputTokens: v.TotalOutputTokens,
			Currency:          v.Currency,
			LastUpdated:       v.LastUpdated,
		}
	}

	backends = make(map[string]*CostStats, len(c.backendCosts))
	for k, v := range c.backendCosts {
		backends[k] = &CostStats{
			TotalCost:         v.TotalCost,
			TotalRequests:     v.TotalRequests,
			TotalInputTokens:  v.TotalInputTokens,
			TotalOutputTokens: v.TotalOutputTokens,
			Currency:          v.Currency,
			LastUpdated:       v.LastUpdated,
		}
	}

	return routes, backends
}

// Reset clears all cost statistics
func (c *CostTracker) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.routeCosts = make(map[string]*CostStats)
	c.backendCosts = make(map[string]*CostStats)
}

// ParseTokenUsage extracts token usage from an API response based on provider
func ParseTokenUsage(provider string, resp *http.Response, body []byte) TokenUsage {
	switch provider {
	case "openai", "":
		return parseOpenAIUsage(body)
	case "anthropic":
		return parseAnthropicUsage(resp, body)
	case "cohere":
		return parseCohereUsage(body)
	default:
		return TokenUsage{}
	}
}

// parseOpenAIUsage extracts token usage from OpenAI response
func parseOpenAIUsage(body []byte) TokenUsage {
	// OpenAI response format:
	// {"usage": {"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30}}
	var response struct {
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return TokenUsage{}
	}

	return TokenUsage{
		InputTokens:  response.Usage.PromptTokens,
		OutputTokens: response.Usage.CompletionTokens,
	}
}

// parseAnthropicUsage extracts token usage from Anthropic response
func parseAnthropicUsage(resp *http.Response, body []byte) TokenUsage {
	// Try headers first (newer API versions)
	inputStr := resp.Header.Get("X-Usage-Input-Tokens")
	outputStr := resp.Header.Get("X-Usage-Output-Tokens")

	if inputStr != "" && outputStr != "" {
		input, _ := strconv.ParseInt(inputStr, 10, 64)
		output, _ := strconv.ParseInt(outputStr, 10, 64)
		return TokenUsage{
			InputTokens:  input,
			OutputTokens: output,
		}
	}

	// Fall back to response body
	// Anthropic response format:
	// {"usage": {"input_tokens": 10, "output_tokens": 20}}
	var response struct {
		Usage struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return TokenUsage{}
	}

	return TokenUsage{
		InputTokens:  response.Usage.InputTokens,
		OutputTokens: response.Usage.OutputTokens,
	}
}

// parseCohereUsage extracts token usage from Cohere response
func parseCohereUsage(body []byte) TokenUsage {
	// Cohere response format:
	// {"meta": {"billed_units": {"input_tokens": 10, "output_tokens": 20}}}
	var response struct {
		Meta struct {
			BilledUnits struct {
				InputTokens  int64 `json:"input_tokens"`
				OutputTokens int64 `json:"output_tokens"`
			} `json:"billed_units"`
		} `json:"meta"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return TokenUsage{}
	}

	return TokenUsage{
		InputTokens:  response.Meta.BilledUnits.InputTokens,
		OutputTokens: response.Meta.BilledUnits.OutputTokens,
	}
}

// CaptureResponseBody creates a response body capturer for token parsing
// This allows reading the response body for token counting while still forwarding it
type ResponseBodyCapturer struct {
	body   *bytes.Buffer
	reader io.ReadCloser
}

// NewResponseBodyCapturer creates a new response body capturer
func NewResponseBodyCapturer(body io.ReadCloser) *ResponseBodyCapturer {
	return &ResponseBodyCapturer{
		body:   &bytes.Buffer{},
		reader: body,
	}
}

// Read implements io.Reader
func (r *ResponseBodyCapturer) Read(p []byte) (n int, err error) {
	n, err = r.reader.Read(p)
	if n > 0 {
		r.body.Write(p[:n])
	}
	return n, err
}

// Close implements io.Closer
func (r *ResponseBodyCapturer) Close() error {
	return r.reader.Close()
}

// Bytes returns the captured body bytes
func (r *ResponseBodyCapturer) Bytes() []byte {
	return r.body.Bytes()
}
