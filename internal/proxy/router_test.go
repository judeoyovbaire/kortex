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
	"net/http/httptest"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	gatewayv1alpha1 "github.com/judeoyovbaire/kortex/api/v1alpha1"
	"github.com/judeoyovbaire/kortex/internal/cache"
)

func TestRouter_FindRoute_WithExplicitHeader(t *testing.T) {
	store := cache.NewStore()
	log := zap.New()
	router := NewRouter(store, nil, log)

	// Add a route to the cache
	route := &gatewayv1alpha1.InferenceRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
		Status: gatewayv1alpha1.InferenceRouteStatus{
			Phase: "Active",
		},
	}
	store.SetRoute(types.NamespacedName{Namespace: "default", Name: "test-route"}, route)

	// Request with explicit route header
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.Header.Set("X-Route", "test-route")
	req.Header.Set("X-Namespace", "default")

	found := router.FindRoute(req)

	if found == nil {
		t.Fatal("expected to find route")
	}
	if found.Name != "test-route" {
		t.Errorf("expected route 'test-route', got '%s'", found.Name)
	}
}

func TestRouter_FindRoute_DefaultNamespace(t *testing.T) {
	store := cache.NewStore()
	log := zap.New()
	router := NewRouter(store, nil, log)

	route := &gatewayv1alpha1.InferenceRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
		Status: gatewayv1alpha1.InferenceRouteStatus{
			Phase: "Active",
		},
	}
	store.SetRoute(types.NamespacedName{Namespace: "default", Name: "test-route"}, route)

	// Request without namespace header (should default to "default")
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.Header.Set("X-Route", "test-route")

	found := router.FindRoute(req)

	if found == nil {
		t.Fatal("expected to find route in default namespace")
	}
}

func TestRouter_FindRoute_AnyActiveRoute(t *testing.T) {
	store := cache.NewStore()
	log := zap.New()
	router := NewRouter(store, nil, log)

	// Add an active route
	route := &gatewayv1alpha1.InferenceRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "any-route",
			Namespace: "default",
		},
		Status: gatewayv1alpha1.InferenceRouteStatus{
			Phase: "Active",
		},
	}
	store.SetRoute(types.NamespacedName{Namespace: "default", Name: "any-route"}, route)

	// Request without specific route header
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)

	found := router.FindRoute(req)

	if found == nil {
		t.Fatal("expected to find any active route")
	}
}

func TestRouter_FindRoute_SkipsFailedRoutes(t *testing.T) {
	store := cache.NewStore()
	log := zap.New()
	router := NewRouter(store, nil, log)

	// Add a failed route
	failedRoute := &gatewayv1alpha1.InferenceRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "failed-route",
			Namespace: "default",
		},
		Status: gatewayv1alpha1.InferenceRouteStatus{
			Phase: "Failed",
		},
	}
	store.SetRoute(types.NamespacedName{Namespace: "default", Name: "failed-route"}, failedRoute)

	// Add an active route
	activeRoute := &gatewayv1alpha1.InferenceRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "active-route",
			Namespace: "default",
		},
		Status: gatewayv1alpha1.InferenceRouteStatus{
			Phase: "Active",
		},
	}
	store.SetRoute(types.NamespacedName{Namespace: "default", Name: "active-route"}, activeRoute)

	// Request without specific route
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)

	found := router.FindRoute(req)

	if found == nil {
		t.Fatal("expected to find active route")
	}
	if found.Status.Phase == "Failed" {
		t.Error("should not return failed route")
	}
}

func TestRouter_FindRoute_NotFound(t *testing.T) {
	store := cache.NewStore()
	log := zap.New()
	router := NewRouter(store, nil, log)

	// Request for non-existent route
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.Header.Set("X-Route", "non-existent")

	found := router.FindRoute(req)

	if found != nil {
		t.Error("expected nil for non-existent route")
	}
}

func TestRouter_selectWeightedBackend_Single(t *testing.T) {
	store := cache.NewStore()
	log := zap.New()
	router := NewRouter(store, nil, log)

	backends := []gatewayv1alpha1.BackendRef{
		{Name: "only-backend", Weight: 100},
	}

	selected := router.selectWeightedBackend(backends)

	if selected.Name != "only-backend" {
		t.Errorf("expected 'only-backend', got '%s'", selected.Name)
	}
}

func TestRouter_selectWeightedBackend_Empty(t *testing.T) {
	store := cache.NewStore()
	log := zap.New()
	router := NewRouter(store, nil, log)

	selected := router.selectWeightedBackend(nil)

	if selected.Name != "" {
		t.Errorf("expected empty backend for nil slice, got '%s'", selected.Name)
	}
}

