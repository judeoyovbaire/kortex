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

package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gatewayv1alpha1 "github.com/judeoyovbaire/inference-gateway/api/v1alpha1"
)

func TestNewChecker(t *testing.T) {
	checker := NewChecker()
	if checker == nil {
		t.Fatal("expected non-nil checker")
	}
	if checker.httpClient == nil {
		t.Error("expected non-nil HTTP client")
	}
}

func TestNewCheckerWithTimeout(t *testing.T) {
	checker := NewCheckerWithTimeout(5 * time.Second)
	if checker == nil {
		t.Fatal("expected non-nil checker")
	}
	if checker.httpClient.Timeout != 5*time.Second {
		t.Errorf("expected 5s timeout, got %v", checker.httpClient.Timeout)
	}
}

func TestChecker_Check_ExternalBackend(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("expected HEAD request, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := NewChecker()
	backend := &gatewayv1alpha1.InferenceBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backend",
			Namespace: "default",
		},
		Spec: gatewayv1alpha1.InferenceBackendSpec{
			Type: gatewayv1alpha1.BackendTypeExternal,
			External: &gatewayv1alpha1.ExternalBackend{
				URL: server.URL,
			},
		},
	}

	result := checker.Check(context.Background(), backend)

	if !result.Healthy {
		t.Errorf("expected healthy, got unhealthy: %v", result.Error)
	}
	if result.Latency <= 0 {
		t.Error("expected positive latency")
	}
}

func TestChecker_Check_ExternalBackend_Unhealthy(t *testing.T) {
	// Create a test server that returns 500
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	checker := NewChecker()
	backend := &gatewayv1alpha1.InferenceBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backend",
			Namespace: "default",
		},
		Spec: gatewayv1alpha1.InferenceBackendSpec{
			Type: gatewayv1alpha1.BackendTypeExternal,
			External: &gatewayv1alpha1.ExternalBackend{
				URL: server.URL,
			},
		},
	}

	result := checker.Check(context.Background(), backend)

	if result.Healthy {
		t.Error("expected unhealthy for 500 response")
	}
	if result.Error == nil {
		t.Error("expected error for unhealthy check")
	}
}

func TestChecker_Check_ExternalBackend_AuthError(t *testing.T) {
	// Create a test server that returns 401 (should still be considered healthy)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	checker := NewChecker()
	backend := &gatewayv1alpha1.InferenceBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backend",
			Namespace: "default",
		},
		Spec: gatewayv1alpha1.InferenceBackendSpec{
			Type: gatewayv1alpha1.BackendTypeExternal,
			External: &gatewayv1alpha1.ExternalBackend{
				URL: server.URL,
			},
		},
	}

	result := checker.Check(context.Background(), backend)

	// 401 means the service is up, just not authenticated
	if !result.Healthy {
		t.Error("expected healthy for 401 response (service is up)")
	}
}

func TestChecker_Check_ExternalBackend_NilConfig(t *testing.T) {
	checker := NewChecker()
	backend := &gatewayv1alpha1.InferenceBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backend",
			Namespace: "default",
		},
		Spec: gatewayv1alpha1.InferenceBackendSpec{
			Type:     gatewayv1alpha1.BackendTypeExternal,
			External: nil,
		},
	}

	result := checker.Check(context.Background(), backend)

	if result.Healthy {
		t.Error("expected unhealthy for nil external config")
	}
	if result.Error == nil {
		t.Error("expected error for nil config")
	}
}

func TestChecker_Check_ExternalBackend_EmptyURL(t *testing.T) {
	checker := NewChecker()
	backend := &gatewayv1alpha1.InferenceBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backend",
			Namespace: "default",
		},
		Spec: gatewayv1alpha1.InferenceBackendSpec{
			Type: gatewayv1alpha1.BackendTypeExternal,
			External: &gatewayv1alpha1.ExternalBackend{
				URL: "",
			},
		},
	}

	result := checker.Check(context.Background(), backend)

	if result.Healthy {
		t.Error("expected unhealthy for empty URL")
	}
}

func TestChecker_Check_UnknownBackendType(t *testing.T) {
	checker := NewChecker()
	backend := &gatewayv1alpha1.InferenceBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backend",
			Namespace: "default",
		},
		Spec: gatewayv1alpha1.InferenceBackendSpec{
			Type: "unknown",
		},
	}

	result := checker.Check(context.Background(), backend)

	if result.Healthy {
		t.Error("expected unhealthy for unknown backend type")
	}
	if result.Error == nil {
		t.Error("expected error for unknown backend type")
	}
}

func TestChecker_Check_WithTimeout(t *testing.T) {
	checker := NewChecker()
	backend := &gatewayv1alpha1.InferenceBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backend",
			Namespace: "default",
		},
		Spec: gatewayv1alpha1.InferenceBackendSpec{
			Type: gatewayv1alpha1.BackendTypeExternal,
			External: &gatewayv1alpha1.ExternalBackend{
				URL: "http://localhost:99999", // Non-existent endpoint
			},
			HealthCheck: &gatewayv1alpha1.HealthCheck{
				TimeoutSeconds: 1,
			},
		},
	}

	start := time.Now()
	result := checker.Check(context.Background(), backend)
	elapsed := time.Since(start)

	if result.Healthy {
		t.Error("expected unhealthy for non-existent endpoint")
	}

	// Should timeout around 1 second (allow some margin)
	if elapsed > 5*time.Second {
		t.Errorf("expected check to timeout within ~1s, took %v", elapsed)
	}
}

