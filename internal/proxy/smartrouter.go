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
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-logr/logr"

	gatewayv1alpha1 "github.com/judeoyovbaire/kortex/api/v1alpha1"
)

// SmartRouterConfig holds configuration for smart routing decisions
type SmartRouterConfig struct {
	// LongContextThreshold is the token count above which requests are routed to long-context models
	LongContextThreshold int

	// FastModelThreshold is the token count below which requests are routed to fast models
	FastModelThreshold int

	// LongContextBackend is the backend name for long-context requests
	LongContextBackend string

	// FastModelBackend is the backend name for fast, short requests
	FastModelBackend string

	// DefaultBackend is the backend used when no smart routing rules match
	DefaultBackend string

	// EnableCostOptimization enables cost-based routing decisions
	EnableCostOptimization bool
}

// DefaultSmartRouterConfig returns sensible defaults for smart routing
func DefaultSmartRouterConfig() SmartRouterConfig {
	return SmartRouterConfig{
		LongContextThreshold:   4000,
		FastModelThreshold:     500,
		EnableCostOptimization: false,
	}
}

// SmartRouter provides intelligent routing based on request characteristics
type SmartRouter struct {
	config SmartRouterConfig
	log    logr.Logger
}

// NewSmartRouter creates a new smart router instance
func NewSmartRouter(config SmartRouterConfig, log logr.Logger) *SmartRouter {
	return &SmartRouter{
		config: config,
		log:    log.WithName("smart-router"),
	}
}

// RouteDecision represents a smart routing decision
type RouteDecision struct {
	// Backend is the selected backend name
	Backend string

	// Reason explains why this backend was selected
	Reason string

	// EstimatedTokens is the estimated input token count
	EstimatedTokens int

	// Category is the request category (short, medium, long)
	Category string
}

// SelectBackend analyzes the request and returns a routing decision
func (s *SmartRouter) SelectBackend(req *http.Request, route *gatewayv1alpha1.InferenceRoute) *RouteDecision {
	// Try to extract and estimate tokens from the request body
	estimatedTokens := s.estimateRequestTokens(req)

	decision := &RouteDecision{
		EstimatedTokens: estimatedTokens,
	}

	// Determine category based on token count
	switch {
	case estimatedTokens > s.config.LongContextThreshold:
		decision.Category = "long"
		if s.config.LongContextBackend != "" {
			decision.Backend = s.config.LongContextBackend
			decision.Reason = "Token count exceeds long-context threshold"
		}

	case estimatedTokens < s.config.FastModelThreshold:
		decision.Category = "short"
		if s.config.FastModelBackend != "" {
			decision.Backend = s.config.FastModelBackend
			decision.Reason = "Token count below fast-model threshold"
		}

	default:
		decision.Category = "medium"
		if s.config.DefaultBackend != "" {
			decision.Backend = s.config.DefaultBackend
			decision.Reason = "Standard routing for medium-length requests"
		}
	}

	// If no backend was selected, fall back to default
	if decision.Backend == "" {
		if route.Spec.DefaultBackend != nil {
			decision.Backend = route.Spec.DefaultBackend.Name
			decision.Reason = "Fallback to route default backend"
		}
	}

	s.log.V(1).Info("Smart routing decision",
		"estimated_tokens", estimatedTokens,
		"category", decision.Category,
		"backend", decision.Backend,
		"reason", decision.Reason,
	)

	return decision
}

// estimateRequestTokens extracts message content and estimates token count
func (s *SmartRouter) estimateRequestTokens(req *http.Request) int {
	if req.Body == nil {
		return 0
	}

	// Read the body
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		s.log.V(1).Info("Failed to read request body for token estimation", "error", err)
		return 0
	}

	// Replace the body so it can still be read by subsequent handlers
	req.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// Try to parse as OpenAI-compatible chat format
	var chatReq struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
		Prompt string `json:"prompt"` // For completion-style requests
	}

	if err := json.Unmarshal(bodyBytes, &chatReq); err != nil {
		s.log.V(2).Info("Failed to parse request body as chat format", "error", err)
		// Fall back to raw body token estimation
		return estimateTokensFromText(string(bodyBytes))
	}

	// Aggregate all message content
	var totalText strings.Builder
	for _, msg := range chatReq.Messages {
		totalText.WriteString(msg.Content)
		totalText.WriteString(" ")
	}

	// Also include prompt field if present
	if chatReq.Prompt != "" {
		totalText.WriteString(chatReq.Prompt)
	}

	return estimateTokensFromText(totalText.String())
}