func TestRouter_selectWeightedBackend_DefaultWeight(t *testing.T) {
	store := cache.NewStore()
	log := zap.New()
	router := NewRouter(store, nil, log)

	// Backends without explicit weights should get default weight of 100
	backends := []gatewayv1alpha1.BackendRef{
		{Name: "backend-a"},
		{Name: "backend-b"},
	}

	// Run multiple selections to verify both can be selected
	selections := make(map[string]int)
	for i := 0; i < 100; i++ {
		selected := router.selectWeightedBackend(backends)
		selections[selected.Name]++
	}

	if len(selections) < 2 {
		t.Error("expected both backends to be selected at least once")
	}
}

func TestRouter_selectWeightedBackend_WeightedDistribution(t *testing.T) {
	store := cache.NewStore()
	log := zap.New()
	router := NewRouter(store, nil, log)

	// 90% to backend-a, 10% to backend-b
	backends := []gatewayv1alpha1.BackendRef{
		{Name: "backend-a", Weight: 90},
		{Name: "backend-b", Weight: 10},
	}

	selections := make(map[string]int)
	iterations := 1000
	for i := 0; i < iterations; i++ {
		selected := router.selectWeightedBackend(backends)
		selections[selected.Name]++
	}

	// backend-a should be selected roughly 90% of the time
	ratioA := float64(selections["backend-a"]) / float64(iterations)
	if ratioA < 0.80 || ratioA > 0.98 {
		t.Errorf("expected backend-a to be selected ~90%%, got %.1f%%", ratioA*100)
	}
}

func TestRouter_ruleMatches_NoMatchConditions(t *testing.T) {
	store := cache.NewStore()
	log := zap.New()
	router := NewRouter(store, nil, log)

	rule := &gatewayv1alpha1.RouteRule{
		Match: nil, // No match conditions = match all
		Backends: []gatewayv1alpha1.BackendRef{
			{Name: "backend"},
		},
	}

	req := httptest.NewRequest("POST", "/any/path", nil)
	matches := router.ruleMatches(rule, req)

	if !matches {
		t.Error("expected rule with no conditions to match all")
	}
}

func TestRouter_ruleMatches_HeaderMatch(t *testing.T) {
	store := cache.NewStore()
	log := zap.New()
	router := NewRouter(store, nil, log)

	rule := &gatewayv1alpha1.RouteRule{
		Match: &gatewayv1alpha1.RouteMatch{
			Headers: map[string]string{
				"x-user-tier": "premium",
			},
		},
	}

	tests := []struct {
		name    string
		headers map[string]string
		matches bool
	}{
		{
			name:    "matching header",
			headers: map[string]string{"x-user-tier": "premium"},
			matches: true,
		},
		{
			name:    "wrong header value",
			headers: map[string]string{"x-user-tier": "basic"},
			matches: false,
		},
		{
			name:    "missing header",
			headers: map[string]string{},
			matches: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			result := router.ruleMatches(rule, req)
			if result != tt.matches {
				t.Errorf("expected %v, got %v", tt.matches, result)
			}
		})
	}
}

func TestRouter_ruleMatches_PathPrefix(t *testing.T) {
	store := cache.NewStore()
	log := zap.New()
	router := NewRouter(store, nil, log)

	pathPrefix := "/v1/chat"
	rule := &gatewayv1alpha1.RouteRule{
		Match: &gatewayv1alpha1.RouteMatch{
			PathPrefix: &pathPrefix,
		},
	}

	tests := []struct {
		name    string
		path    string
		matches bool
	}{
		{
			name:    "matching prefix",
			path:    "/v1/chat/completions",
			matches: true,
		},
		{
			name:    "exact match",
			path:    "/v1/chat",
			matches: true,
		},
		{
			name:    "different path",
			path:    "/v1/embeddings",
			matches: false,
		},
		{
			name:    "string prefix match (chatbot starts with chat)",
			path:    "/v1/chatbot",
			matches: true, // strings.HasPrefix behavior
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", tt.path, nil)
			result := router.ruleMatches(rule, req)
			if result != tt.matches {
				t.Errorf("expected %v for path '%s', got %v", tt.matches, tt.path, result)
			}
		})
	}
}

