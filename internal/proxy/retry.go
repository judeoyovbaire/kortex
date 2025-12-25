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
	"math"
	"math/rand"
	"net"
	"net/http"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Prometheus metrics for retries
	retryAttempts = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kortex_retry_attempts_total",
			Help: "Total retry attempts",
		},
		[]string{"backend", "attempt"},
	)

	retrySuccesses = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kortex_retry_successes_total",
			Help: "Total successful retries",
		},
		[]string{"backend"},
	)

	retryExhausted = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kortex_retry_exhausted_total",
			Help: "Total times retries were exhausted",
		},
		[]string{"backend"},
	)
)

// RetryConfig holds configuration for retry behavior
type RetryConfig struct {
	// MaxRetries is the maximum number of retry attempts (0 = no retries)
	MaxRetries int

	// InitialBackoff is the initial backoff duration
	InitialBackoff time.Duration

	// MaxBackoff is the maximum backoff duration
	MaxBackoff time.Duration

	// BackoffMultiplier is multiplied to backoff after each retry
	BackoffMultiplier float64

	// Jitter adds randomness to backoff (0.0 = no jitter, 1.0 = full jitter)
	Jitter float64

	// RetryableStatusCodes are HTTP status codes that should trigger a retry
	RetryableStatusCodes []int

	// RetryOnConnectionError retries on connection errors
	RetryOnConnectionError bool

	// RetryOnTimeout retries on timeout errors
	RetryOnTimeout bool
}

// DefaultRetryConfig returns sensible defaults for retry configuration
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:             3,
		InitialBackoff:         100 * time.Millisecond,
		MaxBackoff:             10 * time.Second,
		BackoffMultiplier:      2.0,
		Jitter:                 0.3, // 30% jitter
		RetryableStatusCodes:   []int{502, 503, 504}, // Bad Gateway, Service Unavailable, Gateway Timeout
		RetryOnConnectionError: true,
		RetryOnTimeout:         true,
	}
}

// Retrier handles retry logic with exponential backoff
type Retrier struct {
	config RetryConfig
	log    logr.Logger
	rng    *rand.Rand
}

// NewRetrier creates a new Retrier with the given configuration
func NewRetrier(config RetryConfig, log logr.Logger) *Retrier {
	return &Retrier{
		config: config,
		log:    log.WithName("retrier"),
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// RetryResult contains the result of a retry operation
type RetryResult struct {
	// Attempts is the total number of attempts made
	Attempts int

	// LastError is the last error encountered (nil if successful)
	LastError error

	// Duration is the total time spent including retries
	Duration time.Duration

	// StatusCode is the last HTTP status code (if applicable)
	StatusCode int
}

// RetryableFunc is a function that can be retried
// Returns (statusCode, error) - statusCode is used for retry decisions
type RetryableFunc func(ctx context.Context, attempt int) (statusCode int, err error)

// Do executes a function with retries
func (r *Retrier) Do(ctx context.Context, backendName string, fn RetryableFunc) RetryResult {
	start := time.Now()
	result := RetryResult{}

	for attempt := 0; attempt <= r.config.MaxRetries; attempt++ {
		result.Attempts = attempt + 1

		// Record attempt metric
		retryAttempts.WithLabelValues(backendName, string(rune('0'+attempt))).Inc()

		// Execute the function
		statusCode, err := fn(ctx, attempt)
		result.StatusCode = statusCode
		result.LastError = err

		// Check if successful
		if err == nil && !r.isRetryableStatusCode(statusCode) {
			result.Duration = time.Since(start)
			if attempt > 0 {
				retrySuccesses.WithLabelValues(backendName).Inc()
				r.log.V(1).Info("Request succeeded after retry",
					"backend", backendName,
					"attempts", result.Attempts,
					"duration", result.Duration,
				)
			}
			return result
		}

		// Check if we should retry
		if !r.shouldRetry(ctx, err, statusCode, attempt) {
			break
		}

		// Calculate backoff
		backoff := r.calculateBackoff(attempt)

		r.log.V(1).Info("Retrying request",
			"backend", backendName,
			"attempt", attempt+1,
			"maxRetries", r.config.MaxRetries,
			"backoff", backoff,
			"statusCode", statusCode,
			"error", err,
		)

		// Wait before retrying
		select {
		case <-ctx.Done():
			result.LastError = ctx.Err()
			result.Duration = time.Since(start)
			return result
		case <-time.After(backoff):
			// Continue to next attempt
		}
	}

	// All retries exhausted
	result.Duration = time.Since(start)
	retryExhausted.WithLabelValues(backendName).Inc()

	r.log.Info("All retries exhausted",
		"backend", backendName,
		"attempts", result.Attempts,
		"duration", result.Duration,
		"lastError", result.LastError,
	)

	return result
}

// shouldRetry determines if the request should be retried
func (r *Retrier) shouldRetry(ctx context.Context, err error, statusCode int, attempt int) bool {
	// Don't retry if we've reached max retries
	if attempt >= r.config.MaxRetries {
		return false
	}

	// Don't retry if context is cancelled
	if ctx.Err() != nil {
		return false
	}

	// Check status code
	if r.isRetryableStatusCode(statusCode) {
		return true
	}

	// Check error type
	if err != nil {
		if r.isRetryableError(err) {
			return true
		}
	}

	return false
}

// isRetryableStatusCode checks if the status code should trigger a retry
func (r *Retrier) isRetryableStatusCode(statusCode int) bool {
	for _, code := range r.config.RetryableStatusCodes {
		if statusCode == code {
			return true
		}
	}
	return false
}

// isRetryableError checks if the error should trigger a retry
func (r *Retrier) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for context errors (not retryable)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// Check for connection errors
	if r.config.RetryOnConnectionError {
		var netErr net.Error
		if errors.As(err, &netErr) {
			return true
		}

		// Check for specific syscall errors (connection refused, etc.)
		if errors.Is(err, syscall.ECONNREFUSED) ||
			errors.Is(err, syscall.ECONNRESET) ||
			errors.Is(err, syscall.ECONNABORTED) {
			return true
		}
	}

	// Check for timeout errors
	if r.config.RetryOnTimeout {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return true
		}
	}

	return false
}

