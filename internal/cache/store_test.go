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

package cache

import (
	"sync"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	gatewayv1alpha1 "github.com/judeoyovbaire/kortex/api/v1alpha1"
)

func TestNewStore(t *testing.T) {
	store := NewStore()

	if store == nil {
		t.Fatal("expected non-nil store")
	}

	stats := store.GetStats()
	if stats.RouteCount != 0 {
		t.Errorf("expected 0 routes, got %d", stats.RouteCount)
	}
	if stats.BackendCount != 0 {
		t.Errorf("expected 0 backends, got %d", stats.BackendCount)
	}
}

func TestStore_SetRoute(t *testing.T) {
	store := NewStore()
	key := types.NamespacedName{Namespace: "default", Name: "test-route"}
	route := &gatewayv1alpha1.InferenceRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
		Spec: gatewayv1alpha1.InferenceRouteSpec{
			DefaultBackend: &gatewayv1alpha1.BackendRef{Name: "backend1"},
		},
	}

	store.SetRoute(key, route)

	stats := store.GetStats()
	if stats.RouteCount != 1 {
		t.Errorf("expected 1 route, got %d", stats.RouteCount)
	}
}

func TestStore_GetRoute(t *testing.T) {
	store := NewStore()
	key := types.NamespacedName{Namespace: "default", Name: "test-route"}
	route := &gatewayv1alpha1.InferenceRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
	}

	// Get non-existent route
	_, ok := store.GetRoute(key)
	if ok {
		t.Error("expected false for non-existent route")
	}

	// Set and get route
	store.SetRoute(key, route)
	retrieved, ok := store.GetRoute(key)
	if !ok {
		t.Error("expected true for existing route")
	}
	if retrieved.Name != "test-route" {
		t.Errorf("expected name 'test-route', got '%s'", retrieved.Name)
	}
}

func TestStore_GetRoute_ReturnsDeepCopy(t *testing.T) {
	store := NewStore()
	key := types.NamespacedName{Namespace: "default", Name: "test-route"}
	route := &gatewayv1alpha1.InferenceRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
	}

	store.SetRoute(key, route)

	// Get the route and modify it
	retrieved, _ := store.GetRoute(key)
	retrieved.Name = "modified"

	// Get again and verify original is unchanged
	retrieved2, _ := store.GetRoute(key)
	if retrieved2.Name != "test-route" {
		t.Error("expected deep copy to prevent mutation")
	}
}

func TestStore_DeleteRoute(t *testing.T) {
	store := NewStore()
	key := types.NamespacedName{Namespace: "default", Name: "test-route"}
	route := &gatewayv1alpha1.InferenceRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
	}

	store.SetRoute(key, route)
	store.DeleteRoute(key)

	_, ok := store.GetRoute(key)
	if ok {
		t.Error("expected route to be deleted")
	}

	stats := store.GetStats()
	if stats.RouteCount != 0 {
		t.Errorf("expected 0 routes after deletion, got %d", stats.RouteCount)
	}
}

func TestStore_ListRoutes(t *testing.T) {
	store := NewStore()

	// Add multiple routes
	for i := 0; i < 3; i++ {
		key := types.NamespacedName{Namespace: "default", Name: "route-" + string(rune('a'+i))}
		route := &gatewayv1alpha1.InferenceRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      key.Name,
				Namespace: key.Namespace,
			},
		}
		store.SetRoute(key, route)
	}

	routes := store.ListRoutes()
	if len(routes) != 3 {
		t.Errorf("expected 3 routes, got %d", len(routes))
	}
}

