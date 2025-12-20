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
	"fmt"
	"net/http"
	"time"

	gatewayv1alpha1 "github.com/judeoyovbaire/inference-gateway/api/v1alpha1"
)

// Result represents the outcome of a health check
type Result struct {
	Healthy   bool
	Latency   time.Duration
	Error     error
	Timestamp time.Time
}

// Checker performs health checks on inference backends
type Checker struct {
	httpClient *http.Client
}

// NewChecker creates a new health checker with default settings
func NewChecker() *Checker {
	return &Checker{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// NewCheckerWithTimeout creates a health checker with a custom timeout
func NewCheckerWithTimeout(timeout time.Duration) *Checker {
	return &Checker{
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Check performs a health check on the given backend
func (c *Checker) Check(ctx context.Context, backend *gatewayv1alpha1.InferenceBackend) Result {
	// Determine timeout from backend config
	timeout := 5 * time.Second
	if backend.Spec.HealthCheck != nil && backend.Spec.HealthCheck.TimeoutSeconds > 0 {
		timeout = time.Duration(backend.Spec.HealthCheck.TimeoutSeconds) * time.Second
	}

	// Create a context with the health check timeout
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	switch backend.Spec.Type {
	case gatewayv1alpha1.BackendTypeExternal:
		return c.checkExternal(checkCtx, backend)
	case gatewayv1alpha1.BackendTypeKubernetes:
		return c.checkKubernetes(checkCtx, backend)
	case gatewayv1alpha1.BackendTypeKServe:
		return c.checkKServe(checkCtx, backend)
	default:
		return Result{
			Healthy:   false,
			Error:     fmt.Errorf("unknown backend type: %s", backend.Spec.Type),
			Timestamp: time.Now(),
		}
	}
}

// checkExternal verifies external API endpoints (OpenAI, Anthropic, etc.)
// External APIs typically don't have traditional health endpoints, so we verify URL reachability
func (c *Checker) checkExternal(ctx context.Context, backend *gatewayv1alpha1.InferenceBackend) Result {
	if backend.Spec.External == nil {
		return Result{
			Healthy:   false,
			Error:     fmt.Errorf("external backend config is nil"),
			Timestamp: time.Now(),
		}
	}

	url := backend.Spec.External.URL
	if url == "" {
		return Result{
			Healthy:   false,
			Error:     fmt.Errorf("external backend URL is empty"),
			Timestamp: time.Now(),
		}
	}

	// For external providers, we do a lightweight HEAD request to verify reachability
	// We don't authenticate here - that's validated during actual requests
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return Result{
			Healthy:   false,
			Error:     fmt.Errorf("failed to create request: %w", err),
			Timestamp: time.Now(),
			Latency:   time.Since(start),
		}
	}

	resp, err := c.httpClient.Do(req)
	latency := time.Since(start)

	if err != nil {
		return Result{
			Healthy:   false,
			Error:     fmt.Errorf("health check failed: %w", err),
			Timestamp: time.Now(),
			Latency:   latency,
		}
	}
	defer resp.Body.Close()

	// For external APIs, we consider the endpoint healthy if it responds
	// Even 401/403 means the service is up (just not authenticated)
	healthy := resp.StatusCode < 500
	var resultErr error
	if !healthy {
		resultErr = fmt.Errorf("external API returned status %d", resp.StatusCode)
	}

	return Result{
		Healthy:   healthy,
		Latency:   latency,
		Error:     resultErr,
		Timestamp: time.Now(),
	}
}

// checkKubernetes performs health check against a Kubernetes service
func (c *Checker) checkKubernetes(ctx context.Context, backend *gatewayv1alpha1.InferenceBackend) Result {
	url, err := c.BuildHealthCheckURL(backend)
	if err != nil {
		return Result{
			Healthy:   false,
			Error:     err,
			Timestamp: time.Now(),
		}
	}

	return c.doHealthCheck(ctx, url)
}

// checkKServe performs health check against a KServe InferenceService
func (c *Checker) checkKServe(ctx context.Context, backend *gatewayv1alpha1.InferenceBackend) Result {
	url, err := c.BuildHealthCheckURL(backend)
	if err != nil {
		return Result{
			Healthy:   false,
			Error:     err,
			Timestamp: time.Now(),
		}
	}

	return c.doHealthCheck(ctx, url)
}

// doHealthCheck performs the actual HTTP health check
func (c *Checker) doHealthCheck(ctx context.Context, url string) Result {
	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Result{
			Healthy:   false,
			Error:     fmt.Errorf("failed to create request: %w", err),
			Timestamp: time.Now(),
			Latency:   time.Since(start),
		}
	}

	resp, err := c.httpClient.Do(req)
	latency := time.Since(start)

	if err != nil {
		return Result{
			Healthy:   false,
			Error:     fmt.Errorf("health check failed: %w", err),
			Timestamp: time.Now(),
			Latency:   latency,
		}
	}
	defer resp.Body.Close()

	// Consider 2xx and 3xx as healthy
	healthy := resp.StatusCode >= 200 && resp.StatusCode < 400
	var resultErr error
	if !healthy {
		resultErr = fmt.Errorf("health check returned status %d", resp.StatusCode)
	}

	return Result{
		Healthy:   healthy,
		Latency:   latency,
		Error:     resultErr,
		Timestamp: time.Now(),
	}
}

// BuildHealthCheckURL constructs the health check URL for a backend
// This is exported for use by other packages that need the URL
func (c *Checker) BuildHealthCheckURL(backend *gatewayv1alpha1.InferenceBackend) (string, error) {
	switch backend.Spec.Type {
	case gatewayv1alpha1.BackendTypeExternal:
		if backend.Spec.External == nil || backend.Spec.External.URL == "" {
			return "", fmt.Errorf("external backend URL is not configured")
		}
		return backend.Spec.External.URL, nil

	case gatewayv1alpha1.BackendTypeKubernetes:
		if backend.Spec.Kubernetes == nil {
			return "", fmt.Errorf("kubernetes backend config is not configured")
		}
		k8s := backend.Spec.Kubernetes
		namespace := k8s.Namespace
		if namespace == "" {
			namespace = backend.Namespace
		}
		port := k8s.Port
		if port == 0 {
			port = 8080
		}
		healthPath := "/health"
		if backend.Spec.HealthCheck != nil && backend.Spec.HealthCheck.Path != "" {
			healthPath = backend.Spec.HealthCheck.Path
		}
		return fmt.Sprintf("http://%s.%s.svc.cluster.local:%d%s",
			k8s.ServiceName, namespace, port, healthPath), nil

	case gatewayv1alpha1.BackendTypeKServe:
		if backend.Spec.KServe == nil {
			return "", fmt.Errorf("kserve backend config is not configured")
		}
		kserve := backend.Spec.KServe
		namespace := kserve.Namespace
		if namespace == "" {
			namespace = backend.Namespace
		}
		healthPath := "/v1/models"
		if backend.Spec.HealthCheck != nil && backend.Spec.HealthCheck.Path != "" {
			healthPath = backend.Spec.HealthCheck.Path
		}
		return fmt.Sprintf("http://%s-predictor.%s.svc.cluster.local%s",
			kserve.ServiceName, namespace, healthPath), nil

	default:
		return "", fmt.Errorf("unknown backend type: %s", backend.Spec.Type)
	}
}
