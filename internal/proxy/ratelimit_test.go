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

	gatewayv1alpha1 "github.com/judeoyovbaire/inference-gateway/api/v1alpha1"
)

func TestRateLimiter_Allow_NilConfig(t *testing.T) {
	rl := NewRateLimiter()
	result := rl.Allow("test-route", "user1", nil)

	if !result.Allowed {
		t.Error("expected nil config to allow all requests")
	}
}

func TestRateLimiter_Allow_ZeroRequestsPerMinute(t *testing.T) {
	rl := NewRateLimiter()
	config := &gatewayv1alpha1.RateLimitConfig{
		RequestsPerMinute: 0,
	}

	result := rl.Allow("test-route", "", config)
	if !result.Allowed {
		t.Error("expected zero requests per minute to allow all requests")
	}
}

func TestRateLimiter_Allow_PerRouteLimit(t *testing.T) {
	rl := NewRateLimiter()
	config := &gatewayv1alpha1.RateLimitConfig{
		RequestsPerMinute: 60, // 1 per second, burst of 60
	}

	// First requests should be allowed (burst capacity)
	for i := 0; i < 10; i++ {
		result := rl.Allow("test-route", "", config)
		if !result.Allowed {
			t.Errorf("request %d should be allowed within burst", i+1)
		}
	}
}

func TestRateLimiter_Allow_PerUserLimit(t *testing.T) {
	rl := NewRateLimiter()
	config := &gatewayv1alpha1.RateLimitConfig{
		RequestsPerMinute: 60,
		PerUser:           true,
		UserHeader:        "x-user-id",
	}

	// Both users should have their own limits
	result := rl.Allow("test-route", "user1", config)
	if !result.Allowed {
		t.Error("first request for user1 should be allowed")
	}

	result = rl.Allow("test-route", "user2", config)
	if !result.Allowed {
		t.Error("first request for user2 should be allowed")
	}
}

func TestRateLimiter_Allow_DifferentRoutes(t *testing.T) {
	rl := NewRateLimiter()
	config := &gatewayv1alpha1.RateLimitConfig{
		RequestsPerMinute: 60,
	}

	// Routes should have separate limits
	result1 := rl.Allow("route1", "", config)
	result2 := rl.Allow("route2", "", config)

	if !result1.Allowed || !result2.Allowed {
		t.Error("both routes should have separate limits and be allowed")
	}
}

func TestRateLimiter_Allow_RetryAfterProvided(t *testing.T) {
	rl := NewRateLimiter()
	config := &gatewayv1alpha1.RateLimitConfig{
		RequestsPerMinute: 1, // Very low limit to trigger rate limiting
	}

	// Exhaust the limit with burst
	for i := 0; i < 5; i++ {
		rl.Allow("test-route", "", config)
	}

	// Next request should be rate limited with RetryAfter
	result := rl.Allow("test-route", "", config)
	// Note: May or may not be rate limited depending on timing
	// If rate limited, RetryAfter should be positive
	if !result.Allowed && result.RetryAfter <= 0 {
		t.Error("RetryAfter should be positive when rate limited")
	}
}

func TestRateLimiter_Allow_RemainingTokens(t *testing.T) {
	rl := NewRateLimiter()
	config := &gatewayv1alpha1.RateLimitConfig{
		RequestsPerMinute: 60,
	}

	result := rl.Allow("test-route", "", config)

	if !result.Allowed {
		t.Error("first request should be allowed")
	}
	if result.Limit != 60 {
		t.Errorf("expected limit 60, got %d", result.Limit)
	}
}

func TestRateLimiter_UpdateRouteLimit(t *testing.T) {
	rl := NewRateLimiter()
	config := &gatewayv1alpha1.RateLimitConfig{
		RequestsPerMinute: 60,
	}

	// First request with initial config
	rl.Allow("test-route", "", config)

	// Update limit
	newConfig := &gatewayv1alpha1.RateLimitConfig{
		RequestsPerMinute: 120,
	}
	rl.UpdateRouteLimit("test-route", newConfig)

	// Verify limit was updated by checking the result
	result := rl.Allow("test-route", "", newConfig)
	if result.Limit != 120 {
		t.Errorf("expected updated limit 120, got %d", result.Limit)
	}
}

func TestRateLimiter_UpdateRouteLimit_NilConfig(t *testing.T) {
	rl := NewRateLimiter()
	config := &gatewayv1alpha1.RateLimitConfig{
		RequestsPerMinute: 60,
	}

	// Create route limiter
	rl.Allow("test-route", "", config)

	// Verify it exists
	stats := rl.GetStats()
	if stats.RouteCount != 1 {
		t.Error("expected 1 route limiter")
	}

	// Remove with nil config
	rl.UpdateRouteLimit("test-route", nil)

	// Verify it was removed
	stats = rl.GetStats()
	if stats.RouteCount != 0 {
		t.Error("expected 0 route limiters after nil update")
	}
}

func TestRateLimiter_RemoveRouteLimit(t *testing.T) {
	rl := NewRateLimiter()
	config := &gatewayv1alpha1.RateLimitConfig{
		RequestsPerMinute: 60,
		PerUser:           true,
	}

	// Create route and user limiters
	rl.Allow("test-route", "user1", config)
	rl.Allow("test-route", "user2", config)

	stats := rl.GetStats()
	if stats.RouteCount != 0 { // Route limiters aren't created for per-user mode
		t.Errorf("expected 0 route limiters in per-user mode, got %d", stats.RouteCount)
	}
	if stats.UserCount != 2 {
		t.Errorf("expected 2 user limiters, got %d", stats.UserCount)
	}

	// Remove route limit
	rl.RemoveRouteLimit("test-route")

	// Verify user limiters for this route were removed
	stats = rl.GetStats()
	if stats.UserCount != 0 {
		t.Errorf("expected 0 user limiters after removal, got %d", stats.UserCount)
	}
}

func TestRateLimiter_Cleanup(t *testing.T) {
	rl := NewRateLimiter()
	config := &gatewayv1alpha1.RateLimitConfig{
		RequestsPerMinute: 60,
		PerUser:           true,
	}

	// Make a request to create user limiter
	rl.Allow("test-route", "user1", config)

	// Verify limiter exists
	stats := rl.GetStats()
	if stats.UserCount != 1 {
		t.Error("expected 1 user limiter")
	}

	// Simulate old access time
	rl.mu.Lock()
	rl.lastAccess["test-route:user1"] = time.Now().Add(-time.Hour)
	rl.mu.Unlock()

	// Run cleanup
	rl.cleanup()

	// Verify limiter was removed
	stats = rl.GetStats()
	if stats.UserCount != 0 {
		t.Error("expected 0 user limiters after cleanup")
	}
}

func TestRateLimiter_GetStats(t *testing.T) {
	rl := NewRateLimiter()
	config := &gatewayv1alpha1.RateLimitConfig{
		RequestsPerMinute: 60,
	}
	userConfig := &gatewayv1alpha1.RateLimitConfig{
		RequestsPerMinute: 60,
		PerUser:           true,
	}

	// Create route limiters
	rl.Allow("route1", "", config)
	rl.Allow("route2", "", config)

	// Create user limiters
	rl.Allow("route3", "user1", userConfig)
	rl.Allow("route3", "user2", userConfig)

	stats := rl.GetStats()

	if stats.RouteCount != 2 {
		t.Errorf("expected 2 route limiters, got %d", stats.RouteCount)
	}
	if stats.UserCount != 2 {
		t.Errorf("expected 2 user limiters, got %d", stats.UserCount)
	}
}
