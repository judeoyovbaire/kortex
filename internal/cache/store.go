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

	"k8s.io/apimachinery/pkg/types"

	gatewayv1alpha1 "github.com/judeoyovbaire/inference-gateway/api/v1alpha1"
)

// Store provides thread-safe access to InferenceRoutes and InferenceBackends.
// Controllers update this cache, and the proxy reads from it to avoid
// making Kubernetes API calls on every request.
type Store struct {
	mu       sync.RWMutex
	routes   map[types.NamespacedName]*gatewayv1alpha1.InferenceRoute
	backends map[types.NamespacedName]*gatewayv1alpha1.InferenceBackend
}

// NewStore creates a new empty cache store
func NewStore() *Store {
	return &Store{
		routes:   make(map[types.NamespacedName]*gatewayv1alpha1.InferenceRoute),
		backends: make(map[types.NamespacedName]*gatewayv1alpha1.InferenceBackend),
	}
}

// --- Route operations ---

// SetRoute adds or updates a route in the cache
func (s *Store) SetRoute(key types.NamespacedName, route *gatewayv1alpha1.InferenceRoute) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Deep copy to prevent mutation of cached objects
	s.routes[key] = route.DeepCopy()
}

// GetRoute retrieves a route from the cache
func (s *Store) GetRoute(key types.NamespacedName) (*gatewayv1alpha1.InferenceRoute, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	route, ok := s.routes[key]
	if !ok {
		return nil, false
	}
	// Return a deep copy to prevent mutation
	return route.DeepCopy(), true
}

// DeleteRoute removes a route from the cache
func (s *Store) DeleteRoute(key types.NamespacedName) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.routes, key)
}

// ListRoutes returns all routes in the cache
func (s *Store) ListRoutes() []*gatewayv1alpha1.InferenceRoute {
	s.mu.RLock()
	defer s.mu.RUnlock()
	routes := make([]*gatewayv1alpha1.InferenceRoute, 0, len(s.routes))
	for _, r := range s.routes {
		routes = append(routes, r.DeepCopy())
	}
	return routes
}

// ListRoutesInNamespace returns all routes in a specific namespace
func (s *Store) ListRoutesInNamespace(namespace string) []*gatewayv1alpha1.InferenceRoute {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var routes []*gatewayv1alpha1.InferenceRoute
	for key, r := range s.routes {
		if key.Namespace == namespace {
			routes = append(routes, r.DeepCopy())
		}
	}
	return routes
}

// --- Backend operations ---

// SetBackend adds or updates a backend in the cache
func (s *Store) SetBackend(key types.NamespacedName, backend *gatewayv1alpha1.InferenceBackend) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Deep copy to prevent mutation of cached objects
	s.backends[key] = backend.DeepCopy()
}

// GetBackend retrieves a backend from the cache
func (s *Store) GetBackend(key types.NamespacedName) (*gatewayv1alpha1.InferenceBackend, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	backend, ok := s.backends[key]
	if !ok {
		return nil, false
	}
	// Return a deep copy to prevent mutation
	return backend.DeepCopy(), true
}

// DeleteBackend removes a backend from the cache
func (s *Store) DeleteBackend(key types.NamespacedName) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.backends, key)
}

// ListBackends returns all backends in the cache
func (s *Store) ListBackends() []*gatewayv1alpha1.InferenceBackend {
	s.mu.RLock()
	defer s.mu.RUnlock()
	backends := make([]*gatewayv1alpha1.InferenceBackend, 0, len(s.backends))
	for _, b := range s.backends {
		backends = append(backends, b.DeepCopy())
	}
	return backends
}

// ListBackendsInNamespace returns all backends in a specific namespace
func (s *Store) ListBackendsInNamespace(namespace string) []*gatewayv1alpha1.InferenceBackend {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var backends []*gatewayv1alpha1.InferenceBackend
	for key, b := range s.backends {
		if key.Namespace == namespace {
			backends = append(backends, b.DeepCopy())
		}
	}
	return backends
}

// --- Convenience methods for proxy ---

// GetHealthyBackend retrieves a backend only if it's healthy
func (s *Store) GetHealthyBackend(namespace, name string) (*gatewayv1alpha1.InferenceBackend, bool) {
	backend, ok := s.GetBackend(types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	})
	if !ok {
		return nil, false
	}
	if backend.Status.Health != "Healthy" {
		return nil, false
	}
	return backend, true
}

// GetBackendByName is a convenience method to get a backend by namespace and name
func (s *Store) GetBackendByName(namespace, name string) (*gatewayv1alpha1.InferenceBackend, bool) {
	return s.GetBackend(types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	})
}

// GetRouteByName is a convenience method to get a route by namespace and name
func (s *Store) GetRouteByName(namespace, name string) (*gatewayv1alpha1.InferenceRoute, bool) {
	return s.GetRoute(types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	})
}

// --- Stats ---

// Stats returns cache statistics
type Stats struct {
	RouteCount   int
	BackendCount int
}

// GetStats returns current cache statistics
func (s *Store) GetStats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Stats{
		RouteCount:   len(s.routes),
		BackendCount: len(s.backends),
	}
}
