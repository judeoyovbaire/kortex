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
	"sync"
	"time"

	"golang.org/x/time/rate"

	gatewayv1alpha1 "github.com/judeoyovbaire/kortex/api/v1alpha1"
)

// RateLimitResult contains the result of a rate limit check
type RateLimitResult struct {
	Allowed    bool
	Limit      int32
	Remaining  int
	RetryAfter time.Duration
}

// RateLimiter enforces rate limits on a per-route and per-user basis
type RateLimiter struct {
	mu sync.RWMutex

	// routeLimiters stores per-route rate limiters
	routeLimiters map[string]*rate.Limiter

	// userLimiters stores per-user rate limiters (key: "route:user")
	userLimiters map[string]*rate.Limiter

	// cleanupInterval for removing stale user limiters
	cleanupInterval time.Duration

	// userLimiterTTL is how long to keep inactive user limiters
	userLimiterTTL time.Duration

	// lastAccess tracks when each user limiter was last used
	lastAccess map[string]time.Time

	// stopCh signals the cleanup goroutine to stop
	stopCh chan struct{}
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter() *RateLimiter {
	rl := &RateLimiter{
		routeLimiters:   make(map[string]*rate.Limiter),
		userLimiters:    make(map[string]*rate.Limiter),
		lastAccess:      make(map[string]time.Time),
		cleanupInterval: 5 * time.Minute,
		userLimiterTTL:  30 * time.Minute,
		stopCh:          make(chan struct{}),
	}

	// Start background cleanup goroutine
	go rl.cleanupLoop()

	return rl
}

// Stop stops the cleanup goroutine
func (r *RateLimiter) Stop() {
	close(r.stopCh)
}

// Allow checks if a request should be allowed based on rate limits
func (r *RateLimiter) Allow(routeName, userID string, config *gatewayv1alpha1.RateLimitConfig) RateLimitResult {
	// No rate limit configured
	if config == nil || config.RequestsPerMinute <= 0 {
		return RateLimitResult{Allowed: true}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Calculate rate (requests per second)
	rps := float64(config.RequestsPerMinute) / 60.0
	burst := int(config.RequestsPerMinute) // Allow burst up to the per-minute limit

	// Check per-user limit if enabled
	if config.PerUser && userID != "" {
		userKey := routeName + ":" + userID
		limiter := r.getOrCreateLimiter(r.userLimiters, userKey, rps, burst)
		r.lastAccess[userKey] = time.Now()

		if !limiter.Allow() {
			reservation := limiter.Reserve()
			delay := reservation.Delay()
			reservation.Cancel() // Cancel since we're denying

			return RateLimitResult{
				Allowed:    false,
				Limit:      config.RequestsPerMinute,
				Remaining:  0,
				RetryAfter: delay,
			}
		}

		// Calculate remaining tokens (approximate)
		remaining := int(limiter.Tokens())
		return RateLimitResult{
			Allowed:   true,
			Limit:     config.RequestsPerMinute,
			Remaining: remaining,
		}
	}

	// Check per-route limit (always enforced)
	limiter := r.getOrCreateLimiter(r.routeLimiters, routeName, rps, burst)

	if !limiter.Allow() {
		reservation := limiter.Reserve()
		delay := reservation.Delay()
		reservation.Cancel()

		return RateLimitResult{
			Allowed:    false,
			Limit:      config.RequestsPerMinute,
			Remaining:  0,
			RetryAfter: delay,
		}
	}

	remaining := int(limiter.Tokens())
	return RateLimitResult{
		Allowed:   true,
		Limit:     config.RequestsPerMinute,
		Remaining: remaining,
	}
}

// getOrCreateLimiter gets an existing limiter or creates a new one
func (r *RateLimiter) getOrCreateLimiter(limiters map[string]*rate.Limiter, key string, rps float64, burst int) *rate.Limiter {
	limiter, exists := limiters[key]
	if !exists {
		limiter = rate.NewLimiter(rate.Limit(rps), burst)
		limiters[key] = limiter
	}
	return limiter
}

// UpdateRouteLimit updates or creates a rate limiter for a route
func (r *RateLimiter) UpdateRouteLimit(routeName string, config *gatewayv1alpha1.RateLimitConfig) {
	if config == nil || config.RequestsPerMinute <= 0 {
		r.mu.Lock()
		delete(r.routeLimiters, routeName)
		r.mu.Unlock()
		return
	}

	rps := float64(config.RequestsPerMinute) / 60.0
	burst := int(config.RequestsPerMinute)

	r.mu.Lock()
	defer r.mu.Unlock()

	limiter, exists := r.routeLimiters[routeName]
	if exists {
		// Update existing limiter
		limiter.SetLimit(rate.Limit(rps))
		limiter.SetBurst(burst)
	} else {
		r.routeLimiters[routeName] = rate.NewLimiter(rate.Limit(rps), burst)
	}
}

// RemoveRouteLimit removes the rate limiter for a route
func (r *RateLimiter) RemoveRouteLimit(routeName string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.routeLimiters, routeName)

	// Also remove all user limiters for this route
	for key := range r.userLimiters {
		if len(key) > len(routeName) && key[:len(routeName)+1] == routeName+":" {
			delete(r.userLimiters, key)
			delete(r.lastAccess, key)
		}
	}
}

// cleanupLoop periodically removes stale user limiters
func (r *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(r.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.cleanup()
		case <-r.stopCh:
			return
		}
	}
}

// cleanup removes user limiters that haven't been used recently
func (r *RateLimiter) cleanup() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	for key, lastUsed := range r.lastAccess {
		if now.Sub(lastUsed) > r.userLimiterTTL {
			delete(r.userLimiters, key)
			delete(r.lastAccess, key)
		}
	}
}

// GetStats returns current rate limiter statistics
func (r *RateLimiter) GetStats() RateLimiterStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return RateLimiterStats{
		RouteCount: len(r.routeLimiters),
		UserCount:  len(r.userLimiters),
	}
}

// RateLimiterStats contains rate limiter statistics
type RateLimiterStats struct {
	RouteCount int
	UserCount  int
}

// SetDefaultLimit updates the default rate limit for all new routes
// This is called when configuration is hot-reloaded
func (r *RateLimiter) SetDefaultLimit(requestsPerMinute int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if requestsPerMinute <= 0 {
		return
	}

	rps := float64(requestsPerMinute) / 60.0
	burst := requestsPerMinute

	// Update all existing route limiters
	for _, limiter := range r.routeLimiters {
		limiter.SetLimit(rate.Limit(rps))
		limiter.SetBurst(burst)
	}
}
