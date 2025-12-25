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
	"math/rand"
	"net/http"
	"path"
	"strings"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gatewayv1alpha1 "github.com/judeoyovbaire/kortex/api/v1alpha1"
	"github.com/judeoyovbaire/kortex/internal/cache"
	"github.com/judeoyovbaire/kortex/internal/tracing"
)

// Router handles request routing to backends
type Router struct {
	cache       *cache.Store
	handler     *BackendHandler
	log         logr.Logger
	metrics     *MetricsRecorder
	experiments *ExperimentManager
	costTracker *CostTracker
	tracer      *tracing.Tracer
}

// RouterOption is a functional option for configuring the router
type RouterOption func(*Router)

// WithRouterMetrics adds metrics to the router
func WithRouterMetrics(m *MetricsRecorder) RouterOption {
	return func(r *Router) {
		r.metrics = m
	}
}

// WithRouterExperiments adds experiment support to the router
func WithRouterExperiments(em *ExperimentManager) RouterOption {
	return func(r *Router) {
		r.experiments = em
	}
}

// WithRouterCostTracker adds cost tracking to the router
func WithRouterCostTracker(ct *CostTracker) RouterOption {
	return func(r *Router) {
		r.costTracker = ct
	}
}

// WithRouterTracer adds OpenTelemetry tracing to the router
func WithRouterTracer(t *tracing.Tracer) RouterOption {
	return func(r *Router) {
		r.tracer = t
	}
}

// NewRouter creates a new router
func NewRouter(store *cache.Store, k8sClient client.Client, log logr.Logger, opts ...RouterOption) *Router {
	r := &Router{
		cache: store,
		log:   log.WithName("router"),
	}

	// Apply options
	for _, opt := range opts {
		opt(r)
	}

	// Create handler with metrics, cost tracker, and tracer
	r.handler = NewBackendHandler(store, k8sClient, log, r.metrics, r.costTracker, r.tracer)

	return r
}

// FindRoute finds the route that should handle this request (exported for rate limiting)
func (r *Router) FindRoute(req *http.Request) *gatewayv1alpha1.InferenceRoute {
	route, _ := r.findMatchingRoute(req)
	return route
}

// HandleRequest processes an incoming request and routes it to the appropriate backend
func (r *Router) HandleRequest(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	// Find matching route
	route, matched := r.findMatchingRoute(req)
	if !matched {
		r.log.V(1).Info("No matching route found",
			"namespace", req.Header.Get("X-Namespace"),
			"route", req.Header.Get("X-Route"),
		)
		http.Error(w, "No matching route found", http.StatusNotFound)
		return
	}

	// Start router span if tracing is enabled
	if r.tracer != nil {
		var span trace.Span
		ctx, span = r.tracer.StartRouterSpan(ctx, route.Name)
		defer span.End()
		span.SetAttributes(
			attribute.String("kortex.route.namespace", route.Namespace),
			attribute.String("kortex.route.phase", string(route.Status.Phase)),
		)
	}

	// Check route phase
	if route.Status.Phase == "Failed" {
		r.log.Info("Route is in failed state", "route", route.Name)
		http.Error(w, "Route is not operational", http.StatusServiceUnavailable)
		return
	}

	// Find matching rule within the route
	rule := r.matchRule(route, req)

	// Determine which backends to use
	var backends []gatewayv1alpha1.BackendRef
	if rule != nil {
		backends = rule.Backends
	} else if route.Spec.DefaultBackend != nil {
		backends = []gatewayv1alpha1.BackendRef{*route.Spec.DefaultBackend}
	} else {
		r.log.Info("No backend configured for route", "route", route.Name)
		http.Error(w, "No backend configured for this route", http.StatusServiceUnavailable)
		return
	}

	// Select backend using weighted selection if multiple backends
	selectedBackend := r.selectWeightedBackend(backends)

	// Apply A/B experiment if configured
	var experimentResult *ExperimentResult
	if len(route.Spec.Experiments) > 0 && r.experiments != nil {
		newBackend, result := r.experiments.ApplyExperiment(route.Spec.Experiments, selectedBackend.Name, req)
		if result != nil {
			selectedBackend.Name = newBackend
			experimentResult = result
			// Set experiment headers
			r.experiments.SetExperimentHeaders(w, result)
		}
	}

	r.log.V(1).Info("Routing request",
		"route", route.Name,
		"backend", selectedBackend.Name,
		"hasRule", rule != nil,
		"experiment", experimentResult != nil,
	)

	// Execute request with fallback support
	r.handler.ExecuteWithFallback(ctx, w, req, route, selectedBackend)
}

