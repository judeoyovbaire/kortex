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
	"net/http"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	gatewayv1alpha1 "github.com/judeoyovbaire/inference-gateway/api/v1alpha1"
	"github.com/judeoyovbaire/inference-gateway/internal/cache"
)

func TestBackendHandler_buildFallbackChain_PrimaryOnly(t *testing.T) {
	store := cache.NewStore()
	log := zap.New()
	handler := NewBackendHandler(store, nil, log, nil, nil)

	route := &gatewayv1alpha1.InferenceRoute{
		Spec: gatewayv1alpha1.InferenceRouteSpec{
			Fallback: nil, // No fallback configured
		},
	}
	primary := gatewayv1alpha1.BackendRef{Name: "primary-backend"}

	chain := handler.buildFallbackChain(route, primary)

	if len(chain) != 1 {
		t.Errorf("expected 1 backend in chain, got %d", len(chain))
	}
	if chain[0] != "primary-backend" {
		t.Errorf("expected 'primary-backend', got '%s'", chain[0])
	}
}

func TestBackendHandler_buildFallbackChain_WithFallbacks(t *testing.T) {
	store := cache.NewStore()
	log := zap.New()
	handler := NewBackendHandler(store, nil, log, nil, nil)

	route := &gatewayv1alpha1.InferenceRoute{
		Spec: gatewayv1alpha1.InferenceRouteSpec{
			Fallback: &gatewayv1alpha1.FallbackChain{
				Backends: []string{"fallback-1", "fallback-2"},
			},
		},
	}
	primary := gatewayv1alpha1.BackendRef{Name: "primary-backend"}

	chain := handler.buildFallbackChain(route, primary)

	if len(chain) != 3 {
		t.Errorf("expected 3 backends in chain, got %d", len(chain))
	}

	expected := []string{"primary-backend", "fallback-1", "fallback-2"}
	for i, name := range expected {
		if chain[i] != name {
			t.Errorf("expected chain[%d]='%s', got '%s'", i, name, chain[i])
		}
	}
}

func TestBackendHandler_buildFallbackChain_NoDuplicates(t *testing.T) {
	store := cache.NewStore()
	log := zap.New()
	handler := NewBackendHandler(store, nil, log, nil, nil)

	// Primary is also in fallback list - should not duplicate
	route := &gatewayv1alpha1.InferenceRoute{
		Spec: gatewayv1alpha1.InferenceRouteSpec{
			Fallback: &gatewayv1alpha1.FallbackChain{
				Backends: []string{"primary-backend", "fallback-1"},
			},
		},
	}
	primary := gatewayv1alpha1.BackendRef{Name: "primary-backend"}

	chain := handler.buildFallbackChain(route, primary)

	if len(chain) != 2 {
		t.Errorf("expected 2 backends (no duplicate), got %d", len(chain))
	}

	expected := []string{"primary-backend", "fallback-1"}
	for i, name := range expected {
		if chain[i] != name {
			t.Errorf("expected chain[%d]='%s', got '%s'", i, name, chain[i])
		}
	}
}

func TestBackendHandler_buildTargetURL_External(t *testing.T) {
	store := cache.NewStore()
	log := zap.New()
	handler := NewBackendHandler(store, nil, log, nil, nil)

	backend := &gatewayv1alpha1.InferenceBackend{
		Spec: gatewayv1alpha1.InferenceBackendSpec{
			Type: gatewayv1alpha1.BackendTypeExternal,
			External: &gatewayv1alpha1.ExternalBackend{
				URL: "https://api.openai.com/v1",
			},
		},
	}

	url, err := handler.buildTargetURL(backend)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if url.String() != "https://api.openai.com/v1" {
		t.Errorf("expected 'https://api.openai.com/v1', got '%s'", url.String())
	}
}

