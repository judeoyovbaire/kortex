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
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestCircuitBreaker_InitialState(t *testing.T) {
	log := zap.New(zap.UseDevMode(true))
	cb := NewCircuitBreaker("test-backend", DefaultCircuitBreakerConfig(), log)

	if cb.State() != StateClosed {
		t.Errorf("expected initial state to be closed, got %v", cb.State())
	}

	stats := cb.Stats()
	if stats.Failures != 0 || stats.Successes != 0 {
		t.Error("expected initial stats to be zero")
	}
}

func TestCircuitBreaker_AllowWhenClosed(t *testing.T) {
	log := zap.New(zap.UseDevMode(true))
	cb := NewCircuitBreaker("test-backend", DefaultCircuitBreakerConfig(), log)

	err := cb.Allow()
	if err != nil {
		t.Errorf("expected no error when circuit is closed, got %v", err)
	}
}

func TestCircuitBreaker_TripsAfterThreshold(t *testing.T) {
	log := zap.New(zap.UseDevMode(true))
	config := CircuitBreakerConfig{
		FailureThreshold:     3,
		SuccessThreshold:     2,
		Timeout:              100 * time.Millisecond,
		HalfOpenMaxRequests:  1,
		FailureRateThreshold: 0,
		MinRequestsForRate:   0,
	}
	cb := NewCircuitBreaker("test-backend", config, log)

	// Record failures up to threshold
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	if cb.State() != StateOpen {
		t.Errorf("expected circuit to be open after %d failures, got %v", 3, cb.State())
	}

	// Requests should be blocked
	err := cb.Allow()
	if err != ErrCircuitOpen {
		t.Errorf("expected ErrCircuitOpen, got %v", err)
	}
}

func TestCircuitBreaker_TransitionsToHalfOpen(t *testing.T) {
	log := zap.New(zap.UseDevMode(true))
	config := CircuitBreakerConfig{
		FailureThreshold:     2,
		SuccessThreshold:     2,
		Timeout:              50 * time.Millisecond,
		HalfOpenMaxRequests:  1,
		FailureRateThreshold: 0,
		MinRequestsForRate:   0,
	}
	cb := NewCircuitBreaker("test-backend", config, log)

	// Trip the circuit
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != StateOpen {
		t.Fatal("circuit should be open")
	}

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// Next request should be allowed (transitions to half-open)
	err := cb.Allow()
	if err != nil {
		t.Errorf("expected request to be allowed after timeout, got %v", err)
	}

	if cb.State() != StateHalfOpen {
		t.Errorf("expected half-open state, got %v", cb.State())
	}
}

func TestCircuitBreaker_ClosesAfterSuccessInHalfOpen(t *testing.T) {
	log := zap.New(zap.UseDevMode(true))
	config := CircuitBreakerConfig{
		FailureThreshold:     2,
		SuccessThreshold:     2,
		Timeout:              50 * time.Millisecond,
		HalfOpenMaxRequests:  3,
		FailureRateThreshold: 0,
		MinRequestsForRate:   0,
	}
	cb := NewCircuitBreaker("test-backend", config, log)

	// Trip and wait
	cb.RecordFailure()
	cb.RecordFailure()
	time.Sleep(60 * time.Millisecond)

	// Transition to half-open
	_ = cb.Allow()

	// Record successes to close circuit
	cb.RecordSuccess()
	cb.RecordSuccess()

	if cb.State() != StateClosed {
		t.Errorf("expected circuit to be closed after successes, got %v", cb.State())
	}
}