// estimateTokensFromText provides a rough token estimate
// Uses the approximation of ~4 characters per token for English text
func estimateTokensFromText(text string) int {
	if text == "" {
		return 0
	}

	// Word-based estimation
	words := len(strings.Fields(text))

	// Character-based estimation (for non-English or code)
	chars := len(text)

	// Use a weighted combination
	// - Words * 1.3 (average tokens per word)
	// - Characters / 4 (average chars per token)
	wordEstimate := int(float64(words) * 1.3)
	charEstimate := chars / 4

	// Return the average for better accuracy across different content types
	return (wordEstimate + charEstimate) / 2
}

// CostBasedSelection selects a backend based on cost optimization
func (s *SmartRouter) CostBasedSelection(
	backends []gatewayv1alpha1.BackendRef,
	estimatedTokens int,
	backendCosts map[string]*gatewayv1alpha1.CostConfig,
) string {
	if len(backends) == 0 || !s.config.EnableCostOptimization {
		return ""
	}

	var bestBackend string
	bestCost := float64(-1)

	for _, backend := range backends {
		cost, ok := backendCosts[backend.Name]
		if !ok || cost == nil {
			continue
		}

		// Parse cost strings to floats
		inputCost := parseCostString(cost.InputTokenCost)
		requestCost := parseCostString(cost.RequestCost)

		// Calculate estimated cost for this backend
		// Cost = (input_tokens * input_cost_per_1k / 1000) + fixed_request_cost
		estimatedCost := (float64(estimatedTokens) * inputCost / 1000) + requestCost

		s.log.V(2).Info("Cost calculation",
			"backend", backend.Name,
			"estimated_tokens", estimatedTokens,
			"input_cost_per_1k", inputCost,
			"request_cost", requestCost,
			"estimated_total_cost", estimatedCost,
		)

		if bestCost < 0 || estimatedCost < bestCost {
			bestCost = estimatedCost
			bestBackend = backend.Name
		}
	}

	if bestBackend != "" {
		s.log.V(1).Info("Cost-optimized backend selection",
			"backend", bestBackend,
			"estimated_cost", bestCost,
		)
	}

	return bestBackend
}

// parseCostString converts a cost string (e.g., "0.0001") to a float64.
// Returns 0 if the string is empty or cannot be parsed.
func parseCostString(s string) float64 {
	if s == "" {
		return 0
	}
	var f float64
	if _, err := fmt.Sscanf(s, "%f", &f); err != nil {
		return 0
	}
	return f
}

// LatencyBasedSelection selects a backend optimized for latency
// Useful for short requests where response time matters more than cost
func (s *SmartRouter) LatencyBasedSelection(
	backends []gatewayv1alpha1.BackendRef,
	backendHealth map[string]int64, // backend name -> average latency in ms
) string {
	if len(backends) == 0 {
		return ""
	}

	var bestBackend string
	bestLatency := int64(-1)

	for _, backend := range backends {
		latency, ok := backendHealth[backend.Name]
		if !ok {
			continue
		}

		if bestLatency < 0 || latency < bestLatency {
			bestLatency = latency
			bestBackend = backend.Name
		}
	}

	if bestBackend != "" {
		s.log.V(1).Info("Latency-optimized backend selection",
			"backend", bestBackend,
			"latency_ms", bestLatency,
		)
	}

	return bestBackend
}

// ContextLengthCapability returns whether a backend can handle the estimated token count
func (s *SmartRouter) ContextLengthCapability(
	backendName string,
	estimatedTokens int,
	backendContextLimits map[string]int, // backend name -> max context tokens
) bool {
	limit, ok := backendContextLimits[backendName]
	if !ok {
		// Assume capable if no limit specified
		return true
	}

	// Leave headroom for response tokens (assume 25% of context for output)
	effectiveLimit := int(float64(limit) * 0.75)
	return estimatedTokens <= effectiveLimit
}