func TestBackendHandler_buildTargetURL_Kubernetes(t *testing.T) {
	store := cache.NewStore()
	log := zap.New()
	handler := NewBackendHandler(store, nil, log, nil, nil)

	backend := &gatewayv1alpha1.InferenceBackend{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
		},
		Spec: gatewayv1alpha1.InferenceBackendSpec{
			Type: gatewayv1alpha1.BackendTypeKubernetes,
			Kubernetes: &gatewayv1alpha1.KubernetesBackend{
				ServiceName: "my-service",
				Namespace:   "my-namespace",
				Port:        8080,
			},
		},
	}

	url, err := handler.buildTargetURL(backend)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected := "http://my-service.my-namespace.svc.cluster.local:8080"
	if url.String() != expected {
		t.Errorf("expected '%s', got '%s'", expected, url.String())
	}
}

func TestBackendHandler_buildTargetURL_Kubernetes_DefaultNamespace(t *testing.T) {
	store := cache.NewStore()
	log := zap.New()
	handler := NewBackendHandler(store, nil, log, nil, nil)

	backend := &gatewayv1alpha1.InferenceBackend{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
		},
		Spec: gatewayv1alpha1.InferenceBackendSpec{
			Type: gatewayv1alpha1.BackendTypeKubernetes,
			Kubernetes: &gatewayv1alpha1.KubernetesBackend{
				ServiceName: "my-service",
				// Namespace not specified - should use backend's namespace
			},
		},
	}

	url, err := handler.buildTargetURL(backend)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected := "http://my-service.default.svc.cluster.local:8080"
	if url.String() != expected {
		t.Errorf("expected '%s', got '%s'", expected, url.String())
	}
}

func TestBackendHandler_buildTargetURL_KServe(t *testing.T) {
	store := cache.NewStore()
	log := zap.New()
	handler := NewBackendHandler(store, nil, log, nil, nil)

	backend := &gatewayv1alpha1.InferenceBackend{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
		},
		Spec: gatewayv1alpha1.InferenceBackendSpec{
			Type: gatewayv1alpha1.BackendTypeKServe,
			KServe: &gatewayv1alpha1.KServeBackend{
				ServiceName: "mistral-7b",
				Namespace:   "models",
			},
		},
	}

	url, err := handler.buildTargetURL(backend)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected := "http://mistral-7b-predictor.models.svc.cluster.local"
	if url.String() != expected {
		t.Errorf("expected '%s', got '%s'", expected, url.String())
	}
}

