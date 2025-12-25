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

package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	gatewayv1alpha1 "github.com/judeoyovbaire/kortex/api/v1alpha1"
	"github.com/judeoyovbaire/kortex/internal/cache"
	"github.com/judeoyovbaire/kortex/internal/health"
)

// Health status constants
const (
	HealthStatusHealthy   = "Healthy"
	HealthStatusUnhealthy = "Unhealthy"
	HealthStatusUnknown   = "Unknown"
)

// Condition types for InferenceBackend
const (
	ConditionTypeBackendHealthy = "Healthy"
	ConditionTypeBackendReady   = "Ready"
)

// InferenceBackendReconciler reconciles an InferenceBackend object
type InferenceBackendReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	HealthChecker *health.Checker
	Cache         *cache.Store

	// Track consecutive failures per backend for threshold logic
	failureCounts map[string]int32
	failureMu     sync.RWMutex
}

// +kubebuilder:rbac:groups=gateway.inference-gateway.io,resources=inferencebackends,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gateway.inference-gateway.io,resources=inferencebackends/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gateway.inference-gateway.io,resources=inferencebackends/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch

// Reconcile performs the reconciliation loop for InferenceBackend resources
func (r *InferenceBackendReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Initialize failure counts map if needed
	if r.failureCounts == nil {
		r.failureCounts = make(map[string]int32)
	}

	// Fetch the InferenceBackend resource
	backend := &gatewayv1alpha1.InferenceBackend{}
	if err := r.Get(ctx, req.NamespacedName, backend); err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Resource was deleted, clean up failure tracking and cache
			r.cleanupBackend(req.String())
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to fetch InferenceBackend")
		return ctrl.Result{}, err
	}

	// Validate backend configuration
	if err := r.validateBackendConfig(backend); err != nil {
		log.Error(err, "Invalid backend configuration")
		return r.updateStatusWithError(ctx, backend, err)
	}

	// Perform health check
	result := r.HealthChecker.Check(ctx, backend)

	// Update failure count tracking
	key := req.String()
	r.failureMu.Lock()
	if !result.Healthy {
		r.failureCounts[key]++
	} else {
		r.failureCounts[key] = 0
	}
	currentFailures := r.failureCounts[key]
	r.failureMu.Unlock()

	// Determine health status based on failure threshold
	threshold := int32(3) // default
	if backend.Spec.HealthCheck != nil && backend.Spec.HealthCheck.FailureThreshold > 0 {
		threshold = backend.Spec.HealthCheck.FailureThreshold
	}

	var healthStatus string
	if result.Healthy {
		healthStatus = HealthStatusHealthy
	} else if currentFailures >= threshold {
		healthStatus = HealthStatusUnhealthy
	} else {
		healthStatus = HealthStatusUnknown
	}

	// Update status fields
	backend.Status.Health = healthStatus
	backend.Status.AverageLatencyMs = result.Latency.Milliseconds()

	if result.Healthy {
		now := metav1.Now()
		backend.Status.LastHealthCheck = &now
	}

	// Set conditions
	r.setHealthCondition(backend, healthStatus, result.Error)
	r.setReadyCondition(backend, healthStatus)

	// Persist status update
	if err := r.Status().Update(ctx, backend); err != nil {
		log.Error(err, "Failed to update InferenceBackend status")
		return ctrl.Result{}, err
	}

	// Update cache for proxy to use
	if r.Cache != nil {
		r.Cache.SetBackend(req.NamespacedName, backend)
	}

	log.V(1).Info("Reconciled InferenceBackend",
		"health", healthStatus,
		"latency_ms", result.Latency.Milliseconds(),
		"failures", currentFailures)

	// Calculate requeue interval from health check config
	interval := 30 * time.Second // default
	if backend.Spec.HealthCheck != nil && backend.Spec.HealthCheck.IntervalSeconds > 0 {
		interval = time.Duration(backend.Spec.HealthCheck.IntervalSeconds) * time.Second
	}

	return ctrl.Result{RequeueAfter: interval}, nil
}

