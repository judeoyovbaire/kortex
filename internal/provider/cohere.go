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
	CohereName    = "cohere"
	CohereBaseURL = "https://api.cohere.ai/v1"
)

// Cohere implements the Provider interface for Cohere's API
type Cohere struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewCohere creates a new Cohere provider
func NewCohere(config ProviderConfig) *Cohere {
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = CohereBaseURL
	}

	return &Cohere{
		apiKey:  config.APIKey,
		baseURL: baseURL,
		client:  &http.Client{},
	}
}

// Name returns the provider name
func (c *Cohere) Name() string {
	return CohereName
}

// SetAPIKey configures the provider's API key
func (c *Cohere) SetAPIKey(key string) {
	c.apiKey = key
}

// GetAuthHeader returns the authentication header for Cohere
func (c *Cohere) GetAuthHeader(apiKey string) http.Header {
	h := make(http.Header)
	h.Set("Authorization", "Bearer "+apiKey)
	h.Set("Content-Type", "application/json")
	return h
}

// GetBaseURL returns the base URL for API requests
func (c *Cohere) GetBaseURL() string {
	return c.baseURL
}

// cohereRequest represents a Cohere chat request
type cohereRequest struct {
	Model       string          `json:"model"`
	Message     string          `json:"message"`
	ChatHistory []cohereMessage `json:"chat_history,omitempty"`
	Preamble    string          `json:"preamble,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
}

type cohereMessage struct {
	Role    string `json:"role"` // USER or CHATBOT
	Message string `json:"message"`
}

// cohereResponse represents a Cohere chat response
type cohereResponse struct {
	ResponseID   string `json:"response_id"`
	Text         string `json:"text"`
	GenerationID string `json:"generation_id"`
	Meta         struct {
		APIVersion struct {
			Version string `json:"version"`
		} `json:"api_version"`
		BilledUnits struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"billed_units"`
		Tokens struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"tokens"`
	} `json:"meta"`
}

// Chat sends a chat completion request to Cohere
func (c *Cohere) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	// Convert messages to Cohere format
	var chatHistory []cohereMessage
	var preamble string
	var lastUserMessage string

	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			preamble = m.Content
		case "user":
			if lastUserMessage != "" {
				// Previous user message, add to history
				chatHistory = append(chatHistory, cohereMessage{
					Role:    "USER",
					Message: lastUserMessage,
				})
			}
			lastUserMessage = m.Content
		case "assistant":
			chatHistory = append(chatHistory, cohereMessage{
				Role:    "CHATBOT",
				Message: m.Content,
			})
		}
	}

	cohereReq := cohereRequest{
		Model:       req.Model,
		Message:     lastUserMessage,
		ChatHistory: chatHistory,
		Preamble:    preamble,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      req.Stream,
	}

	body, err := json.Marshal(cohereReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	for k, v := range c.GetAuthHeader(c.apiKey) {
		httpReq.Header[k] = v
	}

	resp, err := c.client.Do(httpReq)
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

	var cohereResp cohereResponse
	if err := json.Unmarshal(respBody, &cohereResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Use billed_units if available, otherwise use tokens
	inputTokens := cohereResp.Meta.BilledUnits.InputTokens
	outputTokens := cohereResp.Meta.BilledUnits.OutputTokens
	if inputTokens == 0 {
		inputTokens = cohereResp.Meta.Tokens.InputTokens
	}
	if outputTokens == 0 {
		outputTokens = cohereResp.Meta.Tokens.OutputTokens
	}

	chatResp := &ChatResponse{
		ID:    cohereResp.ResponseID,
		Model: req.Model,
		Message: ChatMessage{
			Role:    "assistant",
			Content: cohereResp.Text,
		},
		Usage: TokenUsage{
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			TotalTokens:  inputTokens + outputTokens,
		},
		Raw: respBody,
	}

	return chatResp, nil
}

// ParseTokenUsage extracts token usage from a Cohere response body
func (c *Cohere) ParseTokenUsage(body []byte) TokenUsage {
	var resp cohereResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return TokenUsage{}
	}

	inputTokens := resp.Meta.BilledUnits.InputTokens
	outputTokens := resp.Meta.BilledUnits.OutputTokens
	if inputTokens == 0 {
		inputTokens = resp.Meta.Tokens.InputTokens
	}
	if outputTokens == 0 {
		outputTokens = resp.Meta.Tokens.OutputTokens
	}

	return TokenUsage{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalTokens:  inputTokens + outputTokens,
	}
}

// EstimateTokens provides a rough token estimate for Cohere models
func (c *Cohere) EstimateTokens(text string) int {
	// Cohere uses similar tokenization patterns
	words := len(strings.Fields(text))
	chars := len(text)

	wordBasedEstimate := int(float64(words) * 1.3)
	charBasedEstimate := chars / 4

	return (wordBasedEstimate + charBasedEstimate) / 2
}

// Ensure Cohere implements Provider
var _ Provider = (*Cohere)(nil)