// calculateBackoff calculates the backoff duration for a given attempt
func (r *Retrier) calculateBackoff(attempt int) time.Duration {
	// Exponential backoff: initial * multiplier^attempt
	backoff := float64(r.config.InitialBackoff) * math.Pow(r.config.BackoffMultiplier, float64(attempt))

	// Apply jitter
	if r.config.Jitter > 0 {
		jitter := r.rng.Float64() * r.config.Jitter * backoff
		backoff = backoff - (r.config.Jitter * backoff / 2) + jitter
	}

	// Cap at max backoff
	if backoff > float64(r.config.MaxBackoff) {
		backoff = float64(r.config.MaxBackoff)
	}

	return time.Duration(backoff)
}

// IsRetryable is a convenience function to check if an HTTP response should be retried
func IsRetryable(resp *http.Response, err error, config RetryConfig) bool {
	if err != nil {
		r := &Retrier{config: config}
		return r.isRetryableError(err)
	}

	if resp != nil {
		for _, code := range config.RetryableStatusCodes {
			if resp.StatusCode == code {
				return true
			}
		}
	}

	return false
}

// RetryWithCircuitBreaker combines retry logic with circuit breaker
type RetryWithCircuitBreaker struct {
	retrier        *Retrier
	circuitBreaker *CircuitBreakerManager
	log            logr.Logger
}

// NewRetryWithCircuitBreaker creates a new combined retry + circuit breaker handler
func NewRetryWithCircuitBreaker(
	retryConfig RetryConfig,
	cbConfig CircuitBreakerConfig,
	log logr.Logger,
) *RetryWithCircuitBreaker {
	return &RetryWithCircuitBreaker{
		retrier:        NewRetrier(retryConfig, log),
		circuitBreaker: NewCircuitBreakerManager(cbConfig, log),
		log:            log,
	}
}

// Execute runs a function with both retry and circuit breaker protection
func (r *RetryWithCircuitBreaker) Execute(
	ctx context.Context,
	backendName string,
	fn RetryableFunc,
) RetryResult {
	// Check circuit breaker first
	if err := r.circuitBreaker.Allow(backendName); err != nil {
		return RetryResult{
			Attempts:  0,
			LastError: err,
		}
	}

	// Execute with retries
	result := r.retrier.Do(ctx, backendName, fn)

	// Update circuit breaker based on result
	if result.LastError != nil {
		r.circuitBreaker.RecordFailure(backendName)
	} else {
		r.circuitBreaker.RecordSuccess(backendName)
	}

	return result
}

// GetCircuitBreaker returns the circuit breaker manager
func (r *RetryWithCircuitBreaker) GetCircuitBreaker() *CircuitBreakerManager {
	return r.circuitBreaker
}

// GetRetrier returns the retrier
func (r *RetryWithCircuitBreaker) GetRetrier() *Retrier {
	return r.retrier
}
