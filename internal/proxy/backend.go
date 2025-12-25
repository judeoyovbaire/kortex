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
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/otel/trace"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gatewayv1alpha1 "github.com/judeoyovbaire/kortex/api/v1alpha1"
	"github.com/judeoyovbaire/kortex/internal/cache"
	"github.com/judeoyovbaire/kortex/internal/tracing"
)

// BackendHandler executes requests against backends with fallback support
type BackendHandler struct {
	cache       *cache.Store
	client      client.Client
	log         logr.Logger
	metrics     *MetricsRecorder
	costTracker *CostTracker
	tracer      *tracing.Tracer
}

// NewBackendHandler creates a new backend handler
func NewBackendHandler(store *cache.Store, k8sClient client.Client, log logr.Logger, metrics *MetricsRecorder, costTracker *CostTracker, tracer *tracing.Tracer) *BackendHandler {
	return &BackendHandler{
		cache:       store,
		client:      k8sClient,
		log:         log.WithName("backend-handler"),
		metrics:     metrics,
		costTracker: costTracker,
		tracer:      tracer,
	}
}

// ExecuteWithFallback attempts to execute the request against the primary backend,
// falling back to other backends in the chain if the primary fails
func (h *BackendHandler) ExecuteWithFallback(
	ctx context.Context,
	w http.ResponseWriter,
	req *http.Request,
	route *gatewayv1alpha1.InferenceRoute,
	primaryBackend gatewayv1alpha1.BackendRef,
) {
	// Build fallback chain: primary backend first, then fallback backends
	chain := h.buildFallbackChain(route, primaryBackend)

	// Determine timeout per backend attempt
	timeout := 30 * time.Second
	if route.Spec.Fallback != nil && route.Spec.Fallback.TimeoutSeconds > 0 {
		timeout = time.Duration(route.Spec.Fallback.TimeoutSeconds) * time.Second
	}

	var lastErr error
	var previousBackend string
	for i, backendName := range chain {
		// Fetch backend from cache
		backend, ok := h.cache.GetBackendByName(route.Namespace, backendName)
		if !ok {
			h.log.V(1).Info("Backend not found in cache", "backend", backendName)
			lastErr = fmt.Errorf("backend %s not found", backendName)
			continue
		}

		// Skip unhealthy backends unless it's the last resort
		if backend.Status.Health != "Healthy" && i < len(chain)-1 {
			h.log.V(1).Info("Skipping unhealthy backend", "backend", backendName, "health", backend.Status.Health)
			continue
		}

		// Record fallback if we're not on the first attempt
		if previousBackend != "" && h.metrics != nil {
			h.metrics.RecordFallback(route.Name, previousBackend, backendName)
		}

		// Increment active requests
		if h.metrics != nil {
			h.metrics.IncActiveRequests(backendName)
		}

		// Create timeout context for this attempt
		attemptCtx, cancel := context.WithTimeout(ctx, timeout)
		start := time.Now()

		// Execute the request
		statusCode, err := h.executeRequest(attemptCtx, w, req, route, backend)
		duration := time.Since(start)
		cancel()

		// Decrement active requests
		if h.metrics != nil {
			h.metrics.DecActiveRequests(backendName)
		}

		if err == nil {
			// Success - record metrics
			if h.metrics != nil {
				h.metrics.RecordRequest(route.Name, backendName, statusCode, duration)
			}
			return
		}

		// Record error
		if h.metrics != nil {
			h.metrics.RecordError(route.Name, backendName, "request_failed")
			h.metrics.RecordRequest(route.Name, backendName, statusCode, duration)
		}

		h.log.Info("Backend request failed, trying next",
			"backend", backendName,
			"error", err.Error(),
			"attempt", i+1,
			"total", len(chain),
		)
		lastErr = err
		previousBackend = backendName

		// Apply exponential backoff before retrying (100ms * 2^attempt, max 2s)
		if i < len(chain)-1 {
			backoff := time.Duration(100<<uint(i)) * time.Millisecond
			if backoff > 2*time.Second {
				backoff = 2 * time.Second
			}
			select {
			case <-ctx.Done():
				// Context cancelled, don't continue retrying
				http.Error(w, "Request cancelled", http.StatusServiceUnavailable)
				return
			case <-time.After(backoff):
				// Continue to next backend
			}
		}
	}

	// All backends failed
	h.log.Error(lastErr, "All backends in fallback chain failed")
	http.Error(w, "All backends failed: "+lastErr.Error(), http.StatusServiceUnavailable)
}

