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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RouteMatch defines conditions for matching incoming requests
type RouteMatch struct {
	// Headers to match against incoming requests
	// +optional
	Headers map[string]string `json:"headers,omitempty"`

	// Path prefix to match
	// +optional
	PathPrefix *string `json:"pathPrefix,omitempty"`

	// Model name pattern to match (supports wildcards)
	// +optional
	ModelPattern *string `json:"modelPattern,omitempty"`
}

// BackendRef references a backend for routing
type BackendRef struct {
	// Name of the InferenceBackend resource
	// +required
	Name string `json:"name"`

	// Weight for weighted routing (0-100)
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	// +kubebuilder:default=100
	// +optional
	Weight int32 `json:"weight,omitempty"`
}

// RouteRule defines a single routing rule
type RouteRule struct {
	// Match conditions for this rule
	// +optional
	Match *RouteMatch `json:"match,omitempty"`

	// Backends to route matching requests to
	// +required
	// +kubebuilder:validation:MinItems=1
	Backends []BackendRef `json:"backends"`
}

// FallbackChain defines ordered fallback backends
type FallbackChain struct {
	// Ordered list of backend names to try
	// +required
	// +kubebuilder:validation:MinItems=1
	Backends []string `json:"backends"`

	// Timeout per backend attempt in seconds
	// +kubebuilder:default=30
	// +optional
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`
}

// RateLimitConfig defines rate limiting settings
type RateLimitConfig struct {
	// Maximum requests per minute
	// +kubebuilder:validation:Minimum=1
	// +required
	RequestsPerMinute int32 `json:"requestsPerMinute"`

	// Apply rate limit per user (based on header)
	// +kubebuilder:default=false
	// +optional
	PerUser bool `json:"perUser,omitempty"`

	// Header name to identify users
	// +kubebuilder:default="x-user-id"
	// +optional
	UserHeader string `json:"userHeader,omitempty"`
}

// ABExperiment defines an A/B test configuration
type ABExperiment struct {
	// Name of the experiment
	// +required
	Name string `json:"name"`

	// Control backend name
	// +required
	Control string `json:"control"`

	// Treatment backend name
	// +required
	Treatment string `json:"treatment"`

	// Percentage of traffic to send to treatment (0-100)
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	// +kubebuilder:default=10
	// +optional
	TrafficPercent int32 `json:"trafficPercent,omitempty"`

	// Metric to track for statistical analysis
	// +kubebuilder:default="latency_p95"
	// +optional
	Metric string `json:"metric,omitempty"`
}

// InferenceRouteSpec defines the desired state of InferenceRoute
type InferenceRouteSpec struct {
	// Rules for routing requests to backends
	// +optional
	Rules []RouteRule `json:"rules,omitempty"`

	// Default backend if no rules match
	// +optional
	DefaultBackend *BackendRef `json:"defaultBackend,omitempty"`

	// Fallback chain for automatic failover
	// +optional
	Fallback *FallbackChain `json:"fallback,omitempty"`

	// Rate limiting configuration
	// +optional
	RateLimit *RateLimitConfig `json:"rateLimit,omitempty"`

	// A/B testing experiments
	// +optional
	Experiments []ABExperiment `json:"experiments,omitempty"`

	// Enable cost tracking per request
	// +kubebuilder:default=true
	// +optional
	CostTracking bool `json:"costTracking,omitempty"`

	// Enable request/response logging
	// +kubebuilder:default=false
	// +optional
	EnableLogging bool `json:"enableLogging,omitempty"`
}

// InferenceRouteStatus defines the observed state of InferenceRoute
type InferenceRouteStatus struct {
	// Phase represents the current phase of the route
	// +kubebuilder:validation:Enum=Pending;Active;Degraded;Failed
	// +optional
	Phase string `json:"phase,omitempty"`

	// Total requests processed
	// +optional
	TotalRequests int64 `json:"totalRequests,omitempty"`

	// Active backends count
	// +optional
	ActiveBackends int32 `json:"activeBackends,omitempty"`

	// Last time the route was updated
	// +optional
	LastUpdated *metav1.Time `json:"lastUpdated,omitempty"`

	// Conditions represent the current state of the InferenceRoute
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Backends",type="integer",JSONPath=".status.activeBackends"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// InferenceRoute is the Schema for the inferenceroutes API.
// It defines routing rules for directing inference requests to backends.
type InferenceRoute struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +required
	Spec InferenceRouteSpec `json:"spec"`

	// +optional
	Status InferenceRouteStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// InferenceRouteList contains a list of InferenceRoute
type InferenceRouteList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []InferenceRoute `json:"items"`
}

func init() {
	SchemeBuilder.Register(&InferenceRoute{}, &InferenceRouteList{})
}