func TestStore_ListRoutesInNamespace(t *testing.T) {
	store := NewStore()

	// Add routes in different namespaces
	namespaces := []string{"ns1", "ns1", "ns2"}
	for i, ns := range namespaces {
		key := types.NamespacedName{Namespace: ns, Name: "route-" + string(rune('a'+i))}
		route := &gatewayv1alpha1.InferenceRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      key.Name,
				Namespace: key.Namespace,
			},
		}
		store.SetRoute(key, route)
	}

	ns1Routes := store.ListRoutesInNamespace("ns1")
	if len(ns1Routes) != 2 {
		t.Errorf("expected 2 routes in ns1, got %d", len(ns1Routes))
	}

	ns2Routes := store.ListRoutesInNamespace("ns2")
	if len(ns2Routes) != 1 {
		t.Errorf("expected 1 route in ns2, got %d", len(ns2Routes))
	}

	ns3Routes := store.ListRoutesInNamespace("ns3")
	if len(ns3Routes) != 0 {
		t.Errorf("expected 0 routes in ns3, got %d", len(ns3Routes))
	}
}

func TestStore_SetBackend(t *testing.T) {
	store := NewStore()
	key := types.NamespacedName{Namespace: "default", Name: "test-backend"}
	backend := &gatewayv1alpha1.InferenceBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backend",
			Namespace: "default",
		},
		Spec: gatewayv1alpha1.InferenceBackendSpec{
			Type: gatewayv1alpha1.BackendTypeExternal,
		},
	}

	store.SetBackend(key, backend)

	stats := store.GetStats()
	if stats.BackendCount != 1 {
		t.Errorf("expected 1 backend, got %d", stats.BackendCount)
	}
}

func TestStore_GetBackend(t *testing.T) {
	store := NewStore()
	key := types.NamespacedName{Namespace: "default", Name: "test-backend"}
	backend := &gatewayv1alpha1.InferenceBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backend",
			Namespace: "default",
		},
		Spec: gatewayv1alpha1.InferenceBackendSpec{
			Type: gatewayv1alpha1.BackendTypeExternal,
		},
	}

	// Get non-existent backend
	_, ok := store.GetBackend(key)
	if ok {
		t.Error("expected false for non-existent backend")
	}

	// Set and get backend
	store.SetBackend(key, backend)
	retrieved, ok := store.GetBackend(key)
	if !ok {
		t.Error("expected true for existing backend")
	}
	if retrieved.Name != "test-backend" {
		t.Errorf("expected name 'test-backend', got '%s'", retrieved.Name)
	}
}

func TestStore_DeleteBackend(t *testing.T) {
	store := NewStore()
	key := types.NamespacedName{Namespace: "default", Name: "test-backend"}
	backend := &gatewayv1alpha1.InferenceBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backend",
			Namespace: "default",
		},
	}

	store.SetBackend(key, backend)
	store.DeleteBackend(key)

	_, ok := store.GetBackend(key)
	if ok {
		t.Error("expected backend to be deleted")
	}
}

func TestStore_ListBackends(t *testing.T) {
	store := NewStore()

	// Add multiple backends
	for i := 0; i < 3; i++ {
		key := types.NamespacedName{Namespace: "default", Name: "backend-" + string(rune('a'+i))}
		backend := &gatewayv1alpha1.InferenceBackend{
			ObjectMeta: metav1.ObjectMeta{
				Name:      key.Name,
				Namespace: key.Namespace,
			},
		}
		store.SetBackend(key, backend)
	}

	backends := store.ListBackends()
	if len(backends) != 3 {
		t.Errorf("expected 3 backends, got %d", len(backends))
	}
}

func TestStore_ListBackendsInNamespace(t *testing.T) {
	store := NewStore()

	// Add backends in different namespaces
	namespaces := []string{"ns1", "ns1", "ns2"}
	for i, ns := range namespaces {
		key := types.NamespacedName{Namespace: ns, Name: "backend-" + string(rune('a'+i))}
		backend := &gatewayv1alpha1.InferenceBackend{
			ObjectMeta: metav1.ObjectMeta{
				Name:      key.Name,
				Namespace: key.Namespace,
			},
		}
		store.SetBackend(key, backend)
	}

	ns1Backends := store.ListBackendsInNamespace("ns1")
	if len(ns1Backends) != 2 {
		t.Errorf("expected 2 backends in ns1, got %d", len(ns1Backends))
	}

	ns2Backends := store.ListBackendsInNamespace("ns2")
	if len(ns2Backends) != 1 {
		t.Errorf("expected 1 backend in ns2, got %d", len(ns2Backends))
	}
}

