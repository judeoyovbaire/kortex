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
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gatewayv1alpha1 "github.com/judeoyovbaire/kortex/api/v1alpha1"
	"github.com/judeoyovbaire/kortex/internal/cache"
)

// Route phase constants
const (
	PhasePending  = "Pending"
	PhaseActive   = "Active"
	PhaseDegraded = "Degraded"
	PhaseFailed   = "Failed"
)

// Condition types for InferenceRoute
const (
	ConditionTypeRouteReady    = "Ready"
	ConditionTypeBackendsReady = "BackendsReady"
	ConditionTypeRouteValid    = "RouteValid"
)

// InferenceRouteReconciler reconciles a InferenceRoute object
type InferenceRouteReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Cache  *cache.Store
}

// +kubebuilder:rbac:groups=gateway.inference-gateway.io,resources=inferenceroutes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gateway.inference-gateway.io,resources=inferenceroutes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gateway.inference-gateway.io,resources=inferenceroutes/finalizers,verbs=update
// +kubebuilder:rbac:groups=gateway.inference-gateway.io,resources=inferencebackends,verbs=get;list;watch

// Reconcile performs the reconciliation loop for InferenceRoute resources.
// It validates that referenced backends exist and are healthy, then updates
// the route status accordingly.
func (r *InferenceRouteReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the InferenceRoute resource
	route := &gatewayv1alpha1.InferenceRoute{}
	if err := r.Get(ctx, req.NamespacedName, route); err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Resource was deleted, clean up cache
			if r.Cache != nil {
				r.Cache.DeleteRoute(req.NamespacedName)
			}
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to fetch InferenceRoute")
		return ctrl.Result{}, err
	}

	// Collect all referenced backend names from the route spec
	backendNames := r.collectBackendNames(route)

	// Validate and count backend statuses
	totalBackends := len(backendNames)
	healthyBackends := int32(0)
	var missingBackends []string
	var unhealthyBackends []string

	for _, name := range backendNames {
		backend := &gatewayv1alpha1.InferenceBackend{}
		err := r.Get(ctx, types.NamespacedName{
			Name:      name,
			Namespace: route.Namespace,
		}, backend)

		if err != nil {
			if client.IgnoreNotFound(err) == nil {
				missingBackends = append(missingBackends, name)
				continue
			}
			log.Error(err, "Failed to fetch backend", "backend", name)
			continue
		}

		if backend.Status.Health == HealthStatusHealthy {
			healthyBackends++
		} else {
			unhealthyBackends = append(unhealthyBackends, name)
		}
	}

	// Determine the route phase based on backend availability
	phase := r.determinePhase(totalBackends, int(healthyBackends), len(missingBackends))

	// Update status fields
	now := metav1.Now()
	route.Status.Phase = phase
	route.Status.ActiveBackends = healthyBackends
	route.Status.LastUpdated = &now

	// Set conditions
	r.setBackendsCondition(route, missingBackends, unhealthyBackends)
	r.setRouteValidCondition(route, missingBackends)
	r.setReadyCondition(route, phase)

	// Persist status update
	if err := r.Status().Update(ctx, route); err != nil {
		log.Error(err, "Failed to update InferenceRoute status")
		return ctrl.Result{}, err
	}

	// Update cache for proxy to use
	if r.Cache != nil {
		r.Cache.SetRoute(req.NamespacedName, route)
	}

	log.V(1).Info("Reconciled InferenceRoute",
		"phase", phase,
		"activeBackends", healthyBackends,
		"totalBackends", totalBackends,
		"missing", missingBackends,
		"unhealthy", unhealthyBackends)

	// Requeue periodically to catch backend status changes
	// This is a backup; the watch on InferenceBackend should trigger more immediate updates
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// collectBackendNames extracts all unique backend names referenced in the route spec
func (r *InferenceRouteReconciler) collectBackendNames(route *gatewayv1alpha1.InferenceRoute) []string {
	nameSet := make(map[string]struct{})

	// From routing rules
	for _, rule := range route.Spec.Rules {
		for _, backend := range rule.Backends {
			nameSet[backend.Name] = struct{}{}
		}
	}

	// From default backend
	if route.Spec.DefaultBackend != nil {
		nameSet[route.Spec.DefaultBackend.Name] = struct{}{}
	}

	// From fallback chain
	if route.Spec.Fallback != nil {
		for _, name := range route.Spec.Fallback.Backends {
			nameSet[name] = struct{}{}
		}
	}

	// From A/B experiments
	for _, exp := range route.Spec.Experiments {
		nameSet[exp.Control] = struct{}{}
		nameSet[exp.Treatment] = struct{}{}
	}

	// Convert set to slice
	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	return names
}