func TestCircuitBreaker_OpensAgainOnFailureInHalfOpen(t *testing.T) {
	log := zap.New(zap.UseDevMode(true))
	config := CircuitBreakerConfig{
		FailureThreshold:     2,
		SuccessThreshold:     2,
		Timeout:              50 * time.Millisecond,
		HalfOpenMaxRequests:  3,
		FailureRateThreshold: 0,
		MinRequestsForRate:   0,
	}
	cb := NewCircuitBreaker("test-backend", config, log)

	// Trip and wait
	cb.RecordFailure()
	cb.RecordFailure()
	time.Sleep(60 * time.Millisecond)

	// Transition to half-open
	_ = cb.Allow()
	if cb.State() != StateHalfOpen {
		t.Fatal("expected half-open state")
	}

	// Record failure - should immediately open
	cb.RecordFailure()

	if cb.State() != StateOpen {
		t.Errorf("expected circuit to open on failure in half-open, got %v", cb.State())
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	log := zap.New(zap.UseDevMode(true))
	config := DefaultCircuitBreakerConfig()
	config.FailureThreshold = 2
	cb := NewCircuitBreaker("test-backend", config, log)

	// Trip the circuit
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != StateOpen {
		t.Fatal("circuit should be open")
	}

	// Reset manually
	cb.Reset()

	if cb.State() != StateClosed {
		t.Errorf("expected closed state after reset, got %v", cb.State())
	}

	stats := cb.Stats()
	if stats.Failures != 0 {
		t.Errorf("expected failures to be reset, got %d", stats.Failures)
	}
}

func TestCircuitBreakerManager_GetBreaker(t *testing.T) {
	log := zap.New(zap.UseDevMode(true))
	manager := NewCircuitBreakerManager(DefaultCircuitBreakerConfig(), log)

	cb1 := manager.GetBreaker("backend-1")
	cb2 := manager.GetBreaker("backend-2")
	cb1Again := manager.GetBreaker("backend-1")

	if cb1 == cb2 {
		t.Error("different backends should have different breakers")
	}

	if cb1 != cb1Again {
		t.Error("same backend should return same breaker")
	}
}

func TestCircuitBreakerManager_Allow(t *testing.T) {
	log := zap.New(zap.UseDevMode(true))
	config := DefaultCircuitBreakerConfig()
	config.FailureThreshold = 2
	manager := NewCircuitBreakerManager(config, log)

	// Should allow initially
	err := manager.Allow("test-backend")
	if err != nil {
		t.Errorf("expected Allow to succeed, got %v", err)
	}

	// Trip the circuit
	manager.RecordFailure("test-backend")
	manager.RecordFailure("test-backend")

	// Should block now
	err = manager.Allow("test-backend")
	if err != ErrCircuitOpen {
		t.Errorf("expected ErrCircuitOpen, got %v", err)
	}
}

func TestCircuitBreakerManager_AllStats(t *testing.T) {
	log := zap.New(zap.UseDevMode(true))
	manager := NewCircuitBreakerManager(DefaultCircuitBreakerConfig(), log)

	// Create some breakers
	manager.GetBreaker("backend-1")
	manager.GetBreaker("backend-2")
	manager.RecordSuccess("backend-1")
	manager.RecordFailure("backend-2")

	stats := manager.AllStats()

	if len(stats) != 2 {
		t.Errorf("expected 2 breakers, got %d", len(stats))
	}

	if stats["backend-1"].Successes != 1 {
		t.Errorf("expected 1 success for backend-1, got %d", stats["backend-1"].Successes)
	}

	if stats["backend-2"].Failures != 1 {
		t.Errorf("expected 1 failure for backend-2, got %d", stats["backend-2"].Failures)
	}
}

func TestCircuitBreakerManager_ResetAll(t *testing.T) {
	log := zap.New(zap.UseDevMode(true))
	config := DefaultCircuitBreakerConfig()
	config.FailureThreshold = 2
	manager := NewCircuitBreakerManager(config, log)

	// Trip both circuits
	manager.RecordFailure("backend-1")
	manager.RecordFailure("backend-1")
	manager.RecordFailure("backend-2")
	manager.RecordFailure("backend-2")

	// Both should be open
	if manager.GetBreaker("backend-1").State() != StateOpen {
		t.Error("backend-1 should be open")
	}
	if manager.GetBreaker("backend-2").State() != StateOpen {
		t.Error("backend-2 should be open")
	}

	// Reset all
	manager.ResetAll()

	// Both should be closed
	if manager.GetBreaker("backend-1").State() != StateClosed {
		t.Error("backend-1 should be closed after reset")
	}
	if manager.GetBreaker("backend-2").State() != StateClosed {
		t.Error("backend-2 should be closed after reset")
	}
}

func TestCircuitBreaker_FailureRateThreshold(t *testing.T) {
	log := zap.New(zap.UseDevMode(true))
	config := CircuitBreakerConfig{
		FailureThreshold:     100, // High threshold to not trigger
		SuccessThreshold:     2,
		Timeout:              50 * time.Millisecond,
		HalfOpenMaxRequests:  1,
		FailureRateThreshold: 0.5, // 50% failure rate
		MinRequestsForRate:   4,   // Need at least 4 requests
	}
	cb := NewCircuitBreaker("test-backend", config, log)

	// Record 3 successes and 3 failures (50% failure rate, but only 6 < min)
	cb.RecordSuccess()
	cb.RecordFailure()
	cb.RecordSuccess()
	cb.RecordFailure()

	// Should still be closed (only 4 requests, 50% rate triggers)
	if cb.State() != StateOpen {
		t.Errorf("expected open state due to 50%% failure rate, got %v", cb.State())
	}
}

func TestCircuitState_String(t *testing.T) {
	tests := []struct {
		state    CircuitState
		expected string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half-open"},
		{CircuitState(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.expected {
			t.Errorf("CircuitState(%d).String() = %q, want %q", tt.state, got, tt.expected)
		}
	}
}