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
	"net/http"
	"testing"

	gatewayv1alpha1 "github.com/judeoyovbaire/inference-gateway/api/v1alpha1"
)

func TestCostTracker_TrackRequest_NilConfig(t *testing.T) {
	ct := NewCostTracker(nil)

	// Should not panic
	ct.TrackRequest("route1", "backend1", TokenUsage{InputTokens: 100, OutputTokens: 50}, nil)

	stats := ct.GetRouteCosts("route1")
	if stats != nil {
		t.Error("expected nil stats for nil config")
	}
}

func TestCostTracker_TrackRequest_BasicCost(t *testing.T) {
	ct := NewCostTracker(nil)
	config := &gatewayv1alpha1.CostConfig{
		InputTokenCost:  "0.01", // $0.01 per 1K input tokens
		OutputTokenCost: "0.03", // $0.03 per 1K output tokens
		Currency:        "USD",
	}

	usage := TokenUsage{
		InputTokens:  1000,
		OutputTokens: 500,
	}

	ct.TrackRequest("route1", "backend1", usage, config)

	routeStats := ct.GetRouteCosts("route1")
	if routeStats == nil {
		t.Fatal("expected route stats")
	}

	// Expected cost: (1000/1000)*0.01 + (500/1000)*0.03 = 0.01 + 0.015 = 0.025
	expectedCost := 0.025
	if routeStats.TotalCost != expectedCost {
		t.Errorf("expected cost %.4f, got %.4f", expectedCost, routeStats.TotalCost)
	}
	if routeStats.TotalRequests != 1 {
		t.Errorf("expected 1 request, got %d", routeStats.TotalRequests)
	}
	if routeStats.TotalInputTokens != 1000 {
		t.Errorf("expected 1000 input tokens, got %d", routeStats.TotalInputTokens)
	}
	if routeStats.TotalOutputTokens != 500 {
		t.Errorf("expected 500 output tokens, got %d", routeStats.TotalOutputTokens)
	}
	if routeStats.Currency != "USD" {
		t.Errorf("expected USD currency, got %s", routeStats.Currency)
	}
}

func TestCostTracker_TrackRequest_WithRequestCost(t *testing.T) {
	ct := NewCostTracker(nil)
	config := &gatewayv1alpha1.CostConfig{
		InputTokenCost:  "0.01",
		OutputTokenCost: "0.03",
		RequestCost:     "0.001", // Fixed $0.001 per request
		Currency:        "USD",
	}

	usage := TokenUsage{
		InputTokens:  1000,
		OutputTokens: 500,
	}

	ct.TrackRequest("route1", "backend1", usage, config)

	routeStats := ct.GetRouteCosts("route1")
	// Expected cost: 0.025 (tokens) + 0.001 (request) = 0.026
	expectedCost := 0.026
	// Use tolerance for floating point comparison
	diff := routeStats.TotalCost - expectedCost
	if diff < -0.0001 || diff > 0.0001 {
		t.Errorf("expected cost %.4f, got %.4f", expectedCost, routeStats.TotalCost)
	}
}

func TestCostTracker_TrackRequest_Aggregation(t *testing.T) {
	ct := NewCostTracker(nil)
	config := &gatewayv1alpha1.CostConfig{
		InputTokenCost:  "0.01",
		OutputTokenCost: "0.01",
		Currency:        "USD",
	}

	// Track multiple requests
	for i := 0; i < 5; i++ {
		ct.TrackRequest("route1", "backend1", TokenUsage{InputTokens: 100, OutputTokens: 100}, config)
	}

	routeStats := ct.GetRouteCosts("route1")
	if routeStats.TotalRequests != 5 {
		t.Errorf("expected 5 requests, got %d", routeStats.TotalRequests)
	}
	if routeStats.TotalInputTokens != 500 {
		t.Errorf("expected 500 input tokens, got %d", routeStats.TotalInputTokens)
	}
	if routeStats.TotalOutputTokens != 500 {
		t.Errorf("expected 500 output tokens, got %d", routeStats.TotalOutputTokens)
	}
}

func TestCostTracker_TrackRequest_SeparateBackendStats(t *testing.T) {
	ct := NewCostTracker(nil)
	config := &gatewayv1alpha1.CostConfig{
		InputTokenCost:  "0.01",
		OutputTokenCost: "0.01",
		Currency:        "USD",
	}

	ct.TrackRequest("route1", "backend1", TokenUsage{InputTokens: 100, OutputTokens: 100}, config)
	ct.TrackRequest("route1", "backend2", TokenUsage{InputTokens: 200, OutputTokens: 200}, config)

	backend1Stats := ct.GetBackendCosts("backend1")
	backend2Stats := ct.GetBackendCosts("backend2")

	if backend1Stats.TotalInputTokens != 100 {
		t.Errorf("backend1: expected 100 input tokens, got %d", backend1Stats.TotalInputTokens)
	}
	if backend2Stats.TotalInputTokens != 200 {
		t.Errorf("backend2: expected 200 input tokens, got %d", backend2Stats.TotalInputTokens)
	}
}

func TestCostTracker_GetAllStats(t *testing.T) {
	ct := NewCostTracker(nil)
	config := &gatewayv1alpha1.CostConfig{
		InputTokenCost: "0.01",
		Currency:       "USD",
	}

	ct.TrackRequest("route1", "backend1", TokenUsage{InputTokens: 100}, config)
	ct.TrackRequest("route2", "backend2", TokenUsage{InputTokens: 200}, config)

	routes, backends := ct.GetAllStats()

	if len(routes) != 2 {
		t.Errorf("expected 2 routes, got %d", len(routes))
	}
	if len(backends) != 2 {
		t.Errorf("expected 2 backends, got %d", len(backends))
	}
}

