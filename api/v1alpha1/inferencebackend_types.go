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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BackendType defines the type of inference backend
// +kubebuilder:validation:Enum=kserve;external;kubernetes
type BackendType string

const (
	// BackendTypeKServe represents a KServe InferenceService
	BackendTypeKServe BackendType = "kserve"
	// BackendTypeExternal represents an external API (OpenAI, Anthropic, etc.)
	BackendTypeExternal BackendType = "external"
	// BackendTypeKubernetes represents a regular Kubernetes Service
	BackendTypeKubernetes BackendType = "kubernetes"
)

// KServeBackend defines a KServe InferenceService backend
type KServeBackend struct {
	// Name of the KServe InferenceService
	// +required
	ServiceName string `json:"serviceName"`

	// Namespace of the InferenceService (defaults to same namespace)
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// ExternalBackend defines an external API backend
type ExternalBackend struct {
	// Base URL of the external API
	// +required
	URL string `json:"url"`

	// Provider type for API compatibility
	// +kubebuilder:validation:Enum=openai;anthropic;cohere;custom
	// +kubebuilder:default="openai"
	// +optional
	Provider string `json:"provider,omitempty"`

	// Secret containing the API key
	// +optional
	APIKeySecret *corev1.SecretKeySelector `json:"apiKeySecret,omitempty"`

	// Model name to use for this backend
	// +optional
	Model string `json:"model,omitempty"`
}

// KubernetesBackend defines a Kubernetes Service backend
type KubernetesBackend struct {
	// Name of the Kubernetes Service
	// +required
	ServiceName string `json:"serviceName"`

	// Namespace of the Service (defaults to same namespace)
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Port to connect to
	// +kubebuilder:default=8080
	// +optional
	Port int32 `json:"port,omitempty"`
}

// HealthCheck defines health check configuration
type HealthCheck struct {
	// Path for health check endpoint
	// +kubebuilder:default="/health"
	// +optional
	Path string `json:"path,omitempty"`

	// Interval between health checks in seconds
	// +kubebuilder:default=30
	// +optional
	IntervalSeconds int32 `json:"intervalSeconds,omitempty"`

	// Timeout for health check in seconds
	// +kubebuilder:default=5
	// +optional
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`

	// Number of consecutive failures before marking unhealthy
	// +kubebuilder:default=3
	// +optional
	FailureThreshold int32 `json:"failureThreshold,omitempty"`
}

// CostConfig defines cost tracking configuration
type CostConfig struct {
	// Cost per 1000 input tokens
	// +optional
	InputTokenCost string `json:"inputTokenCost,omitempty"`

	// Cost per 1000 output tokens
	// +optional
	OutputTokenCost string `json:"outputTokenCost,omitempty"`

	// Fixed cost per request
	// +optional
	RequestCost string `json:"requestCost,omitempty"`

	// Currency for costs
	// +kubebuilder:default="USD"
	// +optional
	Currency string `json:"currency,omitempty"`
}

// InferenceBackendSpec defines the desired state of InferenceBackend
type InferenceBackendSpec struct {
	// Type of backend
	// +required
	Type BackendType `json:"type"`

	// KServe backend configuration
	// +optional
	KServe *KServeBackend `json:"kserve,omitempty"`

	// External API backend configuration
	// +optional
	External *ExternalBackend `json:"external,omitempty"`

	// Kubernetes Service backend configuration
	// +optional
	Kubernetes *KubernetesBackend `json:"kubernetes,omitempty"`

	// Health check configuration
	// +optional
	HealthCheck *HealthCheck `json:"healthCheck,omitempty"`

	// Cost configuration for tracking
	// +optional
	Cost *CostConfig `json:"cost,omitempty"`

	// Request timeout in seconds
	// +kubebuilder:default=60
	// +optional
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`

	// Maximum concurrent requests
	// +kubebuilder:default=100
	// +optional
	MaxConcurrency int32 `json:"maxConcurrency,omitempty"`

	// Priority for fallback ordering (higher = preferred)
	// +kubebuilder:default=0
	// +optional
	Priority int32 `json:"priority,omitempty"`
}

// InferenceBackendStatus defines the observed state of InferenceBackend
type InferenceBackendStatus struct {
	// Health status of the backend
	// +kubebuilder:validation:Enum=Healthy;Unhealthy;Unknown
	// +optional
	Health string `json:"health,omitempty"`

	// Last successful health check time
	// +optional
	LastHealthCheck *metav1.Time `json:"lastHealthCheck,omitempty"`

	// Current request count
	// +optional
	ActiveRequests int32 `json:"activeRequests,omitempty"`

	// Total requests served
	// +optional
	TotalRequests int64 `json:"totalRequests,omitempty"`

	// Total errors
	// +optional
	TotalErrors int64 `json:"totalErrors,omitempty"`

	// Average latency in milliseconds
	// +optional
	AverageLatencyMs int64 `json:"averageLatencyMs,omitempty"`

	// Conditions represent the current state
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.type"
// +kubebuilder:printcolumn:name="Health",type="string",JSONPath=".status.health"
// +kubebuilder:printcolumn:name="Requests",type="integer",JSONPath=".status.totalRequests"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// InferenceBackend is the Schema for the inferencebackends API.
// It defines a backend service for inference requests.
type InferenceBackend struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +required
	Spec InferenceBackendSpec `json:"spec"`

	// +optional
	Status InferenceBackendStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// InferenceBackendList contains a list of InferenceBackend
type InferenceBackendList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []InferenceBackend `json:"items"`
}

func init() {
	SchemeBuilder.Register(&InferenceBackend{}, &InferenceBackendList{})
}
