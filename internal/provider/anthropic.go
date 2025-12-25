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

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	AnthropicName       = "anthropic"
	AnthropicBaseURL    = "https://api.anthropic.com"
	AnthropicAPIVersion = "2023-06-01"
)

// Anthropic implements the Provider interface for Anthropic's Claude API
type Anthropic struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewAnthropic creates a new Anthropic provider
func NewAnthropic(config ProviderConfig) *Anthropic {
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = AnthropicBaseURL
	}

	return &Anthropic{
		apiKey:  config.APIKey,
		baseURL: baseURL,
		client:  &http.Client{},
	}
}

// Name returns the provider name
func (a *Anthropic) Name() string {
	return AnthropicName
}

// SetAPIKey configures the provider's API key
func (a *Anthropic) SetAPIKey(key string) {
	a.apiKey = key
}

// GetAuthHeader returns the authentication header for Anthropic
func (a *Anthropic) GetAuthHeader(apiKey string) http.Header {
	h := make(http.Header)
	h.Set("x-api-key", apiKey)
	h.Set("anthropic-version", AnthropicAPIVersion)
	h.Set("Content-Type", "application/json")
	return h
}

// GetBaseURL returns the base URL for API requests
func (a *Anthropic) GetBaseURL() string {
	return a.baseURL
}

// anthropicRequest represents an Anthropic messages request
type anthropicRequest struct {
	Model     string              `json:"model"`
	MaxTokens int                 `json:"max_tokens"`
	Messages  []anthropicMessage  `json:"messages"`
	System    string              `json:"system,omitempty"`
	Stream    bool                `json:"stream,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicResponse represents an Anthropic messages response
type anthropicResponse struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	Role         string `json:"role"`
	Model        string `json:"model"`
	Content      []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason   string `json:"stop_reason"`
	StopSequence string `json:"stop_sequence"`
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Chat sends a chat completion request to Anthropic
func (a *Anthropic) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	// Convert messages, extracting system message if present
	var systemMessage string
	messages := make([]anthropicMessage, 0, len(req.Messages))

	for _, m := range req.Messages {
		if m.Role == "system" {
			systemMessage = m.Content
			continue
		}
		messages = append(messages, anthropicMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096 // Anthropic requires max_tokens
	}

	anthropicReq := anthropicRequest{
		Model:     req.Model,
		MaxTokens: maxTokens,
		Messages:  messages,
		System:    systemMessage,
		Stream:    req.Stream,
	}

	body, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	for k, v := range a.GetAuthHeader(a.apiKey) {
		httpReq.Header[k] = v
	}

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var anthropicResp anthropicResponse
	if err := json.Unmarshal(respBody, &anthropicResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	chatResp := &ChatResponse{
		ID:    anthropicResp.ID,
		Model: anthropicResp.Model,
		Usage: TokenUsage{
			InputTokens:  anthropicResp.Usage.InputTokens,
			OutputTokens: anthropicResp.Usage.OutputTokens,
			TotalTokens:  anthropicResp.Usage.InputTokens + anthropicResp.Usage.OutputTokens,
		},
		Raw: respBody,
	}

	// Extract text content from response
	var content strings.Builder
	for _, c := range anthropicResp.Content {
		if c.Type == "text" {
			content.WriteString(c.Text)
		}
	}

	chatResp.Message = ChatMessage{
		Role:    "assistant",
		Content: content.String(),
	}

	return chatResp, nil
}

// ParseTokenUsage extracts token usage from an Anthropic response body
func (a *Anthropic) ParseTokenUsage(body []byte) TokenUsage {
	var resp anthropicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return TokenUsage{}
	}
	return TokenUsage{
		InputTokens:  resp.Usage.InputTokens,
		OutputTokens: resp.Usage.OutputTokens,
		TotalTokens:  resp.Usage.InputTokens + resp.Usage.OutputTokens,
	}
}

// EstimateTokens provides a rough token estimate for Anthropic models
// Claude uses a similar tokenization to GPT models
func (a *Anthropic) EstimateTokens(text string) int {
	// Similar approximation to OpenAI: ~4 characters per token
	words := len(strings.Fields(text))
	chars := len(text)

	wordBasedEstimate := int(float64(words) * 1.3)
	charBasedEstimate := chars / 4

	return (wordBasedEstimate + charBasedEstimate) / 2
}

// Ensure Anthropic implements Provider
var _ Provider = (*Anthropic)(nil)