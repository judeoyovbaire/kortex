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
	"context"
	"errors"
	"net/http"
	"syscall"
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestRetrier_SuccessOnFirstAttempt(t *testing.T) {
	log := zap.New(zap.UseDevMode(true))
	config := RetryConfig{
		MaxRetries:        3,
		InitialBackoff:    10 * time.Millisecond,
		MaxBackoff:        100 * time.Millisecond,
		BackoffMultiplier: 2.0,
		Jitter:            0.1,
	}
	retrier := NewRetrier(config, log)

	attempts := 0
	result := retrier.Do(context.Background(), "test-backend", func(ctx context.Context, attempt int) (int, error) {
		attempts++
		return http.StatusOK, nil
	})

	if result.LastError != nil {
		t.Errorf("expected no error, got %v", result.LastError)
	}
	if result.Attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", result.Attempts)
	}
	if attempts != 1 {
		t.Errorf("expected function to be called 1 time, called %d", attempts)
	}
	if result.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", result.StatusCode)
	}
}

func TestRetrier_SuccessAfterRetries(t *testing.T) {
	log := zap.New(zap.UseDevMode(true))
	config := RetryConfig{
		MaxRetries:           3,
		InitialBackoff:       10 * time.Millisecond,
		MaxBackoff:           100 * time.Millisecond,
		BackoffMultiplier:    2.0,
		Jitter:               0,
		RetryableStatusCodes: []int{503},
	}
	retrier := NewRetrier(config, log)

	attempts := 0
	result := retrier.Do(context.Background(), "test-backend", func(ctx context.Context, attempt int) (int, error) {
		attempts++
		if attempts < 3 {
			return http.StatusServiceUnavailable, nil
		}
		return http.StatusOK, nil
	})

	if result.LastError != nil {
		t.Errorf("expected no error, got %v", result.LastError)
	}
	if result.Attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", result.Attempts)
	}
	if result.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", result.StatusCode)
	}
}

func TestRetrier_ExhaustedRetries(t *testing.T) {
	log := zap.New(zap.UseDevMode(true))
	config := RetryConfig{
		MaxRetries:           2,
		InitialBackoff:       10 * time.Millisecond,
		MaxBackoff:           100 * time.Millisecond,
		BackoffMultiplier:    2.0,
		Jitter:               0,
		RetryableStatusCodes: []int{503},
	}
	retrier := NewRetrier(config, log)

	attempts := 0
	result := retrier.Do(context.Background(), "test-backend", func(ctx context.Context, attempt int) (int, error) {
		attempts++
		return http.StatusServiceUnavailable, nil
	})

	// MaxRetries=2 means initial + 2 retries = 3 total attempts
	if result.Attempts != 3 {
		t.Errorf("expected 3 attempts (1 initial + 2 retries), got %d", result.Attempts)
	}
	if result.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", result.StatusCode)
	}
}

func TestRetrier_RetriesOnError(t *testing.T) {
	log := zap.New(zap.UseDevMode(true))
	config := RetryConfig{
		MaxRetries:             2,
		InitialBackoff:         10 * time.Millisecond,
		MaxBackoff:             100 * time.Millisecond,
		BackoffMultiplier:      2.0,
		Jitter:                 0,
		RetryOnConnectionError: true,
	}
	retrier := NewRetrier(config, log)

	attempts := 0
	testErr := errors.New("connection error")
	result := retrier.Do(context.Background(), "test-backend", func(ctx context.Context, attempt int) (int, error) {
		attempts++
		if attempts < 3 {
			return 0, syscall.ECONNREFUSED
		}
		return http.StatusOK, nil
	})

	if result.LastError != nil {
		t.Errorf("expected success after retries, got %v", result.LastError)
	}
	if result.Attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", result.Attempts)
	}
	_ = testErr // silence unused warning
}