// validateBackendConfig ensures the backend has the required configuration for its type
func (r *InferenceBackendReconciler) validateBackendConfig(backend *gatewayv1alpha1.InferenceBackend) error {
	switch backend.Spec.Type {
	case gatewayv1alpha1.BackendTypeExternal:
		if backend.Spec.External == nil {
			return fmt.Errorf("external config is required for backend type 'external'")
		}
		if backend.Spec.External.URL == "" {
			return fmt.Errorf("external.url is required for backend type 'external'")
		}

	case gatewayv1alpha1.BackendTypeKubernetes:
		if backend.Spec.Kubernetes == nil {
			return fmt.Errorf("kubernetes config is required for backend type 'kubernetes'")
		}
		if backend.Spec.Kubernetes.ServiceName == "" {
			return fmt.Errorf("kubernetes.serviceName is required for backend type 'kubernetes'")
		}

	case gatewayv1alpha1.BackendTypeKServe:
		if backend.Spec.KServe == nil {
			return fmt.Errorf("kserve config is required for backend type 'kserve'")
		}
		if backend.Spec.KServe.ServiceName == "" {
			return fmt.Errorf("kserve.serviceName is required for backend type 'kserve'")
		}

	default:
		return fmt.Errorf("unknown backend type: %s", backend.Spec.Type)
	}

	return nil
}

// updateStatusWithError updates the backend status to reflect a configuration error
func (r *InferenceBackendReconciler) updateStatusWithError(ctx context.Context, backend *gatewayv1alpha1.InferenceBackend, err error) (ctrl.Result, error) {
	backend.Status.Health = HealthStatusUnhealthy

	condition := metav1.Condition{
		Type:               ConditionTypeBackendReady,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: backend.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             "ConfigurationError",
		Message:            err.Error(),
	}
	meta.SetStatusCondition(&backend.Status.Conditions, condition)

	if updateErr := r.Status().Update(ctx, backend); updateErr != nil {
		return ctrl.Result{}, updateErr
	}

	// Requeue to retry after a short delay
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// setHealthCondition sets the Healthy condition based on health check results
func (r *InferenceBackendReconciler) setHealthCondition(backend *gatewayv1alpha1.InferenceBackend, status string, err error) {
	condition := metav1.Condition{
		Type:               ConditionTypeBackendHealthy,
		ObservedGeneration: backend.Generation,
		LastTransitionTime: metav1.Now(),
	}

	switch status {
	case HealthStatusHealthy:
		condition.Status = metav1.ConditionTrue
		condition.Reason = "HealthCheckPassed"
		condition.Message = "Backend is responding to health checks"

	case HealthStatusUnhealthy:
		condition.Status = metav1.ConditionFalse
		condition.Reason = "HealthCheckFailed"
		if err != nil {
			condition.Message = fmt.Sprintf("Health check failed: %s", err.Error())
		} else {
			condition.Message = "Backend failed health check threshold"
		}

	default: // Unknown
		condition.Status = metav1.ConditionUnknown
		condition.Reason = "HealthCheckPending"
		if err != nil {
			condition.Message = fmt.Sprintf("Health check in progress: %s", err.Error())
		} else {
			condition.Message = "Health check status pending"
		}
	}

	meta.SetStatusCondition(&backend.Status.Conditions, condition)
}

// setReadyCondition sets the Ready condition based on overall backend readiness
func (r *InferenceBackendReconciler) setReadyCondition(backend *gatewayv1alpha1.InferenceBackend, healthStatus string) {
	condition := metav1.Condition{
		Type:               ConditionTypeBackendReady,
		ObservedGeneration: backend.Generation,
		LastTransitionTime: metav1.Now(),
	}

	if healthStatus == HealthStatusHealthy {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "BackendReady"
		condition.Message = "Backend is configured and healthy"
	} else {
		condition.Status = metav1.ConditionFalse
		condition.Reason = "BackendNotReady"
		condition.Message = fmt.Sprintf("Backend health status is %s", healthStatus)
	}

	meta.SetStatusCondition(&backend.Status.Conditions, condition)
}

// cleanupBackend removes tracking data for a deleted backend
func (r *InferenceBackendReconciler) cleanupBackend(key string) {
	r.failureMu.Lock()
	delete(r.failureCounts, key)
	r.failureMu.Unlock()

	// Note: Cache cleanup is handled separately when the backend is actually deleted
}

// SetupWithManager sets up the controller with the Manager
func (r *InferenceBackendReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1alpha1.InferenceBackend{}).
		Named("inferencebackend").
		Complete(r)
}