func TestRouter_ruleMatches_ModelPattern(t *testing.T) {
	store := cache.NewStore()
	log := zap.New()
	router := NewRouter(store, nil, log)

	pattern := "gpt-4*"
	rule := &gatewayv1alpha1.RouteRule{
		Match: &gatewayv1alpha1.RouteMatch{
			ModelPattern: &pattern,
		},
	}

	tests := []struct {
		name    string
		model   string
		matches bool
	}{
		{
			name:    "matching pattern",
			model:   "gpt-4",
			matches: true,
		},
		{
			name:    "matching with suffix",
			model:   "gpt-4-turbo",
			matches: true,
		},
		{
			name:    "different model",
			model:   "gpt-3.5-turbo",
			matches: false,
		},
		{
			name:    "no model header",
			model:   "",
			matches: true, // No header means no check (vacuously true for empty header)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/", nil)
			if tt.model != "" {
				req.Header.Set("X-Model", tt.model)
			}

			result := router.ruleMatches(rule, req)
			if result != tt.matches {
				t.Errorf("expected %v for model '%s', got %v", tt.matches, tt.model, result)
			}
		})
	}
}

func TestRouter_ruleMatches_MultipleConditions(t *testing.T) {
	store := cache.NewStore()
	log := zap.New()
	router := NewRouter(store, nil, log)

	pathPrefix := "/v1/chat"
	rule := &gatewayv1alpha1.RouteRule{
		Match: &gatewayv1alpha1.RouteMatch{
			Headers: map[string]string{
				"x-user-tier": "premium",
			},
			PathPrefix: &pathPrefix,
		},
	}

	tests := []struct {
		name    string
		path    string
		headers map[string]string
		matches bool
	}{
		{
			name:    "all conditions match",
			path:    "/v1/chat/completions",
			headers: map[string]string{"x-user-tier": "premium"},
			matches: true,
		},
		{
			name:    "path matches but header doesn't",
			path:    "/v1/chat/completions",
			headers: map[string]string{"x-user-tier": "basic"},
			matches: false,
		},
		{
			name:    "header matches but path doesn't",
			path:    "/v1/embeddings",
			headers: map[string]string{"x-user-tier": "premium"},
			matches: false,
		},
		{
			name:    "neither matches",
			path:    "/v1/embeddings",
			headers: map[string]string{"x-user-tier": "basic"},
			matches: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", tt.path, nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			result := router.ruleMatches(rule, req)
			if result != tt.matches {
				t.Errorf("expected %v, got %v", tt.matches, result)
			}
		})
	}
}

func TestRouter_matchRule_FirstMatch(t *testing.T) {
	store := cache.NewStore()
	log := zap.New()
	router := NewRouter(store, nil, log)

	pathPrefix1 := "/v1/chat"
	pathPrefix2 := "/v1"
	route := &gatewayv1alpha1.InferenceRoute{
		Spec: gatewayv1alpha1.InferenceRouteSpec{
			Rules: []gatewayv1alpha1.RouteRule{
				{
					Match: &gatewayv1alpha1.RouteMatch{
						PathPrefix: &pathPrefix1,
					},
					Backends: []gatewayv1alpha1.BackendRef{{Name: "chat-backend"}},
				},
				{
					Match: &gatewayv1alpha1.RouteMatch{
						PathPrefix: &pathPrefix2,
					},
					Backends: []gatewayv1alpha1.BackendRef{{Name: "general-backend"}},
				},
			},
		},
	}

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	matched := router.matchRule(route, req)

	if matched == nil {
		t.Fatal("expected to match a rule")
	}
	if matched.Backends[0].Name != "chat-backend" {
		t.Errorf("expected 'chat-backend', got '%s'", matched.Backends[0].Name)
	}
}

func TestRouter_matchRule_NoMatch(t *testing.T) {
	store := cache.NewStore()
	log := zap.New()
	router := NewRouter(store, nil, log)

	pathPrefix := "/v1/chat"
	route := &gatewayv1alpha1.InferenceRoute{
		Spec: gatewayv1alpha1.InferenceRouteSpec{
			Rules: []gatewayv1alpha1.RouteRule{
				{
					Match: &gatewayv1alpha1.RouteMatch{
						PathPrefix: &pathPrefix,
					},
					Backends: []gatewayv1alpha1.BackendRef{{Name: "chat-backend"}},
				},
			},
		},
	}

	req := httptest.NewRequest("POST", "/v1/embeddings", nil)
	matched := router.matchRule(route, req)

	if matched != nil {
		t.Error("expected no match")
	}
}

func TestRouterOptions(t *testing.T) {
	store := cache.NewStore()
	log := zap.New()

	metrics := NewMetricsRecorder()
	experiments := NewExperimentManager(nil)
	costTracker := NewCostTracker(nil)

	router := NewRouter(store, nil, log,
		WithRouterMetrics(metrics),
		WithRouterExperiments(experiments),
		WithRouterCostTracker(costTracker),
	)

	if router.metrics != metrics {
		t.Error("expected metrics to be set")
	}
	if router.experiments != experiments {
		t.Error("expected experiments to be set")
	}
	if router.costTracker != costTracker {
		t.Error("expected cost tracker to be set")
	}
}
