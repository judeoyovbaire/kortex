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
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// CircuitState represents the current state of a circuit breaker
type CircuitState int

const (
	// StateClosed is the normal state - requests pass through
	StateClosed CircuitState = iota
	// StateOpen is the failure state - requests fail fast
	StateOpen
	// StateHalfOpen allows limited requests to test recovery
	StateHalfOpen
)

func (s CircuitState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

var (
	// ErrCircuitOpen is returned when the circuit is open
	ErrCircuitOpen = errors.New("circuit breaker is open")

	// ErrTooManyRequests is returned when too many requests are in half-open state
	ErrTooManyRequests = errors.New("too many requests in half-open state")

	// Prometheus metrics for circuit breaker
	circuitBreakerState = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kortex_circuit_breaker_state",
			Help: "Current state of circuit breaker (0=closed, 1=open, 2=half-open)",
		},
		[]string{"backend"},
	)

	circuitBreakerTrips = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kortex_circuit_breaker_trips_total",
			Help: "Total number of times the circuit breaker has tripped open",
		},
		[]string{"backend"},
	)

	circuitBreakerSuccesses = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kortex_circuit_breaker_successes_total",
			Help: "Total successful requests through circuit breaker",
		},
		[]string{"backend"},
	)

	circuitBreakerFailures = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kortex_circuit_breaker_failures_total",
			Help: "Total failed requests through circuit breaker",
		},
		[]string{"backend"},
	)

	circuitBreakerRejections = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kortex_circuit_breaker_rejections_total",
			Help: "Total requests rejected by open circuit breaker",
		},
		[]string{"backend"},
	)
)

// CircuitBreakerConfig holds configuration for the circuit breaker
type CircuitBreakerConfig struct {
	// FailureThreshold is the number of failures before opening the circuit
	FailureThreshold int

	// SuccessThreshold is the number of successes in half-open before closing
	SuccessThreshold int

	// Timeout is how long the circuit stays open before transitioning to half-open
	Timeout time.Duration

	// HalfOpenMaxRequests is the max concurrent requests allowed in half-open state
	HalfOpenMaxRequests int

	// FailureRateThreshold is the failure rate (0.0-1.0) that triggers opening (if > 0)
	FailureRateThreshold float64

	// MinRequestsForRate is minimum requests before rate-based threshold applies
	MinRequestsForRate int
}

// DefaultCircuitBreakerConfig returns sensible defaults
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold:     5,
		SuccessThreshold:     3,
		Timeout:              30 * time.Second,
		HalfOpenMaxRequests:  3,
		FailureRateThreshold: 0.5, // 50% failure rate
		MinRequestsForRate:   10,
	}
}

// CircuitBreaker implements the circuit breaker pattern for a single backend
type CircuitBreaker struct {
	name   string
	config CircuitBreakerConfig
	log    logr.Logger

	mu                  sync.RWMutex
	state               CircuitState
	failures            int
	successes           int
	consecutiveFailures int
	consecutiveSuccesses int
	totalRequests       int
	lastFailure         time.Time
	openedAt            time.Time
	halfOpenRequests    int
}

// NewCircuitBreaker creates a new circuit breaker for a backend
func NewCircuitBreaker(name string, config CircuitBreakerConfig, log logr.Logger) *CircuitBreaker {
	cb := &CircuitBreaker{
		name:   name,
		config: config,
		log:    log.WithName("circuit-breaker").WithValues("backend", name),
		state:  StateClosed,
	}

	// Initialize metrics
	circuitBreakerState.WithLabelValues(name).Set(float64(StateClosed))

	return cb
}

// Allow checks if a request should be allowed through
func (cb *CircuitBreaker) Allow() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()

	switch cb.state {
	case StateClosed:
		return nil

	case StateOpen:
		// Check if timeout has passed
		if now.Sub(cb.openedAt) >= cb.config.Timeout {
			cb.transitionTo(StateHalfOpen)
			cb.halfOpenRequests = 1
			return nil
		}
		circuitBreakerRejections.WithLabelValues(cb.name).Inc()
		return ErrCircuitOpen

	case StateHalfOpen:
		// Limit concurrent requests in half-open state
		if cb.halfOpenRequests >= cb.config.HalfOpenMaxRequests {
			circuitBreakerRejections.WithLabelValues(cb.name).Inc()
			return ErrTooManyRequests
		}
		cb.halfOpenRequests++
		return nil
	}

	return nil
}

// RecordSuccess records a successful request
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.successes++
	cb.totalRequests++
	cb.consecutiveSuccesses++
	cb.consecutiveFailures = 0

	circuitBreakerSuccesses.WithLabelValues(cb.name).Inc()

	switch cb.state {
	case StateHalfOpen:
		cb.halfOpenRequests--
		if cb.consecutiveSuccesses >= cb.config.SuccessThreshold {
			cb.transitionTo(StateClosed)
		}
	case StateClosed:
		// Reset failure count on success
		cb.consecutiveFailures = 0
	}
}

// RecordFailure records a failed request
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.totalRequests++
	cb.consecutiveFailures++
	cb.consecutiveSuccesses = 0
	cb.lastFailure = time.Now()

	circuitBreakerFailures.WithLabelValues(cb.name).Inc()

	switch cb.state {
	case StateClosed:
		// Check if we should trip the circuit
		if cb.shouldTrip() {
			cb.transitionTo(StateOpen)
		}

	case StateHalfOpen:
		cb.halfOpenRequests--
		// Any failure in half-open immediately opens the circuit
		cb.transitionTo(StateOpen)
	}
}