func TestCostTracker_Reset(t *testing.T) {
	ct := NewCostTracker(nil)
	config := &gatewayv1alpha1.CostConfig{
		InputTokenCost: "0.01",
		Currency:       "USD",
	}

	ct.TrackRequest("route1", "backend1", TokenUsage{InputTokens: 100}, config)
	ct.Reset()

	stats := ct.GetRouteCosts("route1")
	if stats != nil {
		t.Error("expected nil stats after reset")
	}
}

func TestCostTracker_DefaultCurrency(t *testing.T) {
	ct := NewCostTracker(nil)
	config := &gatewayv1alpha1.CostConfig{
		InputTokenCost: "0.01",
		// No currency specified
	}

	ct.TrackRequest("route1", "backend1", TokenUsage{InputTokens: 100}, config)

	stats := ct.GetRouteCosts("route1")
	if stats.Currency != "USD" {
		t.Errorf("expected default USD currency, got %s", stats.Currency)
	}
}

func TestParseTokenUsage_OpenAI(t *testing.T) {
	body := []byte(`{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"usage": {
			"prompt_tokens": 100,
			"completion_tokens": 50,
			"total_tokens": 150
		}
	}`)

	usage := ParseTokenUsage("openai", nil, body)

	if usage.InputTokens != 100 {
		t.Errorf("expected 100 input tokens, got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 50 {
		t.Errorf("expected 50 output tokens, got %d", usage.OutputTokens)
	}
}

func TestParseTokenUsage_OpenAI_EmptyProvider(t *testing.T) {
	body := []byte(`{"usage": {"prompt_tokens": 100, "completion_tokens": 50}}`)

	// Empty provider should default to OpenAI
	usage := ParseTokenUsage("", nil, body)

	if usage.InputTokens != 100 {
		t.Errorf("expected 100 input tokens, got %d", usage.InputTokens)
	}
}

func TestParseTokenUsage_Anthropic_Headers(t *testing.T) {
	resp := &http.Response{
		Header: make(http.Header),
	}
	resp.Header.Set("X-Usage-Input-Tokens", "150")
	resp.Header.Set("X-Usage-Output-Tokens", "75")

	usage := ParseTokenUsage("anthropic", resp, nil)

	if usage.InputTokens != 150 {
		t.Errorf("expected 150 input tokens, got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 75 {
		t.Errorf("expected 75 output tokens, got %d", usage.OutputTokens)
	}
}

func TestParseTokenUsage_Anthropic_Body(t *testing.T) {
	resp := &http.Response{
		Header: make(http.Header),
	}
	body := []byte(`{
		"content": [{"type": "text", "text": "Hello!"}],
		"usage": {
			"input_tokens": 200,
			"output_tokens": 100
		}
	}`)

	usage := ParseTokenUsage("anthropic", resp, body)

	if usage.InputTokens != 200 {
		t.Errorf("expected 200 input tokens, got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 100 {
		t.Errorf("expected 100 output tokens, got %d", usage.OutputTokens)
	}
}

func TestParseTokenUsage_Cohere(t *testing.T) {
	body := []byte(`{
		"text": "Response text",
		"meta": {
			"billed_units": {
				"input_tokens": 80,
				"output_tokens": 40
			}
		}
	}`)

	usage := ParseTokenUsage("cohere", nil, body)

	if usage.InputTokens != 80 {
		t.Errorf("expected 80 input tokens, got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 40 {
		t.Errorf("expected 40 output tokens, got %d", usage.OutputTokens)
	}
}

func TestParseTokenUsage_UnknownProvider(t *testing.T) {
	body := []byte(`{"usage": {"tokens": 100}}`)

	usage := ParseTokenUsage("unknown-provider", nil, body)

	if usage.InputTokens != 0 || usage.OutputTokens != 0 {
		t.Error("expected zero tokens for unknown provider")
	}
}

func TestParseTokenUsage_InvalidJSON(t *testing.T) {
	body := []byte(`invalid json`)

	usage := ParseTokenUsage("openai", nil, body)

	if usage.InputTokens != 0 || usage.OutputTokens != 0 {
		t.Error("expected zero tokens for invalid JSON")
	}
}

func TestResponseBodyCapturer(t *testing.T) {
	original := []byte("Hello, World!")
	capturer := NewResponseBodyCapturer(&mockReadCloser{data: original})

	// Read all data
	buf := make([]byte, len(original))
	n, err := capturer.Read(buf)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if n != len(original) {
		t.Errorf("expected to read %d bytes, got %d", len(original), n)
	}

	// Verify captured bytes
	captured := capturer.Bytes()
	if string(captured) != string(original) {
		t.Errorf("expected captured bytes %q, got %q", original, captured)
	}

	// Close should work
	if err := capturer.Close(); err != nil {
		t.Errorf("unexpected error on close: %v", err)
	}
}

// mockReadCloser implements io.ReadCloser for testing
type mockReadCloser struct {
	data   []byte
	offset int
}

func (m *mockReadCloser) Read(p []byte) (n int, err error) {
	if m.offset >= len(m.data) {
		return 0, nil
	}
	n = copy(p, m.data[m.offset:])
	m.offset += n
	return n, nil
}

func (m *mockReadCloser) Close() error {
	return nil
}
