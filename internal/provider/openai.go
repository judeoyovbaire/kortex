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
	OpenAIName    = "openai"
	OpenAIBaseURL = "https://api.openai.com/v1"
)

// OpenAI implements the Provider interface for OpenAI's API
type OpenAI struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewOpenAI creates a new OpenAI provider
func NewOpenAI(config ProviderConfig) *OpenAI {
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = OpenAIBaseURL
	}

	return &OpenAI{
		apiKey:  config.APIKey,
		baseURL: baseURL,
		client:  &http.Client{},
	}
}

// Name returns the provider name
func (o *OpenAI) Name() string {
	return OpenAIName
}

// SetAPIKey configures the provider's API key
func (o *OpenAI) SetAPIKey(key string) {
	o.apiKey = key
}

// GetAuthHeader returns the authentication header for OpenAI
func (o *OpenAI) GetAuthHeader(apiKey string) http.Header {
	h := make(http.Header)
	h.Set("Authorization", "Bearer "+apiKey)
	h.Set("Content-Type", "application/json")
	return h
}

// GetBaseURL returns the base URL for API requests
func (o *OpenAI) GetBaseURL() string {
	return o.baseURL
}

// openAIRequest represents an OpenAI chat completion request
type openAIRequest struct {
	Model       string             `json:"model"`
	Messages    []openAIMessage    `json:"messages"`
	MaxTokens   int                `json:"max_tokens,omitempty"`
	Temperature float64            `json:"temperature,omitempty"`
	TopP        float64            `json:"top_p,omitempty"`
	Stream      bool               `json:"stream,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openAIResponse represents an OpenAI chat completion response
type openAIResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// Chat sends a chat completion request to OpenAI
func (o *OpenAI) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	// Convert to OpenAI format
	messages := make([]openAIMessage, len(req.Messages))
	for i, m := range req.Messages {
		messages[i] = openAIMessage{
			Role:    m.Role,
			Content: m.Content,
		}
	}

	openAIReq := openAIRequest{
		Model:       req.Model,
		Messages:    messages,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      req.Stream,
	}

	body, err := json.Marshal(openAIReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", o.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	for k, v := range o.GetAuthHeader(o.apiKey) {
		httpReq.Header[k] = v
	}

	resp, err := o.client.Do(httpReq)
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

	var openAIResp openAIResponse
	if err := json.Unmarshal(respBody, &openAIResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	chatResp := &ChatResponse{
		ID:    openAIResp.ID,
		Model: openAIResp.Model,
		Usage: TokenUsage{
			InputTokens:  openAIResp.Usage.PromptTokens,
			OutputTokens: openAIResp.Usage.CompletionTokens,
			TotalTokens:  openAIResp.Usage.TotalTokens,
		},
		Raw: respBody,
	}

	if len(openAIResp.Choices) > 0 {
		chatResp.Message = ChatMessage{
			Role:    openAIResp.Choices[0].Message.Role,
			Content: openAIResp.Choices[0].Message.Content,
		}
	}

	return chatResp, nil
}

// ParseTokenUsage extracts token usage from an OpenAI response body
func (o *OpenAI) ParseTokenUsage(body []byte) TokenUsage {
	var resp openAIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return TokenUsage{}
	}
	return TokenUsage{
		InputTokens:  resp.Usage.PromptTokens,
		OutputTokens: resp.Usage.CompletionTokens,
		TotalTokens:  resp.Usage.TotalTokens,
	}
}

// EstimateTokens provides a rough token estimate for OpenAI models
// Uses the approximation of ~4 characters per token for English text
func (o *OpenAI) EstimateTokens(text string) int {
	// Simple approximation: ~4 characters per token for English
	// This is a rough estimate; actual tokenization varies by model
	words := len(strings.Fields(text))
	chars := len(text)

	// Use a weighted average of word-based and character-based estimates
	wordBasedEstimate := int(float64(words) * 1.3)
	charBasedEstimate := chars / 4

	return (wordBasedEstimate + charBasedEstimate) / 2
}

// Ensure OpenAI implements Provider
var _ Provider = (*OpenAI)(nil)
