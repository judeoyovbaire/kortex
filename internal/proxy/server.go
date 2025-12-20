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
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/judeoyovbaire/inference-gateway/internal/cache"
)

// Config holds proxy server configuration
type Config struct {
	// Addr is the address to bind the proxy server (e.g., ":8080")
	Addr string

	// ReadTimeout is the maximum duration for reading the entire request
	ReadTimeout time.Duration

	// WriteTimeout is the maximum duration before timing out writes of the response
	WriteTimeout time.Duration

	// IdleTimeout is the maximum amount of time to wait for the next request
	IdleTimeout time.Duration

	// ShutdownTimeout is the maximum duration to wait for active connections to close
	ShutdownTimeout time.Duration

	// MaxRequestBodySize is the maximum allowed request body size in bytes (0 = no limit)
	MaxRequestBodySize int64
}

// DefaultConfig returns the default proxy configuration
func DefaultConfig() Config {
	return Config{
		Addr:               ":8080",
		ReadTimeout:        30 * time.Second,
		WriteTimeout:       120 * time.Second, // Long timeout for LLM responses
		IdleTimeout:        120 * time.Second,
		ShutdownTimeout:    30 * time.Second,
		MaxRequestBodySize: 10 * 1024 * 1024, // 10MB default limit for LLM requests
	}
}

// Server is the embedded reverse proxy that routes inference requests
type Server struct {
	config      Config
	cache       *cache.Store
	router      *Router
	httpServer  *http.Server
	log         logr.Logger
	client      client.Client
	metrics     *MetricsRecorder
	rateLimiter *RateLimiter
	experiments *ExperimentManager
	costTracker *CostTracker
}

// ServerOption is a functional option for configuring the server
type ServerOption func(*Server)

// WithMetrics adds metrics recording to the server
func WithMetrics(m *MetricsRecorder) ServerOption {
	return func(s *Server) {
		s.metrics = m
	}
}

// WithRateLimiter adds rate limiting to the server
func WithRateLimiter(rl *RateLimiter) ServerOption {
	return func(s *Server) {
		s.rateLimiter = rl
	}
}

// WithExperiments adds A/B experiment support to the server
func WithExperiments(em *ExperimentManager) ServerOption {
	return func(s *Server) {
		s.experiments = em
	}
}

// WithCostTracker adds cost tracking to the server
func WithCostTracker(ct *CostTracker) ServerOption {
	return func(s *Server) {
		s.costTracker = ct
	}
}

// NewServer creates a new proxy server
func NewServer(cfg Config, store *cache.Store, k8sClient client.Client, log logr.Logger, opts ...ServerOption) *Server {
	s := &Server{
		config: cfg,
		cache:  store,
		client: k8sClient,
		log:    log.WithName("proxy-server"),
	}

	// Apply options
	for _, opt := range opts {
		opt(s)
	}

	// Create the router with backend handler and optional features
	s.router = NewRouter(store, k8sClient, log,
		WithRouterMetrics(s.metrics),
		WithRouterExperiments(s.experiments),
		WithRouterCostTracker(s.costTracker),
	)

	// Create the HTTP server
	s.httpServer = &http.Server{
		Addr:         cfg.Addr,
		Handler:      s,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	return s
}

// ServeHTTP implements http.Handler
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Check request body size limit
	if s.config.MaxRequestBodySize > 0 && r.ContentLength > s.config.MaxRequestBodySize {
		http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
		s.log.V(1).Info("Request rejected: body too large",
			"content_length", r.ContentLength,
			"max_size", s.config.MaxRequestBodySize,
		)
		return
	}

	// Wrap the body with a size limiter to catch chunked transfers
	if s.config.MaxRequestBodySize > 0 && r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, s.config.MaxRequestBodySize)
	}

	// Find the route first for rate limiting
	route := s.router.FindRoute(r)

	// Apply rate limiting if configured
	if route != nil && route.Spec.RateLimit != nil && s.rateLimiter != nil {
		userHeader := route.Spec.RateLimit.UserHeader
		if userHeader == "" {
			userHeader = "x-user-id"
		}
		userID := r.Header.Get(userHeader)

		result := s.rateLimiter.Allow(route.Name, userID, route.Spec.RateLimit)
		if !result.Allowed {
			// Record rate limit hit
			if s.metrics != nil {
				s.metrics.RecordRateLimitHit(route.Name, userID)
			}

			// Set rate limit headers
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(int(result.Limit)))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(result.Remaining))
			w.Header().Set("Retry-After", strconv.Itoa(int(result.RetryAfter.Seconds())+1))

			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			s.log.V(1).Info("Rate limit exceeded",
				"route", route.Name,
				"user", userID,
				"retry_after", result.RetryAfter,
			)
			return
		}

		// Set rate limit headers for successful requests too
		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(int(result.Limit)))
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(result.Remaining))
	}

	// Handle the request through the router
	s.router.HandleRequest(r.Context(), w, r)

	// Log the request
	duration := time.Since(start)
	s.log.V(1).Info("Handled request",
		"method", r.Method,
		"path", r.URL.Path,
		"duration", duration.String(),
		"remote", r.RemoteAddr,
	)
}

// Start begins serving requests. This implements manager.Runnable.
func (s *Server) Start(ctx context.Context) error {
	s.log.Info("Starting inference proxy server", "addr", s.config.Addr)

	// Channel for server errors
	errCh := make(chan error, 1)

	// Start the HTTP server in a goroutine
	go func() {
		if err := s.httpServer.ListenAndServe(); err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Wait for either context cancellation or server error
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		s.log.Info("Shutting down inference proxy server")

		// Stop the rate limiter cleanup goroutine
		if s.rateLimiter != nil {
			s.rateLimiter.Stop()
		}

		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.config.ShutdownTimeout)
		defer cancel()
		return s.httpServer.Shutdown(shutdownCtx)
	}
}

// NeedLeaderElection returns false since the proxy should run on all replicas
// This implements manager.LeaderElectionRunnable
func (s *Server) NeedLeaderElection() bool {
	return false
}

// HealthHandler returns a simple health check handler for the proxy
func (s *Server) HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats := s.cache.GetStats()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		response := fmt.Sprintf(`{"status":"ok","routes":%d,"backends":%d}`, stats.RouteCount, stats.BackendCount)
		_, _ = w.Write([]byte(response))
	}
}

// GetMetrics returns the metrics recorder
func (s *Server) GetMetrics() *MetricsRecorder {
	return s.metrics
}

// GetRateLimiter returns the rate limiter
func (s *Server) GetRateLimiter() *RateLimiter {
	return s.rateLimiter
}

// GetCostTracker returns the cost tracker
func (s *Server) GetCostTracker() *CostTracker {
	return s.costTracker
}
