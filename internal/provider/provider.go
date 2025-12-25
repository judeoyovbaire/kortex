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

// Package provider defines the abstraction for LLM inference providers.
// This allows easy addition of new providers (OpenAI, Anthropic, Cohere, DeepSeek, etc.)
// by implementing a common interface.
package provider

import (
	"context"
	"net/http"
)

// TokenUsage represents token consumption for a request
type TokenUsage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

// ChatMessage represents a message in a chat conversation
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest represents a chat completion request
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
	TopP        float64       `json:"top_p,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
}

// ChatResponse represents a chat completion response
type ChatResponse struct {
	ID      string        `json:"id"`
	Model   string        `json:"model"`
	Message ChatMessage   `json:"message"`
	Usage   TokenUsage    `json:"usage"`
	Raw     []byte        `json:"-"` // Raw response body for passthrough
}

// Provider defines the interface that all inference providers must implement
type Provider interface {
	// Name returns the provider name (e.g., "openai", "anthropic", "cohere")
	Name() string

	// Chat sends a chat completion request and returns the response
	Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)

	// SetAPIKey configures the provider's API key
	SetAPIKey(key string)

	// GetAuthHeader returns the authentication header for this provider
	GetAuthHeader(apiKey string) http.Header

	// GetBaseURL returns the base URL for API requests
	GetBaseURL() string

	// ParseTokenUsage extracts token usage from a response body
	ParseTokenUsage(body []byte) TokenUsage

	// EstimateTokens estimates the number of tokens in a text string
	// This is useful for pre-request routing decisions
	EstimateTokens(text string) int
}

// ProviderConfig holds common configuration for providers
type ProviderConfig struct {
	// APIKey is the authentication key for the provider
	APIKey string

	// BaseURL overrides the default API endpoint
	BaseURL string

	// Timeout is the request timeout in seconds
	Timeout int

	// MaxRetries is the number of retry attempts for failed requests
	MaxRetries int
}

// DefaultConfig returns a default provider configuration
func DefaultConfig() ProviderConfig {
	return ProviderConfig{
		Timeout:    120,
		MaxRetries: 3,
	}
}

// Registry holds all registered providers
type Registry struct {
	providers map[string]Provider
}

// NewRegistry creates a new provider registry
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
	}
}

// Register adds a provider to the registry
func (r *Registry) Register(provider Provider) {
	r.providers[provider.Name()] = provider
}

// Get retrieves a provider by name
func (r *Registry) Get(name string) (Provider, bool) {
	p, ok := r.providers[name]
	return p, ok
}

// List returns all registered provider names
func (r *Registry) List() []string {
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}