// shouldTrip determines if the circuit should trip open
func (cb *CircuitBreaker) shouldTrip() bool {
	// Consecutive failure threshold
	if cb.consecutiveFailures >= cb.config.FailureThreshold {
		return true
	}

	// Rate-based threshold (if configured and enough requests)
	if cb.config.FailureRateThreshold > 0 && cb.totalRequests >= cb.config.MinRequestsForRate {
		failureRate := float64(cb.failures) / float64(cb.totalRequests)
		if failureRate >= cb.config.FailureRateThreshold {
			return true
		}
	}

	return false
}

// transitionTo changes the circuit breaker state
func (cb *CircuitBreaker) transitionTo(newState CircuitState) {
	oldState := cb.state
	cb.state = newState

	// Update metrics
	circuitBreakerState.WithLabelValues(cb.name).Set(float64(newState))

	switch newState {
	case StateOpen:
		cb.openedAt = time.Now()
		circuitBreakerTrips.WithLabelValues(cb.name).Inc()
		cb.log.Info("Circuit breaker opened",
			"previousState", oldState.String(),
			"consecutiveFailures", cb.consecutiveFailures,
			"timeout", cb.config.Timeout,
		)

	case StateHalfOpen:
		cb.halfOpenRequests = 0
		cb.consecutiveSuccesses = 0
		cb.log.Info("Circuit breaker half-open",
			"previousState", oldState.String(),
		)

	case StateClosed:
		// Reset counters
		cb.failures = 0
		cb.successes = 0
		cb.totalRequests = 0
		cb.consecutiveFailures = 0
		cb.consecutiveSuccesses = 0
		cb.log.Info("Circuit breaker closed",
			"previousState", oldState.String(),
		)
	}
}

// State returns the current state of the circuit breaker
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Stats returns current circuit breaker statistics
type CircuitBreakerStats struct {
	State                CircuitState
	Failures             int
	Successes            int
	ConsecutiveFailures  int
	ConsecutiveSuccesses int
	TotalRequests        int
	LastFailure          time.Time
	OpenedAt             time.Time
}

// Stats returns the current statistics
func (cb *CircuitBreaker) Stats() CircuitBreakerStats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return CircuitBreakerStats{
		State:                cb.state,
		Failures:             cb.failures,
		Successes:            cb.successes,
		ConsecutiveFailures:  cb.consecutiveFailures,
		ConsecutiveSuccesses: cb.consecutiveSuccesses,
		TotalRequests:        cb.totalRequests,
		LastFailure:          cb.lastFailure,
		OpenedAt:             cb.openedAt,
	}
}

// Reset manually resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.transitionTo(StateClosed)
	cb.log.Info("Circuit breaker manually reset")
}

// CircuitBreakerManager manages circuit breakers for multiple backends
type CircuitBreakerManager struct {
	breakers map[string]*CircuitBreaker
	config   CircuitBreakerConfig
	log      logr.Logger
	mu       sync.RWMutex
}

// NewCircuitBreakerManager creates a new circuit breaker manager
func NewCircuitBreakerManager(config CircuitBreakerConfig, log logr.Logger) *CircuitBreakerManager {
	return &CircuitBreakerManager{
		breakers: make(map[string]*CircuitBreaker),
		config:   config,
		log:      log,
	}
}

// GetBreaker returns the circuit breaker for a backend, creating one if needed
func (m *CircuitBreakerManager) GetBreaker(backendName string) *CircuitBreaker {
	m.mu.RLock()
	cb, exists := m.breakers[backendName]
	m.mu.RUnlock()

	if exists {
		return cb
	}

	// Create new circuit breaker
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if cb, exists = m.breakers[backendName]; exists {
		return cb
	}

	cb = NewCircuitBreaker(backendName, m.config, m.log)
	m.breakers[backendName] = cb

	return cb
}

// Allow checks if a request to the backend should be allowed
func (m *CircuitBreakerManager) Allow(backendName string) error {
	return m.GetBreaker(backendName).Allow()
}

// RecordSuccess records a successful request to a backend
func (m *CircuitBreakerManager) RecordSuccess(backendName string) {
	m.GetBreaker(backendName).RecordSuccess()
}

// RecordFailure records a failed request to a backend
func (m *CircuitBreakerManager) RecordFailure(backendName string) {
	m.GetBreaker(backendName).RecordFailure()
}

// AllStats returns stats for all circuit breakers
func (m *CircuitBreakerManager) AllStats() map[string]CircuitBreakerStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[string]CircuitBreakerStats, len(m.breakers))
	for name, cb := range m.breakers {
		stats[name] = cb.Stats()
	}
	return stats
}

// ResetAll resets all circuit breakers
func (m *CircuitBreakerManager) ResetAll() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, cb := range m.breakers {
		cb.Reset()
	}
}

// Execute runs a function with circuit breaker protection
func (m *CircuitBreakerManager) Execute(ctx context.Context, backendName string, fn func() error) error {
	// Check if request is allowed
	if err := m.Allow(backendName); err != nil {
		return err
	}

	// Execute the function
	err := fn()

	// Record the result
	if err != nil {
		m.RecordFailure(backendName)
	} else {
		m.RecordSuccess(backendName)
	}

	return err
}