func TestStore_GetHealthyBackend(t *testing.T) {
	store := NewStore()
	key := types.NamespacedName{Namespace: "default", Name: "test-backend"}

	// Healthy backend
	healthyBackend := &gatewayv1alpha1.InferenceBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backend",
			Namespace: "default",
		},
		Status: gatewayv1alpha1.InferenceBackendStatus{
			Health: "Healthy",
		},
	}
	store.SetBackend(key, healthyBackend)

	backend, ok := store.GetHealthyBackend("default", "test-backend")
	if !ok {
		t.Error("expected to get healthy backend")
	}
	if backend == nil {
		t.Error("expected non-nil backend")
	}

	// Unhealthy backend
	unhealthyBackend := &gatewayv1alpha1.InferenceBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backend",
			Namespace: "default",
		},
		Status: gatewayv1alpha1.InferenceBackendStatus{
			Health: "Unhealthy",
		},
	}
	store.SetBackend(key, unhealthyBackend)

	_, ok = store.GetHealthyBackend("default", "test-backend")
	if ok {
		t.Error("expected false for unhealthy backend")
	}
}

func TestStore_GetBackendByName(t *testing.T) {
	store := NewStore()
	key := types.NamespacedName{Namespace: "default", Name: "test-backend"}
	backend := &gatewayv1alpha1.InferenceBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backend",
			Namespace: "default",
		},
	}

	store.SetBackend(key, backend)

	retrieved, ok := store.GetBackendByName("default", "test-backend")
	if !ok {
		t.Error("expected to find backend")
	}
	if retrieved.Name != "test-backend" {
		t.Errorf("expected name 'test-backend', got '%s'", retrieved.Name)
	}
}

func TestStore_GetRouteByName(t *testing.T) {
	store := NewStore()
	key := types.NamespacedName{Namespace: "default", Name: "test-route"}
	route := &gatewayv1alpha1.InferenceRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
	}

	store.SetRoute(key, route)

	retrieved, ok := store.GetRouteByName("default", "test-route")
	if !ok {
		t.Error("expected to find route")
	}
	if retrieved.Name != "test-route" {
		t.Errorf("expected name 'test-route', got '%s'", retrieved.Name)
	}
}

func TestStore_ConcurrentAccess(t *testing.T) {
	store := NewStore()
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := types.NamespacedName{Namespace: "default", Name: "route-" + string(rune('a'+id%26))}
			route := &gatewayv1alpha1.InferenceRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
			}
			store.SetRoute(key, route)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := types.NamespacedName{Namespace: "default", Name: "route-" + string(rune('a'+id%26))}
			store.GetRoute(key)
		}(i)
	}

	wg.Wait()

	// Should not panic or produce race conditions
	stats := store.GetStats()
	if stats.RouteCount == 0 {
		t.Error("expected some routes to be stored")
	}
}

func TestStore_GetStats(t *testing.T) {
	store := NewStore()

	// Add routes and backends
	for i := 0; i < 5; i++ {
		routeKey := types.NamespacedName{Namespace: "default", Name: "route-" + string(rune('a'+i))}
		route := &gatewayv1alpha1.InferenceRoute{
			ObjectMeta: metav1.ObjectMeta{Name: routeKey.Name, Namespace: routeKey.Namespace},
		}
		store.SetRoute(routeKey, route)

		backendKey := types.NamespacedName{Namespace: "default", Name: "backend-" + string(rune('a'+i))}
		backend := &gatewayv1alpha1.InferenceBackend{
			ObjectMeta: metav1.ObjectMeta{Name: backendKey.Name, Namespace: backendKey.Namespace},
		}
		store.SetBackend(backendKey, backend)
	}

	stats := store.GetStats()
	if stats.RouteCount != 5 {
		t.Errorf("expected 5 routes, got %d", stats.RouteCount)
	}
	if stats.BackendCount != 5 {
		t.Errorf("expected 5 backends, got %d", stats.BackendCount)
	}
}