// buildFallbackChain constructs the ordered list of backends to try
func (h *BackendHandler) buildFallbackChain(route *gatewayv1alpha1.InferenceRoute, primary gatewayv1alpha1.BackendRef) []string {
	chain := []string{primary.Name}

	// Add fallback backends if configured
	if route.Spec.Fallback != nil {
		for _, name := range route.Spec.Fallback.Backends {
			// Avoid duplicates
			if name != primary.Name {
				chain = append(chain, name)
			}
		}
	}

	return chain
}

// executeRequest performs the actual request to a backend
func (h *BackendHandler) executeRequest(
	ctx context.Context,
	w http.ResponseWriter,
	req *http.Request,
	route *gatewayv1alpha1.InferenceRoute,
	backend *gatewayv1alpha1.InferenceBackend,
) (int, error) {
	// Build target URL
	targetURL, err := h.buildTargetURL(backend)
	if err != nil {
		return 0, fmt.Errorf("failed to build target URL: %w", err)
	}

	// Get provider for cost tracking
	provider := ""
	if backend.Spec.External != nil {
		provider = backend.Spec.External.Provider
	}

	// Start backend span if tracing is enabled
	var span trace.Span
	if h.tracer != nil {
		ctx, span = h.tracer.StartBackendSpan(ctx, backend.Name, string(backend.Spec.Type), targetURL.String())
		defer func() {
			span.End()
		}()
	}

	// Track status code
	statusCode := http.StatusOK

	// Create reverse proxy with response modification for cost tracking
	proxy := &httputil.ReverseProxy{
		Director: func(r *http.Request) {
			r.URL.Scheme = targetURL.Scheme
			r.URL.Host = targetURL.Host
			r.Host = targetURL.Host

			// Preserve the original path
			if targetURL.Path != "" && targetURL.Path != "/" {
				r.URL.Path = targetURL.Path + r.URL.Path
			}

			// Inject API key for external backends
			if backend.Spec.Type == gatewayv1alpha1.BackendTypeExternal {
				h.injectAPIKey(ctx, r, backend)
			}

			h.log.V(2).Info("Proxying request",
				"target", r.URL.String(),
				"backend", backend.Name,
			)
		},
		ModifyResponse: func(resp *http.Response) error {
			statusCode = resp.StatusCode

			// Add headers to indicate which backend served the request
			resp.Header.Set("X-Served-By", backend.Name)
			resp.Header.Set("X-Backend-Type", string(backend.Spec.Type))

			// Track costs if enabled
			if route.Spec.CostTracking && backend.Spec.Cost != nil && h.costTracker != nil {
				h.trackCosts(resp, route.Name, backend, provider)
			}

			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			h.log.Error(err, "Proxy error",
				"backend", backend.Name,
				"target", targetURL.String(),
			)
			statusCode = http.StatusBadGateway
		},
	}

	// Create a response recorder to check if the proxy succeeded
	recorder := &responseRecorder{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}

	// Execute proxy request
	proxy.ServeHTTP(recorder, req.WithContext(ctx))

	// Update status code from recorder
	if recorder.statusCode != http.StatusOK {
		statusCode = recorder.statusCode
	}

	// Add status code to span if tracing enabled
	if span != nil {
		tracing.AddBackendAttributes(span, statusCode, 0, provider)
		if statusCode >= 500 {
			tracing.SetSpanError(span, fmt.Errorf("backend returned status %d", statusCode))
		} else {
			tracing.SetSpanOK(span)
		}
	}

	// Check if the request failed with a server error
	if statusCode >= 500 {
		return statusCode, fmt.Errorf("backend returned status %d", statusCode)
	}

	return statusCode, nil
}