// determinePhase calculates the route phase based on backend availability
func (r *InferenceRouteReconciler) determinePhase(total, healthy, missing int) string {
	// No backends configured
	if total == 0 {
		return PhasePending
	}

	// Some backends are missing (configuration error)
	if missing > 0 {
		return PhaseFailed
	}

	// All backends are unhealthy
	if healthy == 0 {
		return PhaseFailed
	}

	// Some backends are unhealthy but at least one is healthy
	if healthy < total {
		return PhaseDegraded
	}

	// All backends are healthy
	return PhaseActive
}

// setBackendsCondition sets the BackendsReady condition based on backend availability
func (r *InferenceRouteReconciler) setBackendsCondition(route *gatewayv1alpha1.InferenceRoute, missing, unhealthy []string) {
	condition := metav1.Condition{
		Type:               ConditionTypeBackendsReady,
		ObservedGeneration: route.Generation,
		LastTransitionTime: metav1.Now(),
	}

	if len(missing) == 0 && len(unhealthy) == 0 {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "AllBackendsReady"
		condition.Message = "All referenced backends are healthy"
	} else if len(missing) > 0 {
		condition.Status = metav1.ConditionFalse
		condition.Reason = "BackendsMissing"
		condition.Message = fmt.Sprintf("Missing backends: %v", missing)
	} else {
		condition.Status = metav1.ConditionFalse
		condition.Reason = "BackendsUnhealthy"
		condition.Message = fmt.Sprintf("Unhealthy backends: %v", unhealthy)
	}

	meta.SetStatusCondition(&route.Status.Conditions, condition)
}

// setRouteValidCondition sets the RouteValid condition based on configuration validity
func (r *InferenceRouteReconciler) setRouteValidCondition(route *gatewayv1alpha1.InferenceRoute, missing []string) {
	condition := metav1.Condition{
		Type:               ConditionTypeRouteValid,
		ObservedGeneration: route.Generation,
		LastTransitionTime: metav1.Now(),
	}

	if len(missing) == 0 {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "RouteValid"
		condition.Message = "Route configuration is valid"
	} else {
		condition.Status = metav1.ConditionFalse
		condition.Reason = "InvalidBackendReferences"
		condition.Message = fmt.Sprintf("Referenced backends do not exist: %v", missing)
	}

	meta.SetStatusCondition(&route.Status.Conditions, condition)
}

// setReadyCondition sets the Ready condition based on overall route readiness
func (r *InferenceRouteReconciler) setReadyCondition(route *gatewayv1alpha1.InferenceRoute, phase string) {
	condition := metav1.Condition{
		Type:               ConditionTypeRouteReady,
		ObservedGeneration: route.Generation,
		LastTransitionTime: metav1.Now(),
	}

	switch phase {
	case PhaseActive:
		condition.Status = metav1.ConditionTrue
		condition.Reason = "RouteReady"
		condition.Message = "Route is active and all backends are healthy"
	case PhaseDegraded:
		condition.Status = metav1.ConditionTrue
		condition.Reason = "RouteDegraded"
		condition.Message = "Route is operational but some backends are unhealthy"
	case PhasePending:
		condition.Status = metav1.ConditionFalse
		condition.Reason = "RoutePending"
		condition.Message = "Route has no backends configured"
	default: // Failed
		condition.Status = metav1.ConditionFalse
		condition.Reason = "RouteFailed"
		condition.Message = "Route is not operational due to backend issues"
	}

	meta.SetStatusCondition(&route.Status.Conditions, condition)
}

// findRoutesForBackend returns reconcile requests for all routes that reference the given backend.
// This enables the route controller to react to backend status changes.
func (r *InferenceRouteReconciler) findRoutesForBackend(ctx context.Context, obj client.Object) []reconcile.Request {
	backend, ok := obj.(*gatewayv1alpha1.InferenceBackend)
	if !ok {
		return nil
	}

	// List all routes in the same namespace as the backend
	routes := &gatewayv1alpha1.InferenceRouteList{}
	if err := r.List(ctx, routes, client.InNamespace(backend.Namespace)); err != nil {
		logf.FromContext(ctx).Error(err, "Failed to list InferenceRoutes for backend watch")
		return nil
	}

	var requests []reconcile.Request
	for _, route := range routes.Items {
		// Check if this route references the backend
		names := r.collectBackendNames(&route)
		for _, name := range names {
			if name == backend.Name {
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      route.Name,
						Namespace: route.Namespace,
					},
				})
				break
			}
		}
	}

	return requests
}

// SetupWithManager sets up the controller with the Manager.
func (r *InferenceRouteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1alpha1.InferenceRoute{}).
		// Watch InferenceBackends and trigger route reconciliation when backends change
		Watches(
			&gatewayv1alpha1.InferenceBackend{},
			handler.EnqueueRequestsFromMapFunc(r.findRoutesForBackend),
		).
		Named("inferenceroute").
		Complete(r)
}