func TestRetrier_ContextCancellation(t *testing.T) {
	log := zap.New(zap.UseDevMode(true))
	config := RetryConfig{
		MaxRetries:           5,
		InitialBackoff:       100 * time.Millisecond,
		MaxBackoff:           1 * time.Second,
		BackoffMultiplier:    2.0,
		Jitter:               0,
		RetryableStatusCodes: []int{503},
	}
	retrier := NewRetrier(config, log)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	attempts := 0
	result := retrier.Do(ctx, "test-backend", func(ctx context.Context, attempt int) (int, error) {
		attempts++
		return http.StatusServiceUnavailable, nil
	})

	// Should stop early due to context cancellation
	if result.LastError != context.DeadlineExceeded {
		t.Errorf("expected context.DeadlineExceeded, got %v", result.LastError)
	}
	if attempts > 2 {
		t.Errorf("expected at most 2 attempts before timeout, got %d", attempts)
	}
}

func TestRetrier_CalculateBackoff(t *testing.T) {
	log := zap.New(zap.UseDevMode(true))
	config := RetryConfig{
		MaxRetries:        5,
		InitialBackoff:    100 * time.Millisecond,
		MaxBackoff:        1 * time.Second,
		BackoffMultiplier: 2.0,
		Jitter:            0, // No jitter for predictable tests
	}
	retrier := NewRetrier(config, log)

	// Attempt 0: 100ms
	backoff0 := retrier.calculateBackoff(0)
	if backoff0 != 100*time.Millisecond {
		t.Errorf("expected 100ms for attempt 0, got %v", backoff0)
	}

	// Attempt 1: 200ms
	backoff1 := retrier.calculateBackoff(1)
	if backoff1 != 200*time.Millisecond {
		t.Errorf("expected 200ms for attempt 1, got %v", backoff1)
	}

	// Attempt 2: 400ms
	backoff2 := retrier.calculateBackoff(2)
	if backoff2 != 400*time.Millisecond {
		t.Errorf("expected 400ms for attempt 2, got %v", backoff2)
	}

	// Attempt 5: should be capped at MaxBackoff (1s)
	backoff5 := retrier.calculateBackoff(5)
	if backoff5 != 1*time.Second {
		t.Errorf("expected 1s (max) for attempt 5, got %v", backoff5)
	}
}

func TestRetrier_CalculateBackoffWithJitter(t *testing.T) {
	log := zap.New(zap.UseDevMode(true))
	config := RetryConfig{
		MaxRetries:        5,
		InitialBackoff:    100 * time.Millisecond,
		MaxBackoff:        1 * time.Second,
		BackoffMultiplier: 2.0,
		Jitter:            0.5, // 50% jitter
	}
	retrier := NewRetrier(config, log)

	// With jitter, backoff should vary but stay within bounds
	for i := 0; i < 10; i++ {
		backoff := retrier.calculateBackoff(0)
		// With 50% jitter on 100ms, range should be roughly 50-150ms
		if backoff < 50*time.Millisecond || backoff > 150*time.Millisecond {
			t.Errorf("backoff %v outside expected jitter range", backoff)
		}
	}
}

func TestRetrier_IsRetryableStatusCode(t *testing.T) {
	log := zap.New(zap.UseDevMode(true))
	config := RetryConfig{
		RetryableStatusCodes: []int{502, 503, 504},
	}
	retrier := NewRetrier(config, log)

	tests := []struct {
		code     int
		expected bool
	}{
		{200, false},
		{400, false},
		{500, false},
		{502, true},
		{503, true},
		{504, true},
	}

	for _, tt := range tests {
		if got := retrier.isRetryableStatusCode(tt.code); got != tt.expected {
			t.Errorf("isRetryableStatusCode(%d) = %v, want %v", tt.code, got, tt.expected)
		}
	}
}