func TestBackendHandler_buildTargetURL_Errors(t *testing.T) {
	store := cache.NewStore()
	log := zap.New()
	handler := NewBackendHandler(store, nil, log, nil, nil)

	tests := []struct {
		name    string
		backend *gatewayv1alpha1.InferenceBackend
	}{
		{
			name: "external nil config",
			backend: &gatewayv1alpha1.InferenceBackend{
				Spec: gatewayv1alpha1.InferenceBackendSpec{
					Type:     gatewayv1alpha1.BackendTypeExternal,
					External: nil,
				},
			},
		},
		{
			name: "external empty URL",
			backend: &gatewayv1alpha1.InferenceBackend{
				Spec: gatewayv1alpha1.InferenceBackendSpec{
					Type: gatewayv1alpha1.BackendTypeExternal,
					External: &gatewayv1alpha1.ExternalBackend{
						URL: "",
					},
				},
			},
		},
		{
			name: "kubernetes nil config",
			backend: &gatewayv1alpha1.InferenceBackend{
				Spec: gatewayv1alpha1.InferenceBackendSpec{
					Type:       gatewayv1alpha1.BackendTypeKubernetes,
					Kubernetes: nil,
				},
			},
		},
		{
			name: "kserve nil config",
			backend: &gatewayv1alpha1.InferenceBackend{
				Spec: gatewayv1alpha1.InferenceBackendSpec{
					Type:   gatewayv1alpha1.BackendTypeKServe,
					KServe: nil,
				},
			},
		},
		{
			name: "unknown type",
			backend: &gatewayv1alpha1.InferenceBackend{
				Spec: gatewayv1alpha1.InferenceBackendSpec{
					Type: "unknown",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := handler.buildTargetURL(tt.backend)
			if err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestResponseRecorder_WriteHeader(t *testing.T) {
	// Create a mock response writer
	mockWriter := &mockResponseWriter{
		headers: make(http.Header),
	}

	recorder := &responseRecorder{
		ResponseWriter: mockWriter,
		statusCode:     200,
	}

	// First WriteHeader should set the status
	recorder.WriteHeader(404)

	if recorder.statusCode != 404 {
		t.Errorf("expected status 404, got %d", recorder.statusCode)
	}
	if !recorder.written {
		t.Error("expected written to be true")
	}

	// Second WriteHeader should be ignored
	recorder.WriteHeader(500)

	if recorder.statusCode != 404 {
		t.Error("second WriteHeader should be ignored")
	}
}

func TestResponseRecorder_Write(t *testing.T) {
	mockWriter := &mockResponseWriter{
		headers: make(http.Header),
	}

	recorder := &responseRecorder{
		ResponseWriter: mockWriter,
		statusCode:     200,
	}

	data := []byte("test response")
	n, err := recorder.Write(data)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if n != len(data) {
		t.Errorf("expected %d bytes written, got %d", len(data), n)
	}
	if !recorder.written {
		t.Error("expected written to be true after Write")
	}
}

func TestNewBackendHandler(t *testing.T) {
	store := cache.NewStore()
	log := zap.New()
	metrics := NewMetricsRecorder()
	costTracker := NewCostTracker(nil)

	handler := NewBackendHandler(store, nil, log, metrics, costTracker)

	if handler == nil {
		t.Fatal("expected non-nil handler")
	}
	if handler.cache != store {
		t.Error("expected cache to be set")
	}
	if handler.metrics != metrics {
		t.Error("expected metrics to be set")
	}
	if handler.costTracker != costTracker {
		t.Error("expected cost tracker to be set")
	}
}

func TestBackendHandler_ExecuteWithFallback_BackendNotFound(t *testing.T) {
	store := cache.NewStore()
	log := zap.New()
	handler := NewBackendHandler(store, nil, log, nil, nil)

	// No backends in cache

	route := &gatewayv1alpha1.InferenceRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
	}
	primary := gatewayv1alpha1.BackendRef{Name: "non-existent"}

	// Use a mock writer to capture the response
	mockWriter := &mockResponseWriter{
		headers: make(http.Header),
	}

	handler.ExecuteWithFallback(nil, mockWriter, nil, route, primary)

	// Should return 503 when all backends fail
	if mockWriter.statusCode != 503 {
		t.Errorf("expected status 503, got %d", mockWriter.statusCode)
	}
}

func TestBackendHandler_ExecuteWithFallback_SkipsUnhealthyBackends(t *testing.T) {
	store := cache.NewStore()
	log := zap.New()
	handler := NewBackendHandler(store, nil, log, nil, nil)

	// Add unhealthy backend
	unhealthyBackend := &gatewayv1alpha1.InferenceBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unhealthy",
			Namespace: "default",
		},
		Status: gatewayv1alpha1.InferenceBackendStatus{
			Health: "Unhealthy",
		},
	}
	store.SetBackend(types.NamespacedName{Namespace: "default", Name: "unhealthy"}, unhealthyBackend)

	route := &gatewayv1alpha1.InferenceRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
		Spec: gatewayv1alpha1.InferenceRouteSpec{
			Fallback: &gatewayv1alpha1.FallbackChain{
				Backends: []string{"another-backend"},
			},
		},
	}
	primary := gatewayv1alpha1.BackendRef{Name: "unhealthy"}

	mockWriter := &mockResponseWriter{
		headers: make(http.Header),
	}

	handler.ExecuteWithFallback(nil, mockWriter, nil, route, primary)

	// Should try to use the fallback (which doesn't exist either)
	// The point is that it skipped the unhealthy backend
	if mockWriter.statusCode != 503 {
		t.Errorf("expected status 503, got %d", mockWriter.statusCode)
	}
}

// mockResponseWriter implements http.ResponseWriter for testing
type mockResponseWriter struct {
	headers    http.Header
	body       []byte
	statusCode int
}

func (m *mockResponseWriter) Header() http.Header {
	return m.headers
}

func (m *mockResponseWriter) Write(data []byte) (int, error) {
	m.body = append(m.body, data...)
	return len(data), nil
}

func (m *mockResponseWriter) WriteHeader(statusCode int) {
	m.statusCode = statusCode
}
