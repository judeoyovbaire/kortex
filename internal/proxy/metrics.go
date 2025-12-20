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
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// RequestsTotal counts total requests processed by route, backend, and status
	RequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "inference_gateway_requests_total",
			Help: "Total number of inference requests processed",
		},
		[]string{"route", "backend", "status"},
	)

	// RequestDuration tracks request duration in seconds
	RequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "inference_gateway_request_duration_seconds",
			Help:    "Request duration in seconds",
			Buckets: []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120},
		},
		[]string{"route", "backend"},
	)

	// RequestErrors counts request errors by route, backend, and error type
	RequestErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "inference_gateway_request_errors_total",
			Help: "Total number of request errors",
		},
		[]string{"route", "backend", "error_type"},
	)

	// BackendHealth tracks backend health status (1=healthy, 0=unhealthy)
	BackendHealth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "inference_gateway_backend_health",
			Help: "Backend health status (1=healthy, 0=unhealthy)",
		},
		[]string{"backend", "namespace"},
	)

	// ActiveRequests tracks currently active requests per backend
	ActiveRequests = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "inference_gateway_active_requests",
			Help: "Number of currently active requests",
		},
		[]string{"backend"},
	)

	// RateLimitHits counts rate limit rejections
	RateLimitHits = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "inference_gateway_rate_limit_hits_total",
			Help: "Total number of rate limit rejections",
		},
		[]string{"route", "user"},
	)

	// ExperimentAssignments counts experiment variant assignments
	ExperimentAssignments = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "inference_gateway_experiment_assignments_total",
			Help: "Total number of experiment variant assignments",
		},
		[]string{"experiment", "variant"},
	)

	// CostTotal tracks total cost incurred
	CostTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "inference_gateway_cost_total",
			Help: "Total cost incurred in USD",
		},
		[]string{"route", "backend"},
	)

	// TokensProcessed tracks tokens processed
	TokensProcessed = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "inference_gateway_tokens_total",
			Help: "Total tokens processed",
		},
		[]string{"route", "backend", "type"}, // type: input or output
	)

	// FallbacksTriggered counts fallback chain activations
	FallbacksTriggered = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "inference_gateway_fallbacks_total",
			Help: "Total number of fallback chain activations",
		},
		[]string{"route", "from_backend", "to_backend"},
	)
)

func init() {
	// Register all metrics with controller-runtime's registry
	metrics.Registry.MustRegister(
		RequestsTotal,
		RequestDuration,
		RequestErrors,
		BackendHealth,
		ActiveRequests,
		RateLimitHits,
		ExperimentAssignments,
		CostTotal,
		TokensProcessed,
		FallbacksTriggered,
	)
}

// MetricsRecorder provides methods for recording proxy metrics
type MetricsRecorder struct{}

// NewMetricsRecorder creates a new metrics recorder
func NewMetricsRecorder() *MetricsRecorder {
	return &MetricsRecorder{}
}

// RecordRequest records a completed request
func (m *MetricsRecorder) RecordRequest(route, backend string, statusCode int, duration time.Duration) {
	status := strconv.Itoa(statusCode)
	RequestsTotal.WithLabelValues(route, backend, status).Inc()
	RequestDuration.WithLabelValues(route, backend).Observe(duration.Seconds())
}

// RecordError records a request error
func (m *MetricsRecorder) RecordError(route, backend, errorType string) {
	RequestErrors.WithLabelValues(route, backend, errorType).Inc()
}

// SetBackendHealth sets the health status for a backend
func (m *MetricsRecorder) SetBackendHealth(backend, namespace string, healthy bool) {
	value := 0.0
	if healthy {
		value = 1.0
	}
	BackendHealth.WithLabelValues(backend, namespace).Set(value)
}

// IncActiveRequests increments active requests for a backend
func (m *MetricsRecorder) IncActiveRequests(backend string) {
	ActiveRequests.WithLabelValues(backend).Inc()
}

// DecActiveRequests decrements active requests for a backend
func (m *MetricsRecorder) DecActiveRequests(backend string) {
	ActiveRequests.WithLabelValues(backend).Dec()
}

// RecordRateLimitHit records a rate limit rejection
func (m *MetricsRecorder) RecordRateLimitHit(route, user string) {
	RateLimitHits.WithLabelValues(route, user).Inc()
}

// RecordExperimentAssignment records an experiment variant assignment
func (m *MetricsRecorder) RecordExperimentAssignment(experiment, variant string) {
	ExperimentAssignments.WithLabelValues(experiment, variant).Inc()
}

// RecordCost records cost incurred for a request
func (m *MetricsRecorder) RecordCost(route, backend string, cost float64) {
	CostTotal.WithLabelValues(route, backend).Add(cost)
}

// RecordTokens records tokens processed
func (m *MetricsRecorder) RecordTokens(route, backend string, inputTokens, outputTokens int64) {
	if inputTokens > 0 {
		TokensProcessed.WithLabelValues(route, backend, "input").Add(float64(inputTokens))
	}
	if outputTokens > 0 {
		TokensProcessed.WithLabelValues(route, backend, "output").Add(float64(outputTokens))
	}
}

// RecordFallback records a fallback chain activation
func (m *MetricsRecorder) RecordFallback(route, fromBackend, toBackend string) {
	FallbacksTriggered.WithLabelValues(route, fromBackend, toBackend).Inc()
}