func TestRetrier_IsRetryableError(t *testing.T) {
	log := zap.New(zap.UseDevMode(true))
	config := RetryConfig{
		RetryOnConnectionError: true,
		RetryOnTimeout:         true,
	}
	retrier := NewRetrier(config, log)

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"context canceled", context.Canceled, false},
		{"context deadline exceeded", context.DeadlineExceeded, false},
		{"connection refused", syscall.ECONNREFUSED, true},
		{"connection reset", syscall.ECONNRESET, true},
		{"connection aborted", syscall.ECONNABORTED, true},
		{"random error", errors.New("random"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := retrier.isRetryableError(tt.err); got != tt.expected {
				t.Errorf("isRetryableError() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestIsRetryable(t *testing.T) {
	config := DefaultRetryConfig()

	// Test with retryable status code
	resp := &http.Response{StatusCode: 503}
	if !IsRetryable(resp, nil, config) {
		t.Error("expected 503 to be retryable")
	}

	// Test with non-retryable status code
	resp = &http.Response{StatusCode: 200}
	if IsRetryable(resp, nil, config) {
		t.Error("expected 200 to not be retryable")
	}

	// Test with connection error
	if !IsRetryable(nil, syscall.ECONNREFUSED, config) {
		t.Error("expected ECONNREFUSED to be retryable")
	}
}

func TestRetryWithCircuitBreaker_Execute(t *testing.T) {
	log := zap.New(zap.UseDevMode(true))

	retryConfig := RetryConfig{
		MaxRetries:           2,
		InitialBackoff:       10 * time.Millisecond,
		MaxBackoff:           100 * time.Millisecond,
		BackoffMultiplier:    2.0,
		Jitter:               0,
		RetryableStatusCodes: []int{503},
	}

	cbConfig := CircuitBreakerConfig{
		FailureThreshold:    3,
		SuccessThreshold:    2,
		Timeout:             100 * time.Millisecond,
		HalfOpenMaxRequests: 1,
	}

	handler := NewRetryWithCircuitBreaker(retryConfig, cbConfig, log)

	// Successful request
	result := handler.Execute(context.Background(), "test-backend", func(ctx context.Context, attempt int) (int, error) {
		return http.StatusOK, nil
	})

	if result.LastError != nil {
		t.Errorf("expected no error, got %v", result.LastError)
	}

	// Circuit should record success
	stats := handler.GetCircuitBreaker().AllStats()
	if stats["test-backend"].Successes != 1 {
		t.Errorf("expected 1 success recorded, got %d", stats["test-backend"].Successes)
	}
}

func TestRetryWithCircuitBreaker_CircuitOpen(t *testing.T) {
	log := zap.New(zap.UseDevMode(true))

	retryConfig := RetryConfig{
		MaxRetries:        2,
		InitialBackoff:    10 * time.Millisecond,
		MaxBackoff:        100 * time.Millisecond,
		BackoffMultiplier: 2.0,
		Jitter:            0,
	}

	cbConfig := CircuitBreakerConfig{
		FailureThreshold:    2,
		SuccessThreshold:    2,
		Timeout:             100 * time.Millisecond,
		HalfOpenMaxRequests: 1,
	}

	handler := NewRetryWithCircuitBreaker(retryConfig, cbConfig, log)

	// Trip the circuit breaker
	for i := 0; i < 2; i++ {
		handler.Execute(context.Background(), "test-backend", func(ctx context.Context, attempt int) (int, error) {
			return 0, errors.New("failure")
		})
	}

	// Next request should fail immediately due to open circuit
	result := handler.Execute(context.Background(), "test-backend", func(ctx context.Context, attempt int) (int, error) {
		t.Error("function should not be called when circuit is open")
		return http.StatusOK, nil
	})

	if result.LastError != ErrCircuitOpen {
		t.Errorf("expected ErrCircuitOpen, got %v", result.LastError)
	}
	if result.Attempts != 0 {
		t.Errorf("expected 0 attempts, got %d", result.Attempts)
	}
}

func TestDefaultRetryConfig(t *testing.T) {
	config := DefaultRetryConfig()

	if config.MaxRetries != 3 {
		t.Errorf("expected MaxRetries=3, got %d", config.MaxRetries)
	}
	if config.InitialBackoff != 100*time.Millisecond {
		t.Errorf("expected InitialBackoff=100ms, got %v", config.InitialBackoff)
	}
	if config.MaxBackoff != 10*time.Second {
		t.Errorf("expected MaxBackoff=10s, got %v", config.MaxBackoff)
	}
	if len(config.RetryableStatusCodes) != 3 {
		t.Errorf("expected 3 retryable status codes, got %d", len(config.RetryableStatusCodes))
	}
	if !config.RetryOnConnectionError {
		t.Error("expected RetryOnConnectionError=true")
	}
	if !config.RetryOnTimeout {
		t.Error("expected RetryOnTimeout=true")
	}
}