// trackCosts extracts token usage and tracks costs
func (h *BackendHandler) trackCosts(
	resp *http.Response,
	routeName string,
	backend *gatewayv1alpha1.InferenceBackend,
	provider string,
) {
	// We need to read the body to parse token usage, but we also need to forward it
	// For streaming responses, this won't work well - we'd need a different approach
	if resp.Body == nil {
		return
	}

	// Read the body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		h.log.V(1).Info("Failed to read response body for cost tracking", "error", err)
		return
	}

	// Replace the body so it can still be read by the client
	resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// Parse token usage
	usage := ParseTokenUsage(provider, resp, bodyBytes)

	// Track costs
	if usage.InputTokens > 0 || usage.OutputTokens > 0 {
		h.costTracker.TrackRequest(routeName, backend.Name, usage, backend.Spec.Cost)
	}
}

// buildTargetURL constructs the backend URL based on its type
func (h *BackendHandler) buildTargetURL(backend *gatewayv1alpha1.InferenceBackend) (*url.URL, error) {
	switch backend.Spec.Type {
	case gatewayv1alpha1.BackendTypeExternal:
		if backend.Spec.External == nil || backend.Spec.External.URL == "" {
			return nil, fmt.Errorf("external backend URL is not configured")
		}
		return url.Parse(backend.Spec.External.URL)

	case gatewayv1alpha1.BackendTypeKubernetes:
		if backend.Spec.Kubernetes == nil {
			return nil, fmt.Errorf("kubernetes backend config is not configured")
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
		return url.Parse(fmt.Sprintf("http://%s.%s.svc.cluster.local:%d",
			k8s.ServiceName, namespace, port))

	case gatewayv1alpha1.BackendTypeKServe:
		if backend.Spec.KServe == nil {
			return nil, fmt.Errorf("kserve backend config is not configured")
		}
		kserve := backend.Spec.KServe
		namespace := kserve.Namespace
		if namespace == "" {
			namespace = backend.Namespace
		}
		// KServe URL pattern
		return url.Parse(fmt.Sprintf("http://%s-predictor.%s.svc.cluster.local",
			kserve.ServiceName, namespace))

	default:
		return nil, fmt.Errorf("unsupported backend type: %s", backend.Spec.Type)
	}
}

// injectAPIKey adds the API key header for external backends
func (h *BackendHandler) injectAPIKey(ctx context.Context, req *http.Request, backend *gatewayv1alpha1.InferenceBackend) {
	if backend.Spec.External == nil || backend.Spec.External.APIKeySecret == nil {
		return
	}

	secretRef := backend.Spec.External.APIKeySecret

	// Fetch the secret
	secret := &corev1.Secret{}
	err := h.client.Get(ctx, types.NamespacedName{
		Namespace: backend.Namespace,
		Name:      secretRef.Name,
	}, secret)

	if err != nil {
		h.log.Error(err, "Failed to fetch API key secret",
			"secret", secretRef.Name,
			"backend", backend.Name,
		)
		return
	}

	// Get the API key from the secret
	apiKey := string(secret.Data[secretRef.Key])
	if apiKey == "" {
		h.log.Info("API key not found in secret",
			"secret", secretRef.Name,
			"key", secretRef.Key,
			"backend", backend.Name,
		)
		return
	}

	// Set the appropriate header based on provider
	provider := backend.Spec.External.Provider
	if provider == "" {
		provider = "openai" // default
	}

	switch provider {
	case "openai":
		req.Header.Set("Authorization", "Bearer "+apiKey)
	case "anthropic":
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	case "cohere":
		req.Header.Set("Authorization", "Bearer "+apiKey)
	default:
		// Default to Bearer token
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	h.log.V(2).Info("Injected API key", "backend", backend.Name, "provider", provider)
}

// responseRecorder wraps http.ResponseWriter to capture the status code
type responseRecorder struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (r *responseRecorder) WriteHeader(code int) {
	if !r.written {
		r.statusCode = code
		r.written = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if !r.written {
		r.written = true
	}
	return r.ResponseWriter.Write(b)
}