func TestChecker_BuildHealthCheckURL_External(t *testing.T) {
	checker := NewChecker()
	backend := &gatewayv1alpha1.InferenceBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backend",
			Namespace: "default",
		},
		Spec: gatewayv1alpha1.InferenceBackendSpec{
			Type: gatewayv1alpha1.BackendTypeExternal,
			External: &gatewayv1alpha1.ExternalBackend{
				URL: "https://api.openai.com/v1",
			},
		},
	}

	url, err := checker.BuildHealthCheckURL(backend)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if url != "https://api.openai.com/v1" {
		t.Errorf("expected 'https://api.openai.com/v1', got '%s'", url)
	}
}

func TestChecker_BuildHealthCheckURL_Kubernetes(t *testing.T) {
	checker := NewChecker()
	backend := &gatewayv1alpha1.InferenceBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backend",
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

	url, err := checker.BuildHealthCheckURL(backend)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected := "http://my-service.my-namespace.svc.cluster.local:8080/health"
	if url != expected {
		t.Errorf("expected '%s', got '%s'", expected, url)
	}
}

func TestChecker_BuildHealthCheckURL_Kubernetes_DefaultNamespace(t *testing.T) {
	checker := NewChecker()
	backend := &gatewayv1alpha1.InferenceBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backend",
			Namespace: "default",
		},
		Spec: gatewayv1alpha1.InferenceBackendSpec{
			Type: gatewayv1alpha1.BackendTypeKubernetes,
			Kubernetes: &gatewayv1alpha1.KubernetesBackend{
				ServiceName: "my-service",
				// Namespace not set - should use backend's namespace
			},
		},
	}

	url, err := checker.BuildHealthCheckURL(backend)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected := "http://my-service.default.svc.cluster.local:8080/health"
	if url != expected {
		t.Errorf("expected '%s', got '%s'", expected, url)
	}
}

func TestChecker_BuildHealthCheckURL_Kubernetes_CustomPath(t *testing.T) {
	checker := NewChecker()
	backend := &gatewayv1alpha1.InferenceBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backend",
			Namespace: "default",
		},
		Spec: gatewayv1alpha1.InferenceBackendSpec{
			Type: gatewayv1alpha1.BackendTypeKubernetes,
			Kubernetes: &gatewayv1alpha1.KubernetesBackend{
				ServiceName: "my-service",
				Namespace:   "my-namespace",
				Port:        9090,
			},
			HealthCheck: &gatewayv1alpha1.HealthCheck{
				Path: "/healthz",
			},
		},
	}

	url, err := checker.BuildHealthCheckURL(backend)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected := "http://my-service.my-namespace.svc.cluster.local:9090/healthz"
	if url != expected {
		t.Errorf("expected '%s', got '%s'", expected, url)
	}
}

func TestChecker_BuildHealthCheckURL_KServe(t *testing.T) {
	checker := NewChecker()
	backend := &gatewayv1alpha1.InferenceBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backend",
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

	url, err := checker.BuildHealthCheckURL(backend)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected := "http://mistral-7b-predictor.models.svc.cluster.local/v1/models"
	if url != expected {
		t.Errorf("expected '%s', got '%s'", expected, url)
	}
}

func TestChecker_BuildHealthCheckURL_KServe_CustomPath(t *testing.T) {
	checker := NewChecker()
	backend := &gatewayv1alpha1.InferenceBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backend",
			Namespace: "default",
		},
		Spec: gatewayv1alpha1.InferenceBackendSpec{
			Type: gatewayv1alpha1.BackendTypeKServe,
			KServe: &gatewayv1alpha1.KServeBackend{
				ServiceName: "mistral-7b",
				Namespace:   "models",
			},
			HealthCheck: &gatewayv1alpha1.HealthCheck{
				Path: "/v2/health/ready",
			},
		},
	}

	url, err := checker.BuildHealthCheckURL(backend)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected := "http://mistral-7b-predictor.models.svc.cluster.local/v2/health/ready"
	if url != expected {
		t.Errorf("expected '%s', got '%s'", expected, url)
	}
}

func TestChecker_BuildHealthCheckURL_Errors(t *testing.T) {
	checker := NewChecker()

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
			_, err := checker.BuildHealthCheckURL(tt.backend)
			if err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestChecker_doHealthCheck_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET request, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := NewChecker()
	result := checker.doHealthCheck(context.Background(), server.URL)

	if !result.Healthy {
		t.Errorf("expected healthy, got unhealthy: %v", result.Error)
	}
}

func TestChecker_doHealthCheck_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	checker := NewChecker()
	result := checker.doHealthCheck(context.Background(), server.URL)

	if result.Healthy {
		t.Error("expected unhealthy for 500 response")
	}
}

func TestChecker_doHealthCheck_RedirectOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMovedPermanently)
	}))
	defer server.Close()

	checker := NewChecker()
	result := checker.doHealthCheck(context.Background(), server.URL)

	// 3xx responses should be considered healthy
	if !result.Healthy {
		t.Error("expected healthy for 301 response")
	}
}