// findMatchingRoute finds the route that should handle this request
func (r *Router) findMatchingRoute(req *http.Request) (*gatewayv1alpha1.InferenceRoute, bool) {
	// Check for explicit route selection via header
	routeName := req.Header.Get("X-Route")
	namespace := req.Header.Get("X-Namespace")
	if namespace == "" {
		namespace = "default"
	}

	// If a specific route is requested, try to find it
	if routeName != "" {
		route, ok := r.cache.GetRoute(types.NamespacedName{
			Namespace: namespace,
			Name:      routeName,
		})
		if ok {
			return route, true
		}
		// If specific route requested but not found, don't fall back
		return nil, false
	}

	// Otherwise, find any active route in the namespace
	routes := r.cache.ListRoutesInNamespace(namespace)
	for _, route := range routes {
		// Skip routes that are not operational
		if route.Status.Phase == "Failed" || route.Status.Phase == "Pending" {
			continue
		}
		return route, true
	}

	return nil, false
}

// matchRule finds the first matching rule in the route
func (r *Router) matchRule(route *gatewayv1alpha1.InferenceRoute, req *http.Request) *gatewayv1alpha1.RouteRule {
	for i := range route.Spec.Rules {
		rule := &route.Spec.Rules[i]
		if r.ruleMatches(rule, req) {
			return rule
		}
	}
	return nil
}

// ruleMatches checks if a rule matches the given request
func (r *Router) ruleMatches(rule *gatewayv1alpha1.RouteRule, req *http.Request) bool {
	// No match conditions means match all
	if rule.Match == nil {
		return true
	}

	match := rule.Match

	// Check header matching
	for key, value := range match.Headers {
		if req.Header.Get(key) != value {
			return false
		}
	}

	// Check path prefix matching
	if match.PathPrefix != nil && *match.PathPrefix != "" {
		if !strings.HasPrefix(req.URL.Path, *match.PathPrefix) {
			return false
		}
	}

	// Check model pattern matching
	// The model can be specified via X-Model header to avoid parsing request body
	if match.ModelPattern != nil && *match.ModelPattern != "" {
		modelHeader := req.Header.Get("X-Model")
		if modelHeader != "" {
			matched, err := path.Match(*match.ModelPattern, modelHeader)
			if err != nil || !matched {
				return false
			}
		}
	}

	return true
}

// selectWeightedBackend selects a backend from a list using weighted random selection
func (r *Router) selectWeightedBackend(backends []gatewayv1alpha1.BackendRef) gatewayv1alpha1.BackendRef {
	if len(backends) == 0 {
		return gatewayv1alpha1.BackendRef{}
	}

	if len(backends) == 1 {
		return backends[0]
	}

	// Calculate total weight
	totalWeight := int32(0)
	for _, b := range backends {
		weight := b.Weight
		if weight == 0 {
			weight = 100 // default weight
		}
		totalWeight += weight
	}

	if totalWeight == 0 {
		return backends[0]
	}

	// Random selection based on weight
	target := rand.Int31n(totalWeight)
	cumulative := int32(0)

	for _, b := range backends {
		weight := b.Weight
		if weight == 0 {
			weight = 100
		}
		cumulative += weight
		if target < cumulative {
			return b
		}
	}

	// Fallback to first backend (should not reach here)
	return backends[0]
